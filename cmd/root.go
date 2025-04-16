package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/kohofinancial/dependabot-pr-checker/pkg/github"
	"github.com/spf13/cobra"
)

var (
	organization string
	maxAge       int
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "dependabot-pr-checker",
	Short: "Check for old Dependabot PRs in production repositories",
	Long: `This tool checks for Dependabot pull requests older than a specified age
in production repositories of a GitHub organization.

A repository is considered "production" if it has the topic 'business-critical-yes'.
The tool requires a GitHub token to be set in the GITHUB_TOKEN environment variable.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		token := os.Getenv("GITHUB_TOKEN")
		if token == "" {
			return fmt.Errorf("GITHUB_TOKEN environment variable is not set")
		}

		client, err := github.NewClient(token)
		if err != nil {
			return fmt.Errorf("failed to create GitHub client: %w", err)
		}

		repos, err := client.GetProductionRepos(organization)
		if err != nil {
			return fmt.Errorf("failed to get production repositories: %w", err)
		}

		maxAgeDuration := time.Duration(maxAge) * 24 * time.Hour
		reposWithOldPRs, err := client.CheckForOldDependabotPRs(repos, maxAgeDuration)
		if err != nil {
			return fmt.Errorf("failed to check for old Dependabot PRs: %w", err)
		}

		if len(reposWithOldPRs) == 0 {
			fmt.Println("No repositories found with Dependabot PRs older than", maxAge, "days")
			return nil
		}

		fmt.Println("Repositories with Dependabot PRs older than", maxAge, "days:")
		for _, repo := range reposWithOldPRs {
			fmt.Println("-", repo)
		}

		return nil
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.Flags().StringVarP(&organization, "organization", "o", "", "GitHub organization to check (required)")
	rootCmd.Flags().IntVar(&maxAge, "max-age", 30, "Maximum age of Dependabot PRs in days")

	rootCmd.MarkFlagRequired("organization")
}
