package auth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// deviceServer stands in for GitHub's device flow endpoints. Authorization
// succeeds after pendingPolls unsuccessful attempts.
func deviceServer(t *testing.T, pendingPolls int32) (*httptest.Server, *int32) {
	t.Helper()

	var polls int32
	mux := http.NewServeMux()

	mux.HandleFunc("/login/device/code", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Form.Get("client_id") == "" {
			http.Error(w, "missing client_id", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		// An interval of one second keeps the polling loop from dominating the
		// test's runtime; the real endpoint asks for five.
		io.WriteString(w, `{"device_code":"device-code","user_code":"ABCD-1234","verification_uri":"https://github.com/login/device","expires_in":300,"interval":1}`)
	})

	mux.HandleFunc("/login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if atomic.AddInt32(&polls, 1) <= pendingPolls {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":"authorization_pending"}`)
			return
		}
		io.WriteString(w, `{"access_token":"user-access-token","token_type":"bearer","refresh_token":"refresh-token","expires_in":28800}`)
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	previous := deviceEndpoint
	deviceEndpoint = oauth2.Endpoint{
		DeviceAuthURL: server.URL + "/login/device/code",
		TokenURL:      server.URL + "/login/oauth/access_token",
		AuthURL:       server.URL + "/login/oauth/authorize",
	}
	t.Cleanup(func() { deviceEndpoint = previous })

	return server, &polls
}

func TestClientIDPrefersEnvironment(t *testing.T) {
	isolate(t)

	previous := clientID
	clientID = "compiled-in-id"
	t.Cleanup(func() { clientID = previous })

	if got := ClientID(); got != "compiled-in-id" {
		t.Errorf("ClientID() = %q, want the compiled-in value", got)
	}

	t.Setenv(envClientID, "env-id")
	if got := ClientID(); got != "env-id" {
		t.Errorf("ClientID() = %q, want the environment value to win", got)
	}
}

func TestOAuthConfigRequiresClientID(t *testing.T) {
	isolate(t)

	previous := clientID
	clientID = ""
	t.Cleanup(func() { clientID = previous })

	_, err := oauthConfig()
	if err == nil {
		t.Fatal("oauthConfig() expected an error without a client ID, got none")
	}
	if !strings.Contains(err.Error(), envClientID) {
		t.Errorf("oauthConfig() error = %v, want it to name %s", err, envClientID)
	}
}

func TestOAuthConfigParsesScopes(t *testing.T) {
	isolate(t)
	t.Setenv(envClientID, "client-id")
	t.Setenv(envScopes, " repo , read:org ,, ")

	cfg, err := oauthConfig()
	if err != nil {
		t.Fatalf("oauthConfig() unexpected error: %v", err)
	}

	want := []string{"repo", "read:org"}
	if len(cfg.Scopes) != len(want) {
		t.Fatalf("oauthConfig() scopes = %v, want %v", cfg.Scopes, want)
	}
	for i, scope := range want {
		if cfg.Scopes[i] != scope {
			t.Errorf("oauthConfig() scope %d = %q, want %q", i, cfg.Scopes[i], scope)
		}
	}
}

func TestOAuthConfigDefaultsToNoScopes(t *testing.T) {
	isolate(t)
	t.Setenv(envClientID, "client-id")

	cfg, err := oauthConfig()
	if err != nil {
		t.Fatalf("oauthConfig() unexpected error: %v", err)
	}
	// A GitHub App derives its access from the installation, so requesting
	// scopes it does not understand would only cause the flow to fail.
	if len(cfg.Scopes) != 0 {
		t.Errorf("oauthConfig() scopes = %v, want none", cfg.Scopes)
	}
}

func TestLoginCachesToken(t *testing.T) {
	isolate(t)
	t.Setenv(envClientID, "client-id")
	deviceServer(t, 0)

	var out strings.Builder
	path, err := Login(context.Background(), &out)
	if err != nil {
		t.Fatalf("Login() unexpected error: %v", err)
	}
	if path == "" {
		t.Error("Login() returned no cache path")
	}

	// The user cannot complete the flow without seeing both the code and where
	// to enter it, so both belong in the output.
	for _, want := range []string{"ABCD-1234", "https://github.com/login/device"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("Login() output = %q, want it to contain %q", out.String(), want)
		}
	}

	token, err := loadToken()
	if err != nil {
		t.Fatalf("loadToken() after Login() unexpected error: %v", err)
	}
	if token.AccessToken != "user-access-token" {
		t.Errorf("cached access token = %q, want %q", token.AccessToken, "user-access-token")
	}
	if token.RefreshToken != "refresh-token" {
		t.Errorf("cached refresh token = %q, want %q", token.RefreshToken, "refresh-token")
	}
}

func TestLoginPollsUntilAuthorized(t *testing.T) {
	isolate(t)
	t.Setenv(envClientID, "client-id")
	_, polls := deviceServer(t, 2)

	if _, err := Login(context.Background(), io.Discard); err != nil {
		t.Fatalf("Login() unexpected error: %v", err)
	}

	if got := atomic.LoadInt32(polls); got != 3 {
		t.Errorf("Login() polled %d times, want 3", got)
	}
}

func TestLoginRequiresClientID(t *testing.T) {
	isolate(t)

	previous := clientID
	clientID = ""
	t.Cleanup(func() { clientID = previous })

	if _, err := Login(context.Background(), io.Discard); err == nil {
		t.Fatal("Login() expected an error without a client ID, got none")
	}
}

func TestOAuthCredentialUsesCachedToken(t *testing.T) {
	isolate(t)
	t.Setenv(envClientID, "client-id")

	if err := saveToken(&oauth2.Token{AccessToken: "cached-token", TokenType: "bearer"}); err != nil {
		t.Fatalf("saveToken() unexpected error: %v", err)
	}

	cred, err := oauthCredential(context.Background())
	if err != nil {
		t.Fatalf("oauthCredential() unexpected error: %v", err)
	}
	if cred.Client == nil {
		t.Error("oauthCredential() returned no HTTP client")
	}
	if !strings.Contains(cred.Source, "OAuth") {
		t.Errorf("oauthCredential() source = %q, want it to mention OAuth", cred.Source)
	}
}

func TestOAuthCredentialWithoutCacheIsNotConfigured(t *testing.T) {
	isolate(t)
	t.Setenv(envClientID, "client-id")

	_, err := oauthCredential(context.Background())
	if !errors.Is(err, ErrNotConfigured) {
		t.Errorf("oauthCredential() error = %v, want ErrNotConfigured", err)
	}
}

func TestOAuthCredentialSkipsUnrenewableExpiredToken(t *testing.T) {
	isolate(t)
	t.Setenv(envClientID, "client-id")

	// Expired with no refresh token, so there is nothing to salvage; another
	// source should get its turn instead of the run failing outright.
	if err := saveToken(&oauth2.Token{AccessToken: "stale-token", Expiry: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatalf("saveToken() unexpected error: %v", err)
	}

	_, err := oauthCredential(context.Background())
	if !errors.Is(err, ErrNotConfigured) {
		t.Errorf("oauthCredential() error = %v, want ErrNotConfigured", err)
	}
}

func TestOAuthCredentialUsesUnexpiredTokenWithoutClientID(t *testing.T) {
	isolate(t)

	previous := clientID
	clientID = ""
	t.Cleanup(func() { clientID = previous })

	// A client ID is only needed to renew a token. An unexpired one still works
	// on its own, so losing the ID should not lock the user out.
	if err := saveToken(&oauth2.Token{AccessToken: "cached-token", Expiry: time.Now().Add(time.Hour)}); err != nil {
		t.Fatalf("saveToken() unexpected error: %v", err)
	}

	cred, err := oauthCredential(context.Background())
	if err != nil {
		t.Fatalf("oauthCredential() unexpected error: %v", err)
	}
	if cred.Client == nil {
		t.Error("oauthCredential() returned no HTTP client")
	}
}

func TestPersistingSourceCachesRefreshedToken(t *testing.T) {
	isolate(t)

	if err := saveToken(&oauth2.Token{AccessToken: "old-token"}); err != nil {
		t.Fatalf("saveToken() unexpected error: %v", err)
	}

	refreshed := &oauth2.Token{AccessToken: "new-token", RefreshToken: "refresh-token"}
	source := &persistingSource{
		source: oauth2.StaticTokenSource(refreshed),
		last:   "old-token",
	}

	got, err := source.Token()
	if err != nil {
		t.Fatalf("Token() unexpected error: %v", err)
	}
	if got.AccessToken != "new-token" {
		t.Errorf("Token() access token = %q, want %q", got.AccessToken, "new-token")
	}

	// Without writing the renewed token back, every run would need a fresh
	// login once the first token lapsed.
	cached, err := loadToken()
	if err != nil {
		t.Fatalf("loadToken() unexpected error: %v", err)
	}
	if cached.AccessToken != "new-token" {
		t.Errorf("cached access token = %q, want %q", cached.AccessToken, "new-token")
	}
}

func TestPersistingSourceLeavesUnchangedTokenAlone(t *testing.T) {
	dir := isolate(t)

	source := &persistingSource{
		source: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "same-token"}),
		last:   "same-token",
	}

	if _, err := source.Token(); err != nil {
		t.Fatalf("Token() unexpected error: %v", err)
	}

	// Nothing was renewed, so nothing should have been written.
	if entries, err := os.ReadDir(dir); err != nil {
		t.Fatalf("read config dir: %v", err)
	} else if len(entries) != 0 {
		t.Errorf("persistingSource wrote %d file(s) for an unchanged token, want none", len(entries))
	}
}
