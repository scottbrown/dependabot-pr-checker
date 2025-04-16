# Dependabot PR Checker

A CLI tool to check for old Dependabot pull requests in production GitHub repositories.

## Overview

This tool identifies GitHub repositories in a specified organization that:
1. Have the topic `business-critical-yes` (considered "production" repositories)
2. Contain open Dependabot pull requests older than a specified number of days (default: 30 days)

## Installation

```bash
# Clone the repository
git clone https://github.com/kohofinancial/dependabot-pr-checker.git
cd dependabot-pr-checker

# Build the binary
go build -o dependabot-pr-checker
```

Alternatively, you can install directly using Go:

```bash
go install github.com/kohofinancial/dependabot-pr-checker@latest
```

## Usage

```bash
# Set your GitHub token as an environment variable
export GITHUB_TOKEN=your_github_token

# Run the tool
./dependabot-pr-checker -o kohofinancial
```

### Required Permissions

The GitHub token must have:
- `repo` scope to access private repositories
- Access to the specified organization (may require SAML enforcement if enabled)

### Command Line Options

```
Usage:
  dependabot-pr-checker [flags]

Flags:
  -h, --help                  help for dependabot-pr-checker
      --max-age int           Maximum age of Dependabot PRs in days (default 30)
  -o, --organization string   GitHub organization to check (required)
```

## Example

```bash
# Check for Dependabot PRs older than 14 days
./dependabot-pr-checker -o kohofinancial --max-age 14
```

## Output

The tool will output the percentage of repositories with old Dependabot PRs and a list of those repositories:

```
15 of 50 production repositories (30.0%) have Dependabot PRs older than 30 days:
- repo1
- repo2
- repo3
```

If no repositories are found with old Dependabot PRs, it will output:

```
No repositories found with Dependabot PRs older than 30 days (0.0% of 45 production repositories)