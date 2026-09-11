package auth

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// isolate points the token cache at a temporary directory and clears every
// credential the resolver consults, so tests neither read the developer's real
// environment nor write to their real config directory.
func isolate(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	t.Setenv(envConfigDir, dir)
	for _, name := range []string{
		envToken,
		envAppID,
		envAppKey,
		envAppKeyPath,
		envAppInstallation,
		envClientID,
		envScopes,
	} {
		t.Setenv(name, "")
	}

	// An empty PATH makes the GitHub CLI undiscoverable, keeping results
	// independent of whether the host happens to have gh installed.
	t.Setenv("PATH", "")

	return dir
}

func TestParseMethod(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		want        Method
		expectError bool
	}{
		{name: "auto", input: "auto", want: MethodAuto},
		{name: "env", input: "env", want: MethodEnv},
		{name: "app", input: "app", want: MethodApp},
		{name: "oauth", input: "oauth", want: MethodOAuth},
		{name: "gh", input: "gh", want: MethodGH},
		{name: "uppercase_is_accepted", input: "ENV", want: MethodEnv},
		{name: "surrounding_space_is_ignored", input: "  app  ", want: MethodApp},
		{name: "unknown_method_is_rejected", input: "carrier-pigeon", expectError: true},
		{name: "empty_method_is_rejected", input: "", expectError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseMethod(tt.input)
			if (err != nil) != tt.expectError {
				t.Fatalf("ParseMethod(%q) error = %v, expectError %v", tt.input, err, tt.expectError)
			}
			if !tt.expectError && got != tt.want {
				t.Errorf("ParseMethod(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestResolveUsesEnvToken(t *testing.T) {
	isolate(t)
	t.Setenv(envToken, "token-from-env")

	cred, err := Resolve(context.Background(), MethodEnv, "testorg")
	if err != nil {
		t.Fatalf("Resolve() unexpected error: %v", err)
	}
	if cred.Client == nil {
		t.Error("Resolve() returned no HTTP client")
	}
	if cred.Source != envToken {
		t.Errorf("Resolve() source = %q, want %q", cred.Source, envToken)
	}
}

func TestResolveAutoPrefersEnvToken(t *testing.T) {
	isolate(t)
	t.Setenv(envToken, "token-from-env")

	cred, err := Resolve(context.Background(), MethodAuto, "testorg")
	if err != nil {
		t.Fatalf("Resolve() unexpected error: %v", err)
	}
	if cred.Source != envToken {
		t.Errorf("Resolve() source = %q, want %q", cred.Source, envToken)
	}
}

func TestResolveExplicitMethodReportsUnavailable(t *testing.T) {
	isolate(t)

	_, err := Resolve(context.Background(), MethodEnv, "testorg")
	if err == nil {
		t.Fatal("Resolve() expected an error when GITHUB_TOKEN is unset, got none")
	}
	if !strings.Contains(err.Error(), envToken) {
		t.Errorf("Resolve() error = %v, want it to name %s", err, envToken)
	}
}

func TestResolveAutoReportsEverySkippedSource(t *testing.T) {
	isolate(t)

	_, err := Resolve(context.Background(), MethodAuto, "testorg")
	if err == nil {
		t.Fatal("Resolve() expected an error when no credentials exist, got none")
	}

	// The message is the only guidance a stuck user gets, so it should name
	// each source that was tried along with the remedy.
	for _, want := range []string{"no GitHub credentials found", string(MethodEnv), string(MethodApp), string(MethodOAuth), string(MethodGH), "login"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Resolve() error = %v, want it to mention %q", err, want)
		}
	}
}

func TestResolveAutoFailsOnMisconfiguredSource(t *testing.T) {
	isolate(t)
	// A source that is configured but unusable must not be skipped: falling
	// through to another credential would hide the mistake and silently change
	// which repositories the run can see.
	t.Setenv(envAppID, "not-a-number")

	_, err := Resolve(context.Background(), MethodAuto, "testorg")
	if err == nil {
		t.Fatal("Resolve() expected an error for a malformed app ID, got none")
	}
	if !strings.Contains(err.Error(), envAppID) {
		t.Errorf("Resolve() error = %v, want it to name %s", err, envAppID)
	}
}

func TestResolveAutoFailsWhenAppKeyMissing(t *testing.T) {
	isolate(t)
	t.Setenv(envAppID, "12345")

	_, err := Resolve(context.Background(), MethodAuto, "testorg")
	if err == nil {
		t.Fatal("Resolve() expected an error when the app key is absent, got none")
	}
	if !strings.Contains(err.Error(), envAppKey) {
		t.Errorf("Resolve() error = %v, want it to name %s", err, envAppKey)
	}
}

func TestResolveRejectsUnknownMethod(t *testing.T) {
	isolate(t)

	if _, err := Resolve(context.Background(), Method("smoke-signal"), "testorg"); err == nil {
		t.Fatal("Resolve() expected an error for an unknown method, got none")
	}
}

func TestGhCredentialReportsNotConfiguredWithoutCLI(t *testing.T) {
	isolate(t)

	_, err := ghCredential(context.Background())
	if !errors.Is(err, ErrNotConfigured) {
		t.Errorf("ghCredential() error = %v, want ErrNotConfigured", err)
	}
}

func TestGhFailureReason(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "single line",
			err:  exitErrorWithStderr("no oauth token found for github.com\n"),
			want: "no oauth token found for github.com",
		},
		{
			name: "keeps only the first line of remediation advice",
			err:  exitErrorWithStderr("you are not logged in\nTo log in, run: gh auth login\n"),
			want: "you are not logged in",
		},
		{
			name: "skips leading blank lines",
			err:  exitErrorWithStderr("\n  \nno oauth token found\n"),
			want: "no oauth token found",
		},
		{
			name: "silent failure",
			err:  exitErrorWithStderr(""),
			want: "",
		},
		{
			name: "not an exit error",
			err:  errors.New("context deadline exceeded"),
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ghFailureReason(tt.err); got != tt.want {
				t.Errorf("ghFailureReason() = %q, want %q", got, tt.want)
			}
		})
	}
}

// exitErrorWithStderr stands in for what exec.Cmd.Output() returns when the
// command exits non-zero. Only Stderr is populated because that is the sole
// field ghFailureReason reads.
func exitErrorWithStderr(stderr string) error {
	return &exec.ExitError{Stderr: []byte(stderr)}
}
