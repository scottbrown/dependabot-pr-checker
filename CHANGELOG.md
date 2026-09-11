# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- GitHub App authentication, for automation in organizations whose policy
  prohibits personal access tokens. Configure `GITHUB_APP_ID` plus either
  `GITHUB_APP_PRIVATE_KEY` or `GITHUB_APP_PRIVATE_KEY_PATH`; the installation is
  discovered from `--organization` unless `GITHUB_APP_INSTALLATION_ID` pins it.
  Installation tokens are renewed automatically, so a scan lasting longer than
  the one-hour token lifetime no longer fails partway through.
- `login` and `logout` subcommands, authenticating interactively with the OAuth
  device flow and caching the resulting user access token with `0600`
  permissions. Cached tokens are refreshed and re-cached as they expire. Set
  `DEPENDABOT_PR_CHECKER_CLIENT_ID` to the client ID of a GitHub App with device
  flow enabled, or bake one in at build time with `CLIENT_ID=... task build`.
- Credentials from the GitHub CLI, used when nothing else is configured.
- `--auth` flag selecting the credential source explicitly: `auto` (the default),
  `env`, `app`, `oauth`, or `gh`. A source that is configured but broken now
  fails the run rather than falling through to a different credential, which
  would quietly change which repositories the run can see.
- `--verbose` reports which credential source was used.

### Changed

- `github.NewClient` now takes an authenticated `*http.Client` rather than a
  token string, so that credentials which expire mid-run can renew themselves.
  Use `github.NewClientWithToken` for the previous behaviour. This affects only
  callers who consume the packages directly.

## [2.0.1] - 2026-08-27

### Fixed

- `Taskfile.yml` derived the Go module path from the binary name, so it missed
  the `/v2` suffix added in 2.0.0 and every cross-platform build failed with
  "no required module provides package". The module path is now built from an
  explicit `major_version` variable. No release binaries were published for
  2.0.0 as a result; use 2.0.1.

## [2.0.0] - 2026-08-27

### Added

- Repositories can now be selected as "production" by GitHub custom property as
  well as by topic. By default a repository qualifies if it has the topic
  `business-critical-yes` **or** the custom property `business-critical` set to
  `yes`, so organizations migrating between the two conventions are covered
  without configuration.
- New repeatable `--select` flag defines the criteria, accepting `topic:NAME` or
  `property:NAME=VALUE`. Selectors combine with OR semantics.

### Changed

- **Breaking:** the module path is now
  `github.com/scottbrown/dependabot-pr-checker/v2`, as required for a Go major
  version. Install with
  `go install github.com/scottbrown/dependabot-pr-checker/v2@latest`, and update
  import paths if you consume the packages directly.
- **Breaking:** `github.Client.GetProductionRepos` takes a `selector.Set`
  argument. The hard-coded `business-critical-yes` topic string is gone.
- Topic matching is now case-insensitive.

### Notes

- Matching on custom properties requires the GitHub token to be able to read
  custom properties for the organization. If it cannot, the tool prints a
  warning and falls back to matching on topics alone; when only property
  selectors are in use it fails instead, so that a permissions problem is never
  reported as "no production repositories found".
- Property values are read from a single organization-wide endpoint, costing one
  extra paginated request per run rather than one per repository. The request is
  skipped entirely when no property selector is in use.

## [1.0.0]

- Initial release.

[2.0.1]: https://github.com/scottbrown/dependabot-pr-checker/releases/tag/v2.0.1
[2.0.0]: https://github.com/scottbrown/dependabot-pr-checker/releases/tag/v2.0.0
[1.0.0]: https://github.com/scottbrown/dependabot-pr-checker/releases/tag/v1.0.0
