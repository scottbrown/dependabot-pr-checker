package cmd

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/scottbrown/dependabot-pr-checker/v2/pkg/github"
	"github.com/scottbrown/dependabot-pr-checker/v2/pkg/selector"
	"github.com/spf13/cobra"
)

// RepoWithAge represents a repository with the age of its oldest Dependabot PR
type RepoWithAge struct {
	Name      string  `json:"name"`
	AgeInDays float64 `json:"age_in_days"`
}

// OutputData represents the data structure used for formatted output
type OutputData struct {
	TotalRepos      int           `json:"total_repos"`
	TotalWithOldPRs int           `json:"total_with_old_prs"`
	Percentage      float64       `json:"percentage"`
	MaxAge          int           `json:"max_age"`
	ReposWithOldPRs []RepoWithAge `json:"repos_with_old_prs"`
}

var (
	organization string
	maxAge       int
	verbose      bool
	quiet        bool
	outputFormat string
	sortBy       string
	selectExprs  []string
	version      string // Git branch, set during build by -ldflags
	build        string // Git short ref, set during build by -ldflags
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "dependabot-pr-checker",
	Short: "Check for old Dependabot PRs in production repositories",
	Long: `This tool checks for Dependabot pull requests older than a specified age
in production repositories of a GitHub organization.

By default a repository is considered "production" if it has the topic
'business-critical-yes' or the custom property 'business-critical' set to 'yes'.
Use --select to define your own criteria.

The tool requires a GitHub token to be set in the GITHUB_TOKEN environment variable.
Matching on custom properties additionally requires the token to be able to read
custom properties for the organization.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		token := os.Getenv("GITHUB_TOKEN")
		if token == "" {
			return fmt.Errorf("GITHUB_TOKEN environment variable is not set")
		}

		// Check for mutually exclusive flags
		if verbose && quiet {
			return fmt.Errorf("--verbose and --quiet flags cannot be used together")
		}

		// Validate output format
		outputFormat = strings.ToLower(outputFormat)
		if outputFormat != "text" && outputFormat != "json" && outputFormat != "csv" {
			return fmt.Errorf("unsupported output format: %s (must be text, json, or csv)", outputFormat)
		}

		// Validate sort flag
		sortBy = strings.ToLower(sortBy)
		if sortBy != "name" && sortBy != "age" {
			return fmt.Errorf("unsupported sort option: %s (must be name or age)", sortBy)
		}

		selectors, err := selector.ParseAll(selectExprs)
		if err != nil {
			return err
		}

		client, err := github.NewClient(token)
		if err != nil {
			return fmt.Errorf("failed to create GitHub client: %w", err)
		}

		// Structured output formats imply quiet mode for progress reporting
		showOutput := verbose && outputFormat == "text" && !quiet
		repos, err := client.GetProductionRepos(organization, selectors, showOutput)
		if err != nil {
			return fmt.Errorf("failed to get production repositories: %w", err)
		}

		maxAgeDuration := time.Duration(maxAge) * 24 * time.Hour
		reposWithOldPRs, err := client.CheckForOldDependabotPRs(organization, repos, maxAgeDuration, showOutput)
		if err != nil {
			return fmt.Errorf("failed to check for old Dependabot PRs: %w", err)
		}

		totalRepos := len(repos)
		reposWithOldPRsCount := len(reposWithOldPRs)
		percentage := 0.0
		if totalRepos > 0 {
			percentage = float64(reposWithOldPRsCount) / float64(totalRepos) * 100
		}

		// Apply sorting based on user preference
		var sortedRepos []github.RepoInfo
		if sortBy == "age" {
			sortedRepos = github.SortReposByAge(reposWithOldPRs)
		} else {
			// Default is to sort by name
			sortedRepos = github.SortReposByName(reposWithOldPRs)
		}

		// Convert to RepoWithAge format for output
		reposWithAge := make([]RepoWithAge, len(sortedRepos))
		for i, repo := range sortedRepos {
			reposWithAge[i] = RepoWithAge{
				Name:      repo.Name,
				AgeInDays: repo.OldestPRAge.Hours() / 24.0, // Convert to days
			}
		}

		// Prepare output data
		data := OutputData{
			TotalRepos:      totalRepos,
			TotalWithOldPRs: reposWithOldPRsCount,
			Percentage:      percentage,
			MaxAge:          maxAge,
			ReposWithOldPRs: reposWithAge,
		}

		// Output in the requested format
		switch outputFormat {
		case "json":
			return outputJSON(data)
		case "csv":
			return outputCSV(data)
		default: // "text"
			return outputText(data, quiet)
		}
	},
}

// outputText formats the output as text
func outputText(data OutputData, quiet bool) error {
	// In quiet mode, only output the repo names without any context
	if quiet {
		for _, repo := range data.ReposWithOldPRs {
			fmt.Println(repo.Name)
		}
		return nil
	}

	// Normal output mode
	if data.TotalWithOldPRs == 0 {
		fmt.Printf("No repositories found with Dependabot PRs older than %d days (0.0%% of %d production repositories)\n",
			data.MaxAge, data.TotalRepos)
		return nil
	}

	fmt.Printf("%d of %d production repositories (%.1f%%) have Dependabot PRs older than %d days:\n",
		data.TotalWithOldPRs, data.TotalRepos, data.Percentage, data.MaxAge)
	for _, repo := range data.ReposWithOldPRs {
		if sortBy == "age" {
			fmt.Printf("- %s (%.1f days old)\n", repo.Name, repo.AgeInDays)
		} else {
			fmt.Printf("- %s\n", repo.Name)
		}
	}

	return nil
}

// outputJSON formats the output as JSON
func outputJSON(data OutputData) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// outputCSV formats the output as CSV
func outputCSV(data OutputData) error {
	writer := csv.NewWriter(os.Stdout)

	var header []string
	if sortBy == "age" {
		header = []string{"Repository", "PR_Age_Days"}
	} else {
		header = []string{"Repository", "Has_Old_Dependabot_PR"}
	}

	// Write header
	if err := writer.Write(header); err != nil {
		return fmt.Errorf("error writing CSV header: %w", err)
	}

	// Write data for repositories with old PRs
	for _, repo := range data.ReposWithOldPRs {
		var record []string
		if sortBy == "age" {
			record = []string{repo.Name, fmt.Sprintf("%.1f", repo.AgeInDays)}
		} else {
			record = []string{repo.Name, "true"}
		}

		if err := writer.Write(record); err != nil {
			return fmt.Errorf("error writing CSV record: %w", err)
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("error flushing CSV writer: %w", err)
	}

	return nil
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
	rootCmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "Only output repository names with no additional context")
	rootCmd.Flags().StringVar(&outputFormat, "format", "text", "Output format (text, json, csv)")
	rootCmd.Flags().StringVar(&sortBy, "sort", "name", "Sort results by: name, age")
	rootCmd.Flags().StringArrayVar(&selectExprs, "select", nil,
		"Criterion marking a repository as production, as 'topic:NAME' or 'property:NAME=VALUE'. "+
			"Repeatable; a repository matching any one of them is included. "+
			"(default topic:business-critical-yes, property:business-critical=yes)")

	// Set version string in format "BRANCH (SHORT_REF)"
	if version != "" && build != "" {
		rootCmd.Version = version + " (" + build + ")"
	}

	rootCmd.MarkFlagRequired("organization")
}
