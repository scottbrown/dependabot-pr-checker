package cmd

import (
	"bytes"
	"os"
	"testing"
)

func TestRootCommand(t *testing.T) {
	// Save the original os.Args and restore it after the test
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	tests := []struct {
		name          string
		args          []string
		envVars       map[string]string
		expectedError bool
		expectedOut   string
	}{
		{
			name:          "missing_organization_flag",
			args:          []string{"dependabot-pr-checker"},
			envVars:       map[string]string{"GITHUB_TOKEN": "dummy-token"},
			expectedError: true,
		},
		{
			name:          "missing_github_token",
			args:          []string{"dependabot-pr-checker", "-o", "testorg"},
			envVars:       map[string]string{},
			expectedError: true,
		},
		{
			name:          "conflicting_verbose_quiet_flags",
			args:          []string{"dependabot-pr-checker", "-o", "testorg", "-v", "-q"},
			envVars:       map[string]string{"GITHUB_TOKEN": "dummy-token"},
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up command line arguments
			os.Args = tt.args

			// Set up environment variables
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}
			defer func() {
				// Clean up environment variables
				for k := range tt.envVars {
					os.Unsetenv(k)
				}
			}()

			// Capture stdout
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			// Execute the command
			err := Execute()

			// Restore stdout
			w.Close()
			os.Stdout = oldStdout

			// Read captured output
			var buf bytes.Buffer
			buf.ReadFrom(r)
			output := buf.String()

			// Check if the error matches the expected error
			if (err != nil) != tt.expectedError {
				t.Errorf("Execute() error = %v, expectedError %v", err, tt.expectedError)
				return
			}

			// Check if the output contains the expected string
			if tt.expectedOut != "" && output != tt.expectedOut {
				t.Errorf("Execute() output = %v, want %v", output, tt.expectedOut)
			}
		})
	}
}