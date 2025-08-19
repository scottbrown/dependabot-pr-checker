# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Test Commands
- Build application: `task build`
- Run all tests: `task test` or `go test -v ./...` 
- Run specific test: `go test -v ./path/to/package -run TestName`
- Format code: `task fmt` or `go fmt ./...`
- Lint code: `task lint` or `go vet ./...`
- Check test coverage: `task coverage`

## Code Style Guidelines
- Import order: standard library, then third-party, then internal packages
- Error handling: wrap errors with `fmt.Errorf("context: %w", err)`
- Variable naming: camelCase for variables, PascalCase for exported fields/functions
- Use pointer returns for public APIs, especially with types like `string`
- Group variables with `var` blocks for related fields
- Use table-driven tests with descriptive test case names
- Mock external dependencies in tests using httptest
- Add descriptive comments for exported functions and types
- Follow standard Go project layout with cmd/, pkg/ directories