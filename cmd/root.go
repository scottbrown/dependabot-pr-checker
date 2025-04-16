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
	verbose      bool
	version      string // Git branch, set during build by -ldflags
	build        string // Git short ref, set during build by -ldflags
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

		repos, err := client.GetProductionRepos(organization, verbose)
		if err != nil {
			return fmt.Errorf("failed to get production repositories: %w", err)
		}

		maxAgeDuration := time.Duration(maxAge) * 24 * time.Hour
		reposWithOldPRs, err := client.CheckForOldDependabotPRs(repos, maxAgeDuration, verbose)
		if err != nil {
			return fmt.Errorf("failed to check for old Dependabot PRs: %w", err)
		}

		totalRepos := len(repos)
		reposWithOldPRsCount := len(reposWithOldPRs)
		percentage := 0.0
		if totalRepos > 0 {
			percentage = float64(reposWithOldPRsCount) / float64(totalRepos) * 100
		}

		if reposWithOldPRsCount == 0 {
			fmt.Printf("No repositories found with Dependabot PRs older than %d days (0.0%% of %d production repositories)\n", maxAge, totalRepos)
			return nil
		}

		fmt.Printf("%d of %d production repositories (%.1f%%) have Dependabot PRs older than %d days:\n",
			reposWithOldPRsCount, totalRepos, percentage, maxAge)
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
	rootCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show verbose output with progress bars")

	// Set version string in format "BRANCH (SHORT_REF)"
	if version != "" && build != "" {
		rootCmd.Version = version + " (" + build + ")"
	}

	rootCmd.MarkFlagRequired("organization")
}
