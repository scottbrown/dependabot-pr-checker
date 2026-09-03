package auth

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/v68/github"
)

const (
	// envAppID names the environment variable holding the GitHub App's ID.
	envAppID = "GITHUB_APP_ID"
	// envAppKey holds the App's private key as PEM text, which suits CI
	// systems that inject secrets as environment variables.
	envAppKey = "GITHUB_APP_PRIVATE_KEY"
	// envAppKeyPath holds a path to the App's private key file.
	envAppKeyPath = "GITHUB_APP_PRIVATE_KEY_PATH"
	// envAppInstallation pins the installation to use, skipping discovery.
	envAppInstallation = "GITHUB_APP_INSTALLATION_ID"
)

const (
	// jwtLifetime is how long the assertion authenticating as the App itself
	// stays valid. GitHub rejects anything over ten minutes.
	jwtLifetime = 9 * time.Minute
	// jwtBackdate absorbs clock skew between this host and GitHub, which
	// rejects assertions issued in the future.
	jwtBackdate = 30 * time.Second
	// tokenRenewMargin renews the installation token slightly early so that a
	// request cannot be rejected by a token that lapses in flight.
	tokenRenewMargin = 2 * time.Minute
)

// appAPIBase is the GitHub API root used for App authentication. It is a
// variable so that tests can point it at a local server.
var appAPIBase = "https://api.github.com"

// appCredential builds a credential that authenticates as a GitHub App
// installation. Organization policies that prohibit personal access tokens do
// not apply to installation tokens, which makes this the durable option for
// automation in such organizations.
func appCredential(ctx context.Context, org string) (*Credential, error) {
	rawID := strings.TrimSpace(os.Getenv(envAppID))
	if rawID == "" {
		return nil, notConfigured("%s is not set", envAppID)
	}
	appID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", envAppID, err)
	}

	key, err := appPrivateKey()
	if err != nil {
		return nil, err
	}

	installation, err := installationID(ctx, appID, key, org)
	if err != nil {
		return nil, err
	}

	return &Credential{
		Client: &http.Client{
			Transport: &installationTransport{
				base:         http.DefaultTransport,
				apiBase:      appAPIBase,
				appID:        appID,
				key:          key,
				installation: installation,
			},
		},
		Source: fmt.Sprintf("GitHub App %d (installation %d)", appID, installation),
	}, nil
}

// appPrivateKey loads the App's RSA private key from the environment.
func appPrivateKey() (*rsa.PrivateKey, error) {
	if raw := os.Getenv(envAppKey); strings.TrimSpace(raw) != "" {
		key, err := parsePrivateKey([]byte(raw))
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", envAppKey, err)
		}
		return key, nil
	}

	path := strings.TrimSpace(os.Getenv(envAppKeyPath))
	if path == "" {
		// Someone who set the App ID clearly intends App authentication, so a
		// missing key is a misconfiguration to report rather than a reason to
		// quietly authenticate as somebody else.
		return nil, fmt.Errorf("%s is set but neither %s nor %s is", envAppID, envAppKey, envAppKeyPath)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", envAppKeyPath, err)
	}
	key, err := parsePrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("parsing the key at %s: %w", path, err)
	}
	return key, nil
}

// parsePrivateKey decodes a PEM-encoded RSA private key. GitHub hands out
// PKCS#1 keys, but a key round-tripped through other tooling often comes back
// as PKCS#8, so both are accepted.
func parsePrivateKey(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(bytes.TrimSpace(data))
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}

	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("key is neither PKCS#1 nor PKCS#8: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key is %T, but GitHub Apps require RSA", parsed)
	}
	return key, nil
}

