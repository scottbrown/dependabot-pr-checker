package cmd

import (
	"fmt"
	"os"

	"github.com/scottbrown/dependabot-pr-checker/v2/pkg/auth"
	"github.com/spf13/cobra"
)

// loginCmd authenticates interactively using the OAuth device flow.
var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with GitHub using the OAuth device flow",
	Long: `Authenticate with GitHub by entering a one-time code in a browser.

The resulting token is cached so later runs need no interactive step. Device flow
produces an OAuth user access token rather than a personal access token, so it
works in organizations whose policy prohibits personal access tokens.

Set DEPENDABOT_PR_CHECKER_CLIENT_ID to the client ID of a GitHub App that has
device flow enabled, or of an OAuth App. An OAuth App also needs
DEPENDABOT_PR_CHECKER_OAUTH_SCOPES set to 'repo,read:org'; a GitHub App needs no
scopes because its permissions come from the installation.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := auth.Login(cmd.Context(), os.Stdout)
		if err != nil {
			return err
		}

		fmt.Println("\nAuthenticated. Token cached at", path)
		return nil
	},
}

// logoutCmd discards the cached device flow token.
var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Discard the cached OAuth token",
	RunE: func(cmd *cobra.Command, args []string) error {
		removed, err := auth.Logout()
		if err != nil {
			return err
		}

		if !removed {
			fmt.Println("No cached token to remove.")
			return nil
		}
		fmt.Println("Cached token removed.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(logoutCmd)
}
