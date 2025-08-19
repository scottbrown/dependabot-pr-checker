package github

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/go-github/v68/github"
	"github.com/schollz/progressbar/v3"
	"golang.org/x/oauth2"
)

// RepoInfo contains information about a repository with old Dependabot PRs
type RepoInfo struct {
	Name         string
	OldestPRAge  time.Duration
	OldestPRDate time.Time
}

// Client wraps the GitHub API client
type Client struct {
	client *github.Client
	ctx    context.Context
}

// NewClient creates a new GitHub client with the provided token
func NewClient(token string) (*Client, error) {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	return &Client{
		client: client,
		ctx:    ctx,
	}, nil
}

// GetProductionRepos returns all repositories in the organization that have the topic 'business-critical-yes'
func (c *Client) GetProductionRepos(org string, verbose bool) ([]string, error) {
	var allRepos []*github.Repository
	opts := &github.RepositoryListByOrgOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	// Initialize progress bar for repository collection if verbose mode is enabled
	var bar *progressbar.ProgressBar
	if verbose {
		fmt.Println("Collecting repositories from organization:", org)
		bar = progressbar.NewOptions(-1,
			progressbar.OptionSetDescription("Fetching repositories"),
			progressbar.OptionShowCount(),
			progressbar.OptionSetTheme(progressbar.Theme{
				Saucer:        "=",
				SaucerHead:    ">",
				SaucerPadding: " ",
				BarStart:      "[",
				BarEnd:        "]",
			}),
		)
	}

	for {
		repos, resp, err := c.client.Repositories.ListByOrg(c.ctx, org, opts)
		if err != nil {
			if resp != nil && resp.StatusCode == 403 {
				return nil, fmt.Errorf("error listing repositories: access denied. Make sure your GitHub token has the necessary permissions for the %s organization", org)
			}
			return nil, fmt.Errorf("error listing repositories: %w", err)
		}
		allRepos = append(allRepos, repos...)

		if verbose {
			bar.Add(len(repos))
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	if verbose {
		bar.Finish()
		fmt.Printf("Found %d total repositories\n", len(allRepos))
	}

	var productionRepos []string
	for _, repo := range allRepos {
		if repo.Topics == nil {
			continue
		}

		for _, topic := range repo.Topics {
			if topic == "business-critical-yes" {
				productionRepos = append(productionRepos, *repo.Name)
				break
			}
		}
	}

	if verbose {
		fmt.Printf("Found %d production repositories with 'business-critical-yes' topic\n", len(productionRepos))
	}

	return productionRepos, nil
}

// CheckForOldDependabotPRs checks each repository for Dependabot PRs older than maxAge
func (c *Client) CheckForOldDependabotPRs(repos []string, maxAge time.Duration, verbose bool) ([]RepoInfo, error) {
	var reposWithOldPRs []RepoInfo
	cutoffTime := time.Now().Add(-maxAge)
	now := time.Now()

	// Initialize progress bar for checking repositories if verbose mode is enabled
	var bar *progressbar.ProgressBar
	if verbose {
		fmt.Printf("Checking %d repositories for old Dependabot PRs\n", len(repos))
		bar = progressbar.NewOptions(len(repos),
			progressbar.OptionSetDescription("Checking repositories"),
			progressbar.OptionShowCount(),
			progressbar.OptionSetTheme(progressbar.Theme{
				Saucer:        "=",
				SaucerHead:    ">",
				SaucerPadding: " ",
				BarStart:      "[",
				BarEnd:        "]",
			}),
		)
	}

	for i, repo := range repos {
		if verbose {
			bar.Describe(fmt.Sprintf("Checking %s (%d/%d)", repo, i+1, len(repos)))
		}

		opts := &github.PullRequestListOptions{
			State:     "open",
			Sort:      "created",
			Direction: "asc",
			ListOptions: github.ListOptions{
				PerPage: 100,
			},
		}

		prs, _, err := c.client.PullRequests.List(c.ctx, "kohofinancial", repo, opts)
		if err != nil {
			return nil, fmt.Errorf("error listing PRs for %s: %w", repo, err)
		}

		var oldestPR *github.PullRequest
		for _, pr := range prs {
			// Check if PR is from Dependabot
			if pr.User != nil && pr.User.Login != nil && *pr.User.Login == "dependabot[bot]" {
				// Check if PR is older than maxAge
				if pr.CreatedAt != nil && pr.CreatedAt.Time.Before(cutoffTime) {
					// If we haven't found an old PR yet or this one is older
					if oldestPR == nil || pr.CreatedAt.Time.Before(oldestPR.CreatedAt.Time) {
						oldestPR = pr
					}
				}
			}
		}

		if oldestPR != nil {
			repoInfo := RepoInfo{
				Name:         repo,
				OldestPRDate: oldestPR.CreatedAt.Time,
				OldestPRAge:  now.Sub(oldestPR.CreatedAt.Time),
			}
			reposWithOldPRs = append(reposWithOldPRs, repoInfo)
		}

		if verbose {
			bar.Add(1)
		}
	}

	if verbose {
		bar.Finish()
		if len(reposWithOldPRs) > 0 {
			fmt.Printf("Found %d repositories with old Dependabot PRs\n", len(reposWithOldPRs))
		} else {
			fmt.Println("No repositories found with old Dependabot PRs")
		}
	}

	return reposWithOldPRs, nil
}

// SortReposByName sorts repositories by name in ascending order
func SortReposByName(repos []RepoInfo) []RepoInfo {
	sorted := make([]RepoInfo, len(repos))
	copy(sorted, repos)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})
	return sorted
}

// SortReposByAge sorts repositories by age of oldest Dependabot PR in descending order (oldest first)
func SortReposByAge(repos []RepoInfo) []RepoInfo {
	sorted := make([]RepoInfo, len(repos))
	copy(sorted, repos)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].OldestPRAge > sorted[j].OldestPRAge
	})
	return sorted
}
