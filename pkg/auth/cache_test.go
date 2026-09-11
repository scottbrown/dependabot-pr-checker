package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestSaveAndLoadToken(t *testing.T) {
	isolate(t)

	expiry := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	want := &oauth2.Token{
		AccessToken:  "access-token",
		TokenType:    "bearer",
		RefreshToken: "refresh-token",
		Expiry:       expiry,
	}

	if err := saveToken(want); err != nil {
		t.Fatalf("saveToken() unexpected error: %v", err)
	}

	got, err := loadToken()
	if err != nil {
		t.Fatalf("loadToken() unexpected error: %v", err)
	}
	if got.AccessToken != want.AccessToken {
		t.Errorf("loadToken() access token = %q, want %q", got.AccessToken, want.AccessToken)
	}
	if got.TokenType != want.TokenType {
		t.Errorf("loadToken() token type = %q, want %q", got.TokenType, want.TokenType)
	}
	if got.RefreshToken != want.RefreshToken {
		t.Errorf("loadToken() refresh token = %q, want %q", got.RefreshToken, want.RefreshToken)
	}
	if !got.Expiry.Equal(expiry) {
		t.Errorf("loadToken() expiry = %v, want %v", got.Expiry, expiry)
	}
}

func TestSaveTokenRestrictsPermissions(t *testing.T) {
	dir := isolate(t)

	if err := saveToken(&oauth2.Token{AccessToken: "access-token"}); err != nil {
		t.Fatalf("saveToken() unexpected error: %v", err)
	}

	// The cached token is a live credential, so it must not be readable by
	// other users on a shared machine.
	info, err := os.Stat(filepath.Join(dir, "token.json"))
	if err != nil {
		t.Fatalf("stat token file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != tokenFileMode {
		t.Errorf("token file mode = %o, want %o", perm, tokenFileMode)
	}
}

func TestSaveTokenReplacesExistingToken(t *testing.T) {
	isolate(t)

	if err := saveToken(&oauth2.Token{AccessToken: "first"}); err != nil {
		t.Fatalf("saveToken() unexpected error: %v", err)
	}
	if err := saveToken(&oauth2.Token{AccessToken: "second"}); err != nil {
		t.Fatalf("saveToken() unexpected error on replace: %v", err)
	}

	got, err := loadToken()
	if err != nil {
		t.Fatalf("loadToken() unexpected error: %v", err)
	}
	if got.AccessToken != "second" {
		t.Errorf("loadToken() access token = %q, want %q", got.AccessToken, "second")
	}
}

func TestSaveTokenLeavesNoTemporaryFile(t *testing.T) {
	dir := isolate(t)

	if err := saveToken(&oauth2.Token{AccessToken: "access-token"}); err != nil {
		t.Fatalf("saveToken() unexpected error: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read config dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Errorf("saveToken() left a temporary file behind: %s", entry.Name())
		}
	}
}

func TestLoadTokenWithoutCacheIsNotConfigured(t *testing.T) {
	isolate(t)

	_, err := loadToken()
	if !errors.Is(err, ErrNotConfigured) {
		t.Errorf("loadToken() error = %v, want ErrNotConfigured", err)
	}
}

func TestLoadTokenRejectsCorruptCache(t *testing.T) {
	dir := isolate(t)

	if err := os.WriteFile(filepath.Join(dir, "token.json"), []byte("{not json"), tokenFileMode); err != nil {
		t.Fatalf("write corrupt token: %v", err)
	}

	_, err := loadToken()
	if err == nil {
		t.Fatal("loadToken() expected an error for a corrupt cache, got none")
	}
	// A corrupt cache is not the same as an absent one: it needs the user's
	// attention rather than a silent fallback to another credential.
	if errors.Is(err, ErrNotConfigured) {
		t.Error("loadToken() reported ErrNotConfigured for a corrupt cache, want a hard error")
	}
}

func TestLoadTokenRejectsEmptyAccessToken(t *testing.T) {
	dir := isolate(t)

	if err := os.WriteFile(filepath.Join(dir, "token.json"), []byte(`{"token_type":"bearer"}`), tokenFileMode); err != nil {
		t.Fatalf("write token: %v", err)
	}

	if _, err := loadToken(); err == nil {
		t.Fatal("loadToken() expected an error for a token with no access token, got none")
	}
}

func TestDeleteToken(t *testing.T) {
	isolate(t)

	removed, err := deleteToken()
	if err != nil {
		t.Fatalf("deleteToken() unexpected error: %v", err)
	}
	if removed {
		t.Error("deleteToken() reported a removal when no token was cached")
	}

	if err := saveToken(&oauth2.Token{AccessToken: "access-token"}); err != nil {
		t.Fatalf("saveToken() unexpected error: %v", err)
	}

	removed, err = deleteToken()
	if err != nil {
		t.Fatalf("deleteToken() unexpected error: %v", err)
	}
	if !removed {
		t.Error("deleteToken() reported no removal when a token was cached")
	}
	if _, err := loadToken(); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("loadToken() after delete error = %v, want ErrNotConfigured", err)
	}
}
