# Dependabot PR Checker

A CLI tool to check for old Dependabot pull requests in production GitHub repositories.

## Overview

This tool identifies GitHub repositories in a specified organization that:
1. Are considered "production" repositories, meaning they have either the topic
   `business-critical-yes` or the custom property `business-critical` set to `yes`
2. Contain open Dependabot pull requests older than a specified number of days (default: 30 days)

Both criteria are checked by default, so organizations part-way through migrating
from repository topics to custom properties are covered without extra configuration.
See [Defining "production"](#defining-production) to change the criteria.

## Installation

### Homebrew

```bash
brew tap scottbrown/tools
brew install dependabot-pr-checker
```

### Pre-built binaries

Each tagged release publishes `tar.gz` archives for Linux, macOS, and Windows
(amd64 and arm64) plus a CycloneDX SBOM on the
[releases page](https://github.com/scottbrown/dependabot-pr-checker/releases).

```bash
tar xzf dependabot-pr-checker_v2.0.1_darwin_arm64.tar.gz
mv dependabot-pr-checker /usr/local/bin/
```

### From source

```bash
# Clone the repository
git clone https://github.com/scottbrown/dependabot-pr-checker.git
cd dependabot-pr-checker

# Build the binary using Task
task build
# The binary will be available in the .build directory
```

Alternatively, you can install directly using Go:

```bash
go install github.com/scottbrown/dependabot-pr-checker/v2@latest
```

## Development

This project uses [Task](https://taskfile.dev/) for automation. Make sure you have Task installed.

Available tasks:

```bash
# Build the application
task build

# Run tests
task test

# Format code
task fmt

# Lint code
task lint

# Measure test coverage
task coverage

# Generate HTML test coverage report
task coverage-report

# Build release artifacts for all platforms
task release VERSION=v2.0.1
```

The coverage report will be available at `.build/coverage.html` after running the `coverage-report` task.
Release archives are written to `.dist/`.

## Releasing

Releases are cut by pushing a `v`-prefixed tag. The `Release` GitHub Actions
workflow runs the unit tests, cross-compiles for linux/darwin/windows on both
amd64 and arm64, generates an SBOM, and attaches everything to a GitHub release.

```bash
git tag v2.0.1
git push origin v2.0.1
```

The version and commit are baked into the binary via `-ldflags` and reported by
`dependabot-pr-checker --version`.

After the release completes, update the Homebrew formula in
[scottbrown/homebrew-tools](https://github.com/scottbrown/homebrew-tools) with the
new version and the `sha256` of each archive.

## Usage

```bash
# Set your GitHub token as an environment variable
export GITHUB_TOKEN=your_github_token

# Run the tool
./dependabot-pr-checker -o myorg
```

### Required Permissions

The GitHub token must have:
- `repo` scope to access private repositories
- Access to the specified organization (may require SAML enforcement if enabled)
- Permission to read custom properties for the organization, if matching on
  custom properties (the default). Without it the tool prints a warning and falls
  back to matching on topics alone.

### Defining "production"

The `--select` flag defines what makes a repository "production". It takes either
form below and may be repeated; a repository is included if it matches **any** one
selector:

| Selector | Matches |
| --- | --- |
| `topic:NAME` | Repositories carrying the topic `NAME` |
| `property:NAME=VALUE` | Repositories whose custom property `NAME` holds `VALUE` |

When `--select` is omitted, the defaults are `topic:business-critical-yes` and
`property:business-critical=yes`. Passing `--select` replaces both defaults rather
than adding to them.

```bash
# Default: topic OR custom property
./dependabot-pr-checker -o myorg

# Custom properties only (migration finished)
./dependabot-pr-checker -o myorg --select property:business-critical=yes

# Topics only (no custom properties configured, or a token that cannot read them)
./dependabot-pr-checker -o myorg --select topic:business-critical-yes

# Any other criteria your organization uses
./dependabot-pr-checker -o myorg --select property:tier=tier-1 --select topic:production
```

Topic names and property names and values are matched case-insensitively. A
multi-select custom property matches when any of its values matches. Property
values are read from a single organization-wide endpoint, so adding a property
selector costs one extra paginated request per run, not one per repository.

### Command Line Options

```
Usage:
  dependabot-pr-checker [flags]

Flags:
      --format string         Output format (text, json, csv) (default "text")
  -h, --help                  help for dependabot-pr-checker
      --max-age int           Maximum age of Dependabot PRs in days (default 30)
  -o, --organization string   GitHub organization to check (required)
  -q, --quiet                 Only output repository names with no additional context
      --select stringArray    Criterion marking a repository as production, as 'topic:NAME' or
                              'property:NAME=VALUE'. Repeatable; a repository matching any one of
                              them is included. (default topic:business-critical-yes,
                              property:business-critical=yes)
      --sort string           Sort results by: name, age (default "name")
  -v, --verbose               Show verbose output with progress bars
```

## Examples

```bash
# Check for Dependabot PRs older than 14 days
./dependabot-pr-checker -o myorg --max-age 14

# Show verbose output with progress bars
./dependabot-pr-checker -o myorg -v

# Sort repositories by age of oldest PR (oldest first)
./dependabot-pr-checker -o myorg --sort age

# Only output repository names (useful for piping to other commands)
./dependabot-pr-checker -o myorg -q

# Output in JSON format
./dependabot-pr-checker -o myorg --format json

# Output in CSV format with age information
./dependabot-pr-checker -o myorg --format csv --sort age

# Select production repositories by custom property instead of topic
./dependabot-pr-checker -o myorg --select property:business-critical=yes
```

## Output

### Default Text Output

The tool will output the percentage of repositories with old Dependabot PRs and a list of those repositories:

```
15 of 50 production repositories (30.0%) have Dependabot PRs older than 30 days:
- repo1
- repo2
- repo3
```

When sorting by age (`--sort age`), the output includes the age of the oldest PR in days:

```
15 of 50 production repositories (30.0%) have Dependabot PRs older than 30 days:
- repo1 (120.5 days old)
- repo2 (95.2 days old)
- repo3 (45.8 days old)
```

If no repositories are found with old Dependabot PRs, it will output:

```
No repositories found with Dependabot PRs older than 30 days (0.0% of 45 production repositories)
```

### Quiet Mode Output

In quiet mode, only repository names are output:

```
repo1
repo2
repo3
```

### JSON Format Output

```json
{
  "total_repos": 50,
  "total_with_old_prs": 15,
  "percentage": 30.0,
  "max_age": 30,
  "repos_with_old_prs": [
    "repo1",
    "repo2",
    "repo3"
  ]
}
```

### CSV Format Output

Default format:
```
Repository,Has_Old_Dependabot_PR
repo1,true
repo2,true
repo3,true
```

When using `--sort age`, the CSV output includes PR ages:
```
Repository,PR_Age_Days
repo1,120.5
repo2,95.2
repo3,45.8
```