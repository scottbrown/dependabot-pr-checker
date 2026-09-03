package auth

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// testKey generates an RSA key for signing test assertions.
func testKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return key
}

// pkcs1PEM encodes a key the way GitHub distributes App private keys.
func pkcs1PEM(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()

	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
}

// ed25519TestKey generates a key that is valid PKCS#8 but unusable for a
// GitHub App, which signs with RS256.
func ed25519TestKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()

	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate Ed25519 key: %v", err)
	}
	return key
}

// pkcs8PEM encodes a key the way it often comes back after passing through
// other tooling.
func pkcs8PEM(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()

	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal PKCS#8 key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

// useAPIBase points App authentication at a local server.
func useAPIBase(t *testing.T, base string) {
	t.Helper()

	previous := appAPIBase
	appAPIBase = base
	t.Cleanup(func() { appAPIBase = previous })
}

func TestParsePrivateKey(t *testing.T) {
	key := testKey(t)

	tests := []struct {
		name        string
		data        []byte
		expectError bool
	}{
		{name: "pkcs1", data: pkcs1PEM(t, key)},
		{name: "pkcs8", data: pkcs8PEM(t, key)},
		{name: "surrounding_whitespace_is_tolerated", data: []byte("\n  " + string(pkcs1PEM(t, key)) + "  \n")},
		{name: "not_pem", data: []byte("clearly not a key"), expectError: true},
		{name: "empty", data: nil, expectError: true},
		{
			name:        "pem_with_unusable_contents",
			data:        pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: []byte("garbage")}),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := parsePrivateKey(tt.data)
			if (err != nil) != tt.expectError {
				t.Fatalf("parsePrivateKey() error = %v, expectError %v", err, tt.expectError)
			}
			if tt.expectError {
				return
			}
			if !parsed.Equal(key) {
				t.Error("parsePrivateKey() returned a different key than was encoded")
			}
		})
	}
}

func TestParsePrivateKeyRejectsNonRSAKey(t *testing.T) {
	// An Ed25519 key is valid PKCS#8 but useless for a GitHub App, which signs
	// with RS256. The error should say so rather than panic on a type
	// assertion.
	der, err := x509.MarshalPKCS8PrivateKey(ed25519TestKey(t))
	if err != nil {
		t.Fatalf("marshal PKCS#8 key: %v", err)
	}

	_, err = parsePrivateKey(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	if err == nil {
		t.Fatal("parsePrivateKey() expected an error for a non-RSA key, got none")
	}
	if !strings.Contains(err.Error(), "RSA") {
		t.Errorf("parsePrivateKey() error = %v, want it to mention RSA", err)
	}
}

func TestAppJWT(t *testing.T) {
	key := testKey(t)

	assertion, err := appJWT(4242, key)
	if err != nil {
		t.Fatalf("appJWT() unexpected error: %v", err)
	}

	parts := strings.Split(assertion, ".")
	if len(parts) != 3 {
		t.Fatalf("appJWT() produced %d segments, want 3", len(parts))
	}

	// GitHub rejects an assertion whose signature does not verify, so check it
	// here rather than discovering it against the live API.
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], signature); err != nil {
		t.Errorf("appJWT() signature does not verify: %v", err)
	}

	var header map[string]string
	decodeSegment(t, parts[0], &header)
	if header["alg"] != "RS256" {
		t.Errorf("appJWT() alg = %q, want RS256", header["alg"])
	}
	if header["typ"] != "JWT" {
		t.Errorf("appJWT() typ = %q, want JWT", header["typ"])
	}

	var claims struct {
		Iat int64  `json:"iat"`
		Exp int64  `json:"exp"`
		Iss string `json:"iss"`
	}
	decodeSegment(t, parts[1], &claims)

	if claims.Iss != "4242" {
		t.Errorf("appJWT() iss = %q, want %q", claims.Iss, "4242")
	}
	// GitHub rejects assertions issued in the future, so iat is backdated.
	if now := time.Now().Unix(); claims.Iat > now {
		t.Errorf("appJWT() iat = %d, want it at or before %d", claims.Iat, now)
	}
	// GitHub also rejects a lifetime beyond ten minutes.
	if lifetime := claims.Exp - claims.Iat; lifetime > int64((10 * time.Minute).Seconds()) {
		t.Errorf("appJWT() lifetime = %ds, want at most 600s", lifetime)
	}
}

