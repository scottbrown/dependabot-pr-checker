package auth

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/endpoints"
)

const (
	// envClientID overrides the client ID compiled into the binary, for users
	// who register their own GitHub App or OAuth App.
	envClientID = "DEPENDABOT_PR_CHECKER_CLIENT_ID"

	// envScopes requests OAuth scopes during the device flow. A GitHub App
	// needs none, because its permissions come from the installation, but an
	// OAuth App does; 'repo,read:org' covers what this tool reads.
	envScopes = "DEPENDABOT_PR_CHECKER_OAUTH_SCOPES"
)

// clientID is the client ID used for the device flow. It is set at build time
// with -ldflags. This is deliberately not a secret: the device flow exists
// precisely so that a distributed client does not have to hold one.
var clientID string

// deviceEndpoint is the OAuth endpoint driving the device flow. It is a
// variable so that tests can point it at a local server.
var deviceEndpoint = endpoints.GitHub

// ClientID returns the configured device flow client ID, preferring the
// environment over the value compiled into the binary.
func ClientID() string {
	if id := strings.TrimSpace(os.Getenv(envClientID)); id != "" {
		return id
	}
	return strings.TrimSpace(clientID)
}

// oauthConfig builds the device flow configuration.
func oauthConfig() (*oauth2.Config, error) {
	id := ClientID()
	if id == "" {
		return nil, fmt.Errorf("no OAuth client ID configured; set %s to the client ID of a GitHub App with device flow enabled", envClientID)
	}

	var scopes []string
	if raw := strings.TrimSpace(os.Getenv(envScopes)); raw != "" {
		for _, scope := range strings.Split(raw, ",") {
			if scope = strings.TrimSpace(scope); scope != "" {
				scopes = append(scopes, scope)
			}
		}
	}

	return &oauth2.Config{
		ClientID: id,
		Endpoint: deviceEndpoint,
		Scopes:   scopes,
	}, nil
}

// Login runs the OAuth device flow and caches the resulting token. It returns
// the location of the cache so callers can tell the user where the credential
// now lives.
func Login(ctx context.Context, out io.Writer) (string, error) {
	cfg, err := oauthConfig()
	if err != nil {
		return "", err
	}

	auth, err := cfg.DeviceAuth(ctx)
	if err != nil {
		return "", fmt.Errorf("requesting a device code: %w", err)
	}

	fmt.Fprintf(out, "Copy this one-time code: %s\n", auth.UserCode)
	fmt.Fprintf(out, "Then open %s and enter it.\n\n", auth.VerificationURI)
	fmt.Fprintln(out, "Waiting for authorization...")

	// DeviceAccessToken polls until the user authorizes the request, honouring
	// the interval the server asked for and backing off further if it responds
	// with slow_down.
	token, err := cfg.DeviceAccessToken(ctx, auth)
	if err != nil {
		return "", fmt.Errorf("waiting for authorization: %w", err)
	}

	if err := saveToken(token); err != nil {
		return "", fmt.Errorf("caching the token: %w", err)
	}

	path, err := tokenPath()
	if err != nil {
		return "", err
	}
	return path, nil
}

// Logout discards the cached OAuth token.
func Logout() (bool, error) {
	return deleteToken()
}

// oauthCredential builds a credential from the cached device flow token.
func oauthCredential(ctx context.Context) (*Credential, error) {
	token, err := loadToken()
	if err != nil {
		return nil, err
	}

	// A token that has expired with no way to renew it is worthless, and
	// reporting that as unconfigured lets resolution fall through to another
	// source instead of failing the run outright.
	if token.RefreshToken == "" && !token.Expiry.IsZero() && time.Now().After(token.Expiry) {
		return nil, notConfigured("cached login expired; run 'dependabot-pr-checker login' again")
	}

	cfg, err := oauthConfig()
	if err != nil {
		// Without a client ID the token cannot be refreshed, but an
		// unexpired one is still perfectly usable on its own.
		if token.Expiry.IsZero() || time.Now().Before(token.Expiry) {
			return &Credential{
				Client: staticClient(ctx, token.AccessToken),
				Source: "cached OAuth token",
			}, nil
		}
		return nil, err
	}

	source := &persistingSource{source: cfg.TokenSource(ctx, token), last: token.AccessToken}
	return &Credential{
		Client: oauth2.NewClient(ctx, source),
		Source: "cached OAuth token (device flow)",
	}, nil
}

// persistingSource writes refreshed tokens back to the cache.
//
// GitHub App user access tokens expire after a few hours and are issued with a
// refresh token. The underlying source renews them transparently but does not
// persist the result, which would force a fresh login on every run once the
// first token lapsed.
type persistingSource struct {
	source oauth2.TokenSource

	mu   sync.Mutex
	last string
}

// Token returns a valid token, caching it whenever it has been renewed.
func (s *persistingSource) Token() (*oauth2.Token, error) {
	token, err := s.source.Token()
	if err != nil {
		return nil, fmt.Errorf("refreshing the cached token (run 'dependabot-pr-checker login' again): %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if token.AccessToken != s.last {
		s.last = token.AccessToken
		if err := saveToken(token); err != nil {
			// The token itself is fine; only the cache write failed. Warn and
			// carry on rather than abandoning a working credential.
			fmt.Fprintf(os.Stderr, "Warning: could not cache the refreshed token: %v\n", err)
		}
	}
	return token, nil
}
