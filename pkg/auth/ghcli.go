package auth

import (
	"context"
	"errors"
	"os/exec"
	"strings"
)

// ghCredential borrows the token held by the GitHub CLI.
//
// The GitHub CLI authenticates by OAuth device flow, so the token it stores is
// normally a user access token rather than a personal access token, which
// makes it usable in organizations that prohibit personal access tokens. A
// user who instead ran 'gh auth login --with-token' with a personal access
// token gets that token back and is no better off.
func ghCredential(ctx context.Context) (*Credential, error) {
	path, err := exec.LookPath("gh")
	if err != nil {
		return nil, notConfigured("GitHub CLI not installed")
	}

	out, err := exec.CommandContext(ctx, path, "auth", "token").Output()
	if err != nil {
		// A non-zero exit almost always means the CLI is installed but not
		// logged in, which is a reason to try the next source rather than an
		// error worth stopping for. The CLI diagnoses itself on stderr, so
		// repeat what it said: a common cause is that its only credential was
		// GITHUB_TOKEN, which this tool was asked to ignore, and that is
		// invisible from the generic reason alone.
		if reason := ghFailureReason(err); reason != "" {
			return nil, notConfigured("GitHub CLI is not authenticated: %s", reason)
		}
		return nil, notConfigured("GitHub CLI is not authenticated")
	}

	token := strings.TrimSpace(string(out))
	if token == "" {
		return nil, notConfigured("GitHub CLI returned no token")
	}

	return &Credential{
		Client: staticClient(ctx, token),
		Source: "GitHub CLI (gh auth token)",
	}, nil
}

// ghFailureReason picks the first non-empty line the GitHub CLI wrote to
// stderr before exiting. Only the first line is kept because the CLI tends to
// follow its diagnosis with multi-line remediation advice that would swamp a
// one-line error.
func ghFailureReason(err error) string {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return ""
	}

	for _, line := range strings.Split(string(exitErr.Stderr), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}

	return ""
}