// installationID returns the pinned installation, or discovers the one covering
// org. Discovery keeps the usual case down to two environment variables, since
// the organization is already a required flag.
func installationID(ctx context.Context, appID int64, key *rsa.PrivateKey, org string) (int64, error) {
	if raw := strings.TrimSpace(os.Getenv(envAppInstallation)); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid %s: %w", envAppInstallation, err)
		}
		return id, nil
	}

	if org == "" {
		return 0, fmt.Errorf("cannot discover the installation without an organization; set %s", envAppInstallation)
	}

	client, err := appClient(&jwtTransport{base: http.DefaultTransport, appID: appID, key: key})
	if err != nil {
		return 0, err
	}

	installation, _, err := client.Apps.FindOrganizationInstallation(ctx, org)
	if err != nil {
		return 0, fmt.Errorf("finding the installation for %s (confirm the app is installed on that organization): %w", org, err)
	}
	return installation.GetID(), nil
}

// appClient builds a go-github client honouring appAPIBase.
func appClient(transport http.RoundTripper) (*github.Client, error) {
	client := github.NewClient(&http.Client{Transport: transport})

	base, err := url.Parse(strings.TrimSuffix(appAPIBase, "/") + "/")
	if err != nil {
		return nil, fmt.Errorf("invalid GitHub API base URL: %w", err)
	}
	client.BaseURL = base
	return client, nil
}

// jwtTransport authenticates as the GitHub App itself, which is the credential
// required to look up installations and mint installation tokens.
type jwtTransport struct {
	base  http.RoundTripper
	appID int64
	key   *rsa.PrivateKey
}

// RoundTrip attaches a freshly signed assertion to the request.
func (t *jwtTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	assertion, err := appJWT(t.appID, t.key)
	if err != nil {
		return nil, err
	}

	// A RoundTripper must not modify the request it is handed.
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+assertion)
	clone.Header.Set("Accept", "application/vnd.github+json")
	return t.base.RoundTrip(clone)
}

// installationTransport authenticates requests as a GitHub App installation,
// renewing the token as it nears expiry.
//
// Installation tokens last one hour. Because this tool makes one API call per
// repository, a scan of a large organization can outlive a single token, so
// renewal has to happen here rather than once at startup.
type installationTransport struct {
	base         http.RoundTripper
	apiBase      string
	appID        int64
	key          *rsa.PrivateKey
	installation int64

	mu      sync.Mutex
	token   string
	expires time.Time
}

// RoundTrip attaches a current installation token to the request.
func (t *installationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := t.currentToken(req.Context())
	if err != nil {
		return nil, err
	}

	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+token)
	return t.base.RoundTrip(clone)
}

// currentToken returns a valid installation token, minting a new one when the
// held token is missing or close to expiry.
func (t *installationTransport) currentToken(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.token != "" && time.Now().Add(tokenRenewMargin).Before(t.expires) {
		return t.token, nil
	}

	assertion, err := appJWT(t.appID, t.key)
	if err != nil {
		return "", err
	}

	endpoint := fmt.Sprintf("%s/app/installations/%d/access_tokens", strings.TrimSuffix(t.apiBase, "/"), t.installation)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("building the installation token request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+assertion)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return "", fmt.Errorf("requesting an installation token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("reading the installation token response: %w", err)
	}
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("requesting an installation token: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("decoding the installation token: %w", err)
	}
	if payload.Token == "" {
		return "", fmt.Errorf("GitHub returned an empty installation token")
	}

	t.token = payload.Token
	t.expires = payload.ExpiresAt
	if t.expires.IsZero() {
		// Fall back to the documented lifetime rather than treating a missing
		// expiry as "never expires".
		t.expires = time.Now().Add(time.Hour)
	}
	return t.token, nil
}

// appJWT builds the short-lived RS256 assertion that authenticates as the App.
func appJWT(appID int64, key *rsa.PrivateKey) (string, error) {
	now := time.Now()
	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	claims := map[string]any{
		"iat": now.Add(-jwtBackdate).Unix(),
		"exp": now.Add(jwtLifetime).Unix(),
		"iss": strconv.FormatInt(appID, 10),
	}

	encodedHeader, err := encodeSegment(header)
	if err != nil {
		return "", err
	}
	encodedClaims, err := encodeSegment(claims)
	if err != nil {
		return "", err
	}

	signingInput := encodedHeader + "." + encodedClaims
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("signing the app assertion: %w", err)
	}

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

// encodeSegment renders one JWT segment as base64url-encoded JSON.
func encodeSegment(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encoding the app assertion: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