func TestInstallationTransportReusesUnexpiredToken(t *testing.T) {
	var mints int32
	server := installationTokenServer(t, &mints, time.Now().Add(time.Hour))

	transport := &installationTransport{
		base:         http.DefaultTransport,
		apiBase:      server.URL,
		appID:        1,
		key:          testKey(t),
		installation: 99,
	}

	for i := 0; i < 3; i++ {
		token, err := transport.currentToken(context.Background())
		if err != nil {
			t.Fatalf("currentToken() unexpected error: %v", err)
		}
		if token != "installation-token" {
			t.Fatalf("currentToken() = %q, want %q", token, "installation-token")
		}
	}

	if got := atomic.LoadInt32(&mints); got != 1 {
		t.Errorf("currentToken() minted %d tokens, want 1", got)
	}
}

func TestInstallationTransportRenewsNearExpiry(t *testing.T) {
	var mints int32
	// A token this close to expiry falls inside the renewal margin, which is
	// what protects a long scan from a token lapsing mid-run.
	server := installationTokenServer(t, &mints, time.Now().Add(30*time.Second))

	transport := &installationTransport{
		base:         http.DefaultTransport,
		apiBase:      server.URL,
		appID:        1,
		key:          testKey(t),
		installation: 99,
	}

	for i := 0; i < 2; i++ {
		if _, err := transport.currentToken(context.Background()); err != nil {
			t.Fatalf("currentToken() unexpected error: %v", err)
		}
	}

	if got := atomic.LoadInt32(&mints); got != 2 {
		t.Errorf("currentToken() minted %d tokens, want 2", got)
	}
}

func TestInstallationTransportReportsMintFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"message":"Resource not accessible by integration"}`)
	}))
	t.Cleanup(server.Close)

	transport := &installationTransport{
		base:         http.DefaultTransport,
		apiBase:      server.URL,
		appID:        1,
		key:          testKey(t),
		installation: 99,
	}

	_, err := transport.currentToken(context.Background())
	if err == nil {
		t.Fatal("currentToken() expected an error on a rejected mint, got none")
	}
	// The API's own explanation is the most useful part of the message, so it
	// should survive into the error the user sees.
	if !strings.Contains(err.Error(), "not accessible by integration") {
		t.Errorf("currentToken() error = %v, want it to include GitHub's message", err)
	}
}

func TestInstallationTransportAuthenticatesRequests(t *testing.T) {
	var mints int32
	tokenServer := installationTokenServer(t, &mints, time.Now().Add(time.Hour))

	var seen string
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		io.WriteString(w, `{}`)
	}))
	t.Cleanup(apiServer.Close)

	client := &http.Client{Transport: &installationTransport{
		base:         http.DefaultTransport,
		apiBase:      tokenServer.URL,
		appID:        1,
		key:          testKey(t),
		installation: 99,
	}}

	req, err := http.NewRequest(http.MethodGet, apiServer.URL+"/orgs/testorg/repos", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do() unexpected error: %v", err)
	}
	resp.Body.Close()

	if want := "Bearer installation-token"; seen != want {
		t.Errorf("Authorization header = %q, want %q", seen, want)
	}
	// A RoundTripper must not mutate the request it was handed.
	if req.Header.Get("Authorization") != "" {
		t.Error("RoundTrip() modified the caller's request")
	}
}

func TestAppCredentialDiscoversInstallation(t *testing.T) {
	isolate(t)

	var mints int32
	mux := http.NewServeMux()
	mux.HandleFunc("/orgs/testorg/installation", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") {
			t.Errorf("installation lookup Authorization = %q, want a bearer assertion", got)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":31337}`)
	})
	mux.HandleFunc("/app/installations/31337/access_tokens", installationTokenHandler(&mints, time.Now().Add(time.Hour)))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	useAPIBase(t, server.URL)

	keyPath := filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(keyPath, pkcs1PEM(t, testKey(t)), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	t.Setenv(envAppID, "4242")
	t.Setenv(envAppKeyPath, keyPath)

	cred, err := appCredential(context.Background(), "testorg")
	if err != nil {
		t.Fatalf("appCredential() unexpected error: %v", err)
	}
	if cred.Client == nil {
		t.Error("appCredential() returned no HTTP client")
	}
	// Naming the installation in the source makes a run against the wrong
	// organization obvious in verbose output.
	if !strings.Contains(cred.Source, "31337") {
		t.Errorf("appCredential() source = %q, want it to name installation 31337", cred.Source)
	}
}

