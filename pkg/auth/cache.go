package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/oauth2"
)

const (
	// envConfigDir overrides where the cached token is kept.
	envConfigDir = "DEPENDABOT_PR_CHECKER_CONFIG_DIR"

	// tokenFileMode keeps the cached token readable only by its owner.
	tokenFileMode = 0o600
	// configDirMode keeps the containing directory private too.
	configDirMode = 0o700
)

// cachedToken is the on-disk representation of an OAuth token. The oauth2
// token type is not serialised directly because its fields are not all
// exported for JSON and its shape is not part of this tool's file format.
type cachedToken struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Expiry       string `json:"expiry,omitempty"`
}

// configDir returns the directory holding this tool's state.
func configDir() (string, error) {
	if dir := os.Getenv(envConfigDir); dir != "" {
		return dir, nil
	}

	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating user config directory: %w", err)
	}
	return filepath.Join(base, "dependabot-pr-checker"), nil
}

// tokenPath returns the location of the cached OAuth token.
func tokenPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "token.json"), nil
}

// saveToken writes the token to the cache, replacing any existing one.
func saveToken(token *oauth2.Token) error {
	path, err := tokenPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), configDirMode); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	stored := cachedToken{
		AccessToken:  token.AccessToken,
		TokenType:    token.TokenType,
		RefreshToken: token.RefreshToken,
	}
	if !token.Expiry.IsZero() {
		stored.Expiry = token.Expiry.UTC().Format(time.RFC3339)
	}

	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding token: %w", err)
	}

	// Write to a sibling file and rename so that an interrupted write cannot
	// leave a half-written token behind.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), tokenFileMode); err != nil {
		return fmt.Errorf("writing token: %w", err)
	}
	// WriteFile leaves the mode of a pre-existing file alone, so set it
	// explicitly rather than trusting the create mode.
	if err := os.Chmod(tmp, tokenFileMode); err != nil {
		return fmt.Errorf("securing token file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replacing token: %w", err)
	}
	return nil
}

// loadToken reads the cached OAuth token. A missing cache reports
// ErrNotConfigured so that resolution can continue with another source.
func loadToken() (*oauth2.Token, error) {
	path, err := tokenPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, notConfigured("no cached login")
		}
		return nil, fmt.Errorf("reading cached token: %w", err)
	}

	var stored cachedToken
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("cached token is corrupt (delete %s and log in again): %w", path, err)
	}
	if stored.AccessToken == "" {
		return nil, fmt.Errorf("cached token at %s has no access token; log in again", path)
	}

	token := &oauth2.Token{
		AccessToken:  stored.AccessToken,
		TokenType:    stored.TokenType,
		RefreshToken: stored.RefreshToken,
	}
	if stored.Expiry != "" {
		expiry, err := time.Parse(time.RFC3339, stored.Expiry)
		if err != nil {
			return nil, fmt.Errorf("cached token has an unreadable expiry: %w", err)
		}
		token.Expiry = expiry
	}
	return token, nil
}

// deleteToken removes the cached token, reporting whether one was present.
func deleteToken() (bool, error) {
	path, err := tokenPath()
	if err != nil {
		return false, err
	}

	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("removing cached token: %w", err)
	}
	return true, nil
}
