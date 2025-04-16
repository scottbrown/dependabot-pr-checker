package github

import (
	"context"
	"fmt"
	"time"

	"github.com/google/go-github/v60/github"
	"golang.org/x/oauth2"
)

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
func (c *Client) GetProductionRepos(org string) ([]string, error) {
	var allRepos []*github.Repository
	opts := &github.RepositoryListByOrgOptions{
		ListOptions: github.ListOptions{PerPage: 100},
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
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
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

	return productionRepos, nil
}

// CheckForOldDependabotPRs checks each repository for Dependabot PRs older than maxAge
func (c *Client) CheckForOldDependabotPRs(repos []string, maxAge time.Duration) ([]string, error) {
	var reposWithOldPRs []string
	cutoffTime := time.Now().Add(-maxAge)

	for _, repo := range repos {
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

		hasOldDependabotPR := false
		for _, pr := range prs {
			// Check if PR is from Dependabot
			if pr.User != nil && pr.User.Login != nil && *pr.User.Login == "dependabot[bot]" {
				// Check if PR is older than maxAge
				if pr.CreatedAt != nil && pr.CreatedAt.Before(cutoffTime) {
					hasOldDependabotPR = true
					break
				}
			}
		}

		if hasOldDependabotPR {
			reposWithOldPRs = append(reposWithOldPRs, repo)
		}
	}

	return reposWithOldPRs, nil
}
