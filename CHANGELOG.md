# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

[2.0.0]: https://github.com/scottbrown/dependabot-pr-checker/releases/tag/v2.0.0
[1.0.0]: https://github.com/scottbrown/dependabot-pr-checker/releases/tag/v1.0.0
