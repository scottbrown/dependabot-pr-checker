// Package auth resolves credentials for the GitHub API.
//
// A personal access token is the simplest credential, but organizations can
// prohibit both classic and fine-grained tokens by policy. Those policies do
// not govern GitHub App tokens or OAuth user access tokens, so this package
// supports several credential sources and chooses among them at runtime.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
)

// Method identifies which credential source to use.
type Method string

const (
	// MethodAuto tries every source in turn.
	MethodAuto Method = "auto"
	// MethodEnv uses the token in the GITHUB_TOKEN environment variable.
	MethodEnv Method = "env"
	// MethodApp authenticates as a GitHub App installation.
	MethodApp Method = "app"
	// MethodOAuth uses the token cached by a previous device flow login.
	MethodOAuth Method = "oauth"
	// MethodGH borrows the token held by the GitHub CLI.
	MethodGH Method = "gh"
)

// autoOrder lists the sources MethodAuto tries, in order. Explicitly
// configured credentials come before ambient ones so that a deliberate choice
// always wins over whatever happens to be installed on the machine.
var autoOrder = []Method{MethodEnv, MethodApp, MethodOAuth, MethodGH}

// ErrNotConfigured reports that a credential source has nothing to offer and
// that resolution should continue with the next source.
var ErrNotConfigured = errors.New("not configured")

// notConfigured builds an ErrNotConfigured error carrying only the given
// reason. Wrapping the sentinel with %w instead would append "not configured"
// to every message, which reads as noise next to a reason that already says as
// much.
func notConfigured(format string, args ...any) error {
	return &notConfiguredError{reason: fmt.Sprintf(format, args...)}
}

// notConfiguredError is a reason that matches ErrNotConfigured.
type notConfiguredError struct {
	reason string
}

// Error returns the reason alone.
func (e *notConfiguredError) Error() string { return e.reason }

// Unwrap exposes the sentinel so that errors.Is recognises this error.
func (e *notConfiguredError) Unwrap() error { return ErrNotConfigured }

// Credential is an authenticated HTTP client and a description of the source
// that produced it.
//
// Sources return a client rather than a token string because some credentials
// expire partway through a run. A GitHub App installation token lives for one
// hour, which a scan of a large organization can outlast, so renewal has to
// happen inside the transport.
type Credential struct {
	Client *http.Client
	Source string
}

// ParseMethod validates a user-supplied authentication method name.
func ParseMethod(s string) (Method, error) {
	method := Method(strings.ToLower(strings.TrimSpace(s)))
	switch method {
	case MethodAuto, MethodEnv, MethodApp, MethodOAuth, MethodGH:
		return method, nil
	}
	return "", fmt.Errorf("unsupported auth method: %s (must be auto, env, app, oauth, or gh)", s)
}

// Resolve returns a credential for the requested method. The organization is
// used to discover the relevant installation when authenticating as a GitHub
// App, and is ignored by the other methods.
func Resolve(ctx context.Context, method Method, org string) (*Credential, error) {
	if method != MethodAuto {
		cred, err := resolveOne(ctx, method, org)
		if errors.Is(err, ErrNotConfigured) {
			return nil, fmt.Errorf("%s authentication is unavailable: %w", method, err)
		}
		if err != nil {
			return nil, fmt.Errorf("%s authentication failed: %w", method, err)
		}
		return cred, nil
	}

	var skipped []string
	for _, candidate := range autoOrder {
		cred, err := resolveOne(ctx, candidate, org)
		switch {
		case err == nil:
			return cred, nil
		case errors.Is(err, ErrNotConfigured):
			skipped = append(skipped, fmt.Sprintf("  %-5s %v", candidate, err))
		default:
			// A source that is configured but broken is a real problem. Falling
			// through to a weaker credential would hide a misconfiguration and
			// silently change which repositories the run can see.
			return nil, fmt.Errorf("%s authentication failed: %w", candidate, err)
		}
	}

	return nil, fmt.Errorf("no GitHub credentials found:\n%s\n\nSet GITHUB_TOKEN, configure a GitHub App, or run 'dependabot-pr-checker login'", strings.Join(skipped, "\n"))
}

// resolveOne attempts a single credential source.
func resolveOne(ctx context.Context, method Method, org string) (*Credential, error) {
	switch method {
	case MethodEnv:
		return envCredential(ctx)
	case MethodApp:
		return appCredential(ctx, org)
	case MethodOAuth:
		return oauthCredential(ctx)
	case MethodGH:
		return ghCredential(ctx)
	}
	return nil, fmt.Errorf("unsupported auth method: %s", method)
}

// staticClient returns a client that sends a fixed bearer token.
func staticClient(ctx context.Context, token string) *http.Client {
	return oauth2.NewClient(ctx, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token}))
}