func TestAppCredentialHonoursPinnedInstallation(t *testing.T) {
	isolate(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request to %s; a pinned installation needs no discovery", r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	useAPIBase(t, server.URL)

	t.Setenv(envAppID, "4242")
	t.Setenv(envAppKey, string(pkcs1PEM(t, testKey(t))))
	t.Setenv(envAppInstallation, "555")

	cred, err := appCredential(context.Background(), "testorg")
	if err != nil {
		t.Fatalf("appCredential() unexpected error: %v", err)
	}
	if !strings.Contains(cred.Source, "555") {
		t.Errorf("appCredential() source = %q, want it to name installation 555", cred.Source)
	}
}

func TestAppCredentialWithoutAppIDIsNotConfigured(t *testing.T) {
	isolate(t)

	_, err := appCredential(context.Background(), "testorg")
	if !errors.Is(err, ErrNotConfigured) {
		t.Errorf("appCredential() error = %v, want ErrNotConfigured", err)
	}
}

func TestAppCredentialRejectsMalformedInstallationID(t *testing.T) {
	isolate(t)
	t.Setenv(envAppID, "4242")
	t.Setenv(envAppKey, string(pkcs1PEM(t, testKey(t))))
	t.Setenv(envAppInstallation, "not-a-number")

	_, err := appCredential(context.Background(), "testorg")
	if err == nil {
		t.Fatal("appCredential() expected an error for a malformed installation ID, got none")
	}
	if !strings.Contains(err.Error(), envAppInstallation) {
		t.Errorf("appCredential() error = %v, want it to name %s", err, envAppInstallation)
	}
}

func TestAppCredentialNeedsOrgToDiscoverInstallation(t *testing.T) {
	isolate(t)
	t.Setenv(envAppID, "4242")
	t.Setenv(envAppKey, string(pkcs1PEM(t, testKey(t))))

	_, err := appCredential(context.Background(), "")
	if err == nil {
		t.Fatal("appCredential() expected an error without an organization, got none")
	}
	if !strings.Contains(err.Error(), envAppInstallation) {
		t.Errorf("appCredential() error = %v, want it to suggest %s", err, envAppInstallation)
	}
}

func TestAppCredentialReportsMissingInstallation(t *testing.T) {
	isolate(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"message":"Not Found"}`)
	}))
	t.Cleanup(server.Close)
	useAPIBase(t, server.URL)

	t.Setenv(envAppID, "4242")
	t.Setenv(envAppKey, string(pkcs1PEM(t, testKey(t))))

	_, err := appCredential(context.Background(), "testorg")
	if err == nil {
		t.Fatal("appCredential() expected an error when the app is not installed, got none")
	}
	// "Not Found" alone gives the user nothing to act on; the message should
	// point at the likely cause.
	if !strings.Contains(err.Error(), "installed") {
		t.Errorf("appCredential() error = %v, want it to mention installation", err)
	}
}

// installationTokenServer serves installation token mints and counts them.
func installationTokenServer(t *testing.T, mints *int32, expiry time.Time) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(installationTokenHandler(mints, expiry)))
	t.Cleanup(server.Close)
	return server
}

// installationTokenHandler responds to an installation token request.
func installationTokenHandler(mints *int32, expiry time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		atomic.AddInt32(mints, 1)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"token":"installation-token","expires_at":%q}`, expiry.UTC().Format(time.RFC3339))
	}
}

// decodeSegment decodes one base64url-encoded JWT segment.
func decodeSegment(t *testing.T, segment string, into any) {
	t.Helper()

	data, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		t.Fatalf("decode JWT segment: %v", err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		t.Fatalf("unmarshal JWT segment: %v", err)
	}
}
