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
	"github.com/scottbrown/dependabot-pr-checker/pkg/selector"
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
		name string
		// selectors defaults to selector.Defaults() when nil
		selectors    selector.Set
		organization string
		responseBody string
		// propertiesBody is served from /orgs/{org}/properties/values
		propertiesBody   string
		propertiesStatus int
		expectedRepos    []string
		expectedError    bool
		responseStatus   int
	}{
		{
			name:         "matches_default_topic",
			organization: "testorg",
			responseBody: `[
				{"name": "repo1", "topics": ["business-critical-yes"]},
				{"name": "repo2", "topics": ["other-topic"]},
				{"name": "repo3", "topics": ["business-critical-yes", "other-topic"]}
			]`,
			propertiesBody: `[]`,
			expectedRepos:  []string{"repo1", "repo3"},
			expectedError:  false,
			responseStatus: http.StatusOK,
		},
		{
			name:         "matches_default_property",
			organization: "testorg",
			responseBody: `[
				{"name": "repo1", "topics": ["other-topic"]},
				{"name": "repo2", "topics": []}
			]`,
			propertiesBody: `[
				{"repository_name": "repo1", "properties": [{"property_name": "business-critical", "value": "yes"}]},
				{"repository_name": "repo2", "properties": [{"property_name": "business-critical", "value": "no"}]}
			]`,
			expectedRepos:  []string{"repo1"},
			expectedError:  false,
			responseStatus: http.StatusOK,
		},
		{
			name:         "topic_and_property_matches_are_not_duplicated",
			organization: "testorg",
			responseBody: `[
				{"name": "repo1", "topics": ["business-critical-yes"]},
				{"name": "repo2", "topics": ["other-topic"]},
				{"name": "repo3", "topics": ["other-topic"]}
			]`,
			propertiesBody: `[
				{"repository_name": "repo1", "properties": [{"property_name": "business-critical", "value": "yes"}]},
				{"repository_name": "repo2", "properties": [{"property_name": "business-critical", "value": "yes"}]}
			]`,
			expectedRepos:  []string{"repo1", "repo2"},
			expectedError:  false,
			responseStatus: http.StatusOK,
		},
		{
			name:         "unset_property_value_does_not_match",
			organization: "testorg",
			responseBody: `[{"name": "repo1", "topics": []}]`,
			propertiesBody: `[
				{"repository_name": "repo1", "properties": [{"property_name": "business-critical", "value": null}]}
			]`,
			expectedRepos:  nil,
			expectedError:  false,
			responseStatus: http.StatusOK,
		},
		{
			name:         "multi_select_property_matches_any_value",
			organization: "testorg",
			responseBody: `[{"name": "repo1", "topics": []}]`,
			propertiesBody: `[
				{"repository_name": "repo1", "properties": [{"property_name": "business-critical", "value": ["no", "yes"]}]}
			]`,
			expectedRepos:  []string{"repo1"},
			expectedError:  false,
			responseStatus: http.StatusOK,
		},
		{
			name:         "custom_selectors_replace_defaults",
			selectors:    selector.Set{selector.NewProperty("tier", "1")},
			organization: "testorg",
			responseBody: `[
				{"name": "repo1", "topics": ["business-critical-yes"]},
				{"name": "repo2", "topics": []}
			]`,
			propertiesBody: `[
				{"repository_name": "repo2", "properties": [{"property_name": "tier", "value": "1"}]}
			]`,
			expectedRepos:  []string{"repo2"},
			expectedError:  false,
			responseStatus: http.StatusOK,
		},
		{
			name:         "topic_only_selectors_skip_the_properties_call",
			selectors:    selector.Set{selector.NewTopic("business-critical-yes")},
			organization: "testorg",
			responseBody: `[{"name": "repo1", "topics": ["business-critical-yes"]}]`,
			// A 500 here fails the test if the properties endpoint is called.
			propertiesBody:   `{"message": "should not be called"}`,
			propertiesStatus: http.StatusInternalServerError,
			expectedRepos:    []string{"repo1"},
			expectedError:    false,
			responseStatus:   http.StatusOK,
		},
		{
			name:             "forbidden_properties_degrades_to_topics",
			organization:     "testorg",
			responseBody:     `[{"name": "repo1", "topics": ["business-critical-yes"]}]`,
			propertiesBody:   `{"message": "Forbidden"}`,
			propertiesStatus: http.StatusForbidden,
			expectedRepos:    []string{"repo1"},
			expectedError:    false,
			responseStatus:   http.StatusOK,
		},
		{
			name:             "forbidden_properties_fails_when_no_topic_selector",
			selectors:        selector.Set{selector.NewProperty("business-critical", "yes")},
			organization:     "testorg",
			responseBody:     `[{"name": "repo1", "topics": ["business-critical-yes"]}]`,
			propertiesBody:   `{"message": "Forbidden"}`,
			propertiesStatus: http.StatusForbidden,
			expectedRepos:    nil,
			expectedError:    true,
			responseStatus:   http.StatusOK,
		},
		{
			name:           "empty_response",
			organization:   "testorg",
			responseBody:   `[]`,
			propertiesBody: `[]`,
			expectedRepos:  nil,
			expectedError:  false,
			responseStatus: http.StatusOK,
		},
		{
			name:           "error_response",
			organization:   "testorg",
			responseBody:   `{"message": "Not Found"}`,
			propertiesBody: `[]`,
			expectedRepos:  nil,
			expectedError:  true,
			responseStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a handler that returns the test response
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/orgs/"+tt.organization+"/properties/values" {
					status := tt.propertiesStatus
					if status == 0 {
						status = http.StatusOK
					}
					w.WriteHeader(status)
					w.Write([]byte(tt.propertiesBody))
					return
				}

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

			selectors := tt.selectors
			if selectors == nil {
				selectors = selector.Defaults()
			}

			// Call the function being tested
			repos, err := client.GetProductionRepos(tt.organization, selectors, false)

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
				// URL path format: /repos/{org}/{repo}/pulls
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
			repos, err := client.CheckForOldDependabotPRs("testorg", tt.repos, tt.maxAge, false)

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

func TestNewClient(t *testing.T) {
	client, err := NewClient("test-token")
	if err != nil {
		t.Fatalf("NewClient() unexpected error: %v", err)
	}
	if client.client == nil {
		t.Error("NewClient() did not set the underlying GitHub client")
	}
	if client.ctx == nil {
		t.Error("NewClient() did not set a context")
	}
}

func TestGetProductionReposPaginatesProperties(t *testing.T) {
	org := "testorg"
	var propertyPages int

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/orgs/" + org + "/repos":
			w.Write([]byte(`[
				{"name": "repo1", "topics": []},
				{"name": "repo2", "topics": []}
			]`))
		case "/orgs/" + org + "/properties/values":
			propertyPages++
			if r.URL.Query().Get("page") == "" {
				w.Header().Set("Link", `<`+r.URL.Scheme+`//`+r.Host+`/orgs/`+org+`/properties/values?page=2>; rel="next"`)
				w.Write([]byte(`[
					{"repository_name": "repo1", "properties": [{"property_name": "business-critical", "value": "no"}]}
				]`))
				return
			}
			w.Write([]byte(`[
				{"repository_name": "repo2", "properties": [{"property_name": "business-critical", "value": "yes"}]}
			]`))
		default:
			t.Errorf("Unexpected URL path: %s", r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
		}
	})

	server, client := mockGitHubServer(handler)
	defer server.Close()

	repos, err := client.GetProductionRepos(org, selector.Defaults(), false)
	if err != nil {
		t.Fatalf("GetProductionRepos() unexpected error: %v", err)
	}

	if propertyPages != 2 {
		t.Errorf("Requested %d pages of property values, want 2", propertyPages)
	}
	if !deepEqualStringSlices(repos, []string{"repo2"}) {
		t.Errorf("GetProductionRepos() = %v, want [repo2]", repos)
	}
}

