package auth

import (
	"context"
	"os"
)

// envToken names the environment variable holding a ready-made token.
const envToken = "GITHUB_TOKEN"

// envCredential builds a credential from the GITHUB_TOKEN environment
// variable. The token may be a personal access token, a GitHub App
// installation token, or an OAuth user access token; all three are sent the
// same way and the API distinguishes them server side.
func envCredential(ctx context.Context) (*Credential, error) {
	token := os.Getenv(envToken)
	if token == "" {
		return nil, notConfigured("%s is not set", envToken)
	}

	return &Credential{
		Client: staticClient(ctx, token),
		Source: envToken,
	}, nil
}
