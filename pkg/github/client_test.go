package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/go-github/v68/github"
)

// mockGitHubServer creates a mock GitHub API server for testing
func mockGitHubServer(handler http.Handler) (*httptest.Server, *Client) {
	// Create a test server
	server := httptest.NewServer(handler)

	// Create a GitHub client that uses the test server
	client := github.NewClient(nil)
	url, _ := url.Parse(server.URL + "/")
	client.BaseURL = url
	client.UploadURL = url

	// Create our client with the mocked GitHub client
	return server, &Client{
		client: client,
		ctx:    context.Background(),
	}
}

func TestGetProductionRepos(t *testing.T) {
	// Define test cases
	tests := []struct {
		name           string
		organization   string
		responseBody   string
		expectedRepos  []string
		expectedError  bool
		responseStatus int
	}{
		{
			name:         "successful_request",
			organization: "testorg",
			responseBody: `[
				{"name": "repo1", "topics": ["business-critical-yes"]},
				{"name": "repo2", "topics": ["other-topic"]},
				{"name": "repo3", "topics": ["business-critical-yes", "other-topic"]}
			]`,
			expectedRepos:  []string{"repo1", "repo3"},
			expectedError:  false,
			responseStatus: http.StatusOK,
		},
		{
			name:           "empty_response",
			organization:   "testorg",
			responseBody:   `[]`,
			expectedRepos:  nil,
			expectedError:  false,
			responseStatus: http.StatusOK,
		},
		{
			name:           "error_response",
			organization:   "testorg",
			responseBody:   `{"message": "Not Found"}`,
			expectedRepos:  nil,
			expectedError:  true,
			responseStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a handler that returns the test response
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Check if the request is for the expected endpoint
				if r.URL.Path != "/orgs/"+tt.organization+"/repos" {
					t.Errorf("Expected request to '/orgs/%s/repos', got '%s'", tt.organization, r.URL.Path)
				}

				// Set the response status code
				w.WriteHeader(tt.responseStatus)
				// Write the response body
				w.Write([]byte(tt.responseBody))
			})

			// Create a mock server and client
			server, client := mockGitHubServer(handler)
			defer server.Close()

			// Call the function being tested
			repos, err := client.GetProductionRepos(tt.organization, false)

			// Check if the error matches the expected error
			if (err != nil) != tt.expectedError {
				t.Errorf("GetProductionRepos() error = %v, expectedError %v", err, tt.expectedError)
				return
			}

			// Check if the repos match the expected repos
			if !deepEqualStringSlices(repos, tt.expectedRepos) {
				t.Errorf("GetProductionRepos() = %v, want %v", repos, tt.expectedRepos)
			}
		})
	}
}

func TestCheckForOldDependabotPRs(t *testing.T) {
	// Define test cases
	tests := []struct {
		name           string
		repos          []string
		maxAge         time.Duration
		responseBodies map[string]string
		expectedRepos  []RepoInfo
		expectedError  bool
		responseStatus int
	}{
		{
			name:   "old_dependabot_prs",
			repos:  []string{"repo1", "repo2"},
			maxAge: 30 * 24 * time.Hour,
			responseBodies: map[string]string{
				"repo1": `[
					{
						"user": {"login": "dependabot[bot]"},
						"created_at": "2023-01-01T00:00:00Z"
					}
				]`,
				"repo2": `[
					{
						"user": {"login": "other-user"},
						"created_at": "2023-01-01T00:00:00Z"
					}
				]`,
			},
			expectedRepos: []RepoInfo{
				{
					Name: "repo1",
					// Note: In the test, we're only checking if the Name field matches
					// The exact time values will be checked separately
				},
			},
			expectedError:  false,
			responseStatus: http.StatusOK,
		},
		{
			name:   "no_old_dependabot_prs",
			repos:  []string{"repo1"},
			maxAge: 30 * 24 * time.Hour,
			responseBodies: map[string]string{
				"repo1": `[
					{
						"user": {"login": "dependabot[bot]"},
						"created_at": "2025-05-01T00:00:00Z"
					}
				]`,
			},
			expectedRepos:  nil,
			expectedError:  false,
			responseStatus: http.StatusOK,
		},
		{
			name:   "error_response",
			repos:  []string{"repo1"},
			maxAge: 30 * 24 * time.Hour,
			responseBodies: map[string]string{
				"repo1": `{"message": "Not Found"}`,
			},
			expectedRepos:  nil,
			expectedError:  true,
			responseStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a handler that returns the test response
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Extract the repo name from the URL path
				// URL path format: /repos/kohofinancial/{repo}/pulls
				pathParts := strings.Split(r.URL.Path, "/")

				// Check if this is a valid pull request URL
				if len(pathParts) >= 5 && pathParts[1] == "repos" && pathParts[3] != "" {
					repo := pathParts[3]

					// Set the response status code
					w.WriteHeader(tt.responseStatus)

					// Write the response body for the specific repo
					if body, ok := tt.responseBodies[repo]; ok {
						w.Write([]byte(body))
					} else {
						t.Errorf("No response body defined for repo: %s", repo)
					}
				} else {
					t.Errorf("Unexpected URL path: %s", r.URL.Path)
					w.WriteHeader(http.StatusBadRequest)
				}
			})

			// Create a mock server and client
			server, client := mockGitHubServer(handler)
			defer server.Close()

			// Call the function being tested
			repos, err := client.CheckForOldDependabotPRs(tt.repos, tt.maxAge, false)

			// Check if the error matches the expected error
			if (err != nil) != tt.expectedError {
				t.Errorf("CheckForOldDependabotPRs() error = %v, expectedError %v", err, tt.expectedError)
				return
			}

			// For successful cases where we expect repos with old PRs
			if !tt.expectedError && len(tt.expectedRepos) > 0 {
				// Verify we have the same number of repositories
				if len(repos) != len(tt.expectedRepos) {
					t.Errorf("CheckForOldDependabotPRs() returned %d repos, want %d", len(repos), len(tt.expectedRepos))
					return
				}
				
				// Check repo names (ignoring exact age values which would be hard to test)
				for i, repo := range repos {
					if repo.Name != tt.expectedRepos[i].Name {
						t.Errorf("CheckForOldDependabotPRs() repo %d = %s, want %s", i, repo.Name, tt.expectedRepos[i].Name)
					}
				}
			}
		})
	}
}

// Helper function to compare string slices
func deepEqualStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	
	return true
}