func TestSortReposByName(t *testing.T) {
	repos := []RepoInfo{
		{Name: "charlie", OldestPRAge: 90 * 24 * time.Hour},
		{Name: "alpha", OldestPRAge: 10 * 24 * time.Hour},
		{Name: "bravo", OldestPRAge: 50 * 24 * time.Hour},
	}

	sorted := SortReposByName(repos)

	expected := []string{"alpha", "bravo", "charlie"}
	for i, name := range expected {
		if sorted[i].Name != name {
			t.Errorf("SortReposByName()[%d].Name = %s, want %s", i, sorted[i].Name, name)
		}
	}
	if repos[0].Name != "charlie" {
		t.Error("SortReposByName() modified the input slice")
	}
}

func TestSortReposByAge(t *testing.T) {
	repos := []RepoInfo{
		{Name: "alpha", OldestPRAge: 10 * 24 * time.Hour},
		{Name: "charlie", OldestPRAge: 90 * 24 * time.Hour},
		{Name: "bravo", OldestPRAge: 50 * 24 * time.Hour},
	}

	sorted := SortReposByAge(repos)

	expected := []string{"charlie", "bravo", "alpha"}
	for i, name := range expected {
		if sorted[i].Name != name {
			t.Errorf("SortReposByAge()[%d].Name = %s, want %s", i, sorted[i].Name, name)
		}
	}
	if repos[0].Name != "alpha" {
		t.Error("SortReposByAge() modified the input slice")
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
