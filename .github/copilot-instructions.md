# Copilot Instructions for genidi

## Project Overview

genidi is a Go project maintained by @icco. The repository uses Go 1.25.x and follows standard Go conventions and best practices.

## Development Environment

- **Language**: Go 1.25.x
- **Package Manager**: Go modules (`go.mod`)
- **Module Path**: `github.com/icco/genidi`

## Build and Test Commands

### Building the Project
```bash
go build -v ./...
```

### Running Tests
```bash
go test -v ./...
```

### Installing Dependencies
```bash
go get .
```

## Code Quality and Linting

This project uses `golangci-lint` with the following linters enabled:
- `bodyclose` - Checks for unclosed HTTP response bodies
- `misspell` - Finds commonly misspelled English words
- `gosec` - Inspects source code for security problems
- `goconst` - Finds repeated strings that could be constants
- `errorlint` - Checks for common mistakes with error wrapping

### Running the Linter
```bash
golangci-lint run -E bodyclose,misspell,gosec,goconst,errorlint
```

## Coding Standards

- Follow standard Go conventions and idioms
- Use `gofmt` for code formatting (this is standard for Go)
- Write clear, descriptive variable and function names
- Include comments for exported functions and types (godoc style)
- Handle errors explicitly - do not ignore errors
- Write tests for new functionality
- Ensure code passes all configured linters

## CI/CD Pipeline

The repository has the following GitHub Actions workflows:

1. **Test Go** (`test.yml`): Runs on every push
   - Builds the project
   - Runs all tests

2. **golangci-lint** (`golangci-lint.yml`): Runs on pushes to main, tags, and pull requests
   - Performs static code analysis
   - Enforces code quality standards

3. **CodeQL Analysis** (`codeql-analysis.yml`): Security scanning

## Best Practices for Contributing

- Keep changes minimal and focused
- Ensure all tests pass before submitting changes
- Make sure code passes linting checks
- Follow Go's standard project layout if adding new directories
- Write tests for new features or bug fixes
- Update documentation if adding new functionality

## Security Considerations

- Never commit sensitive data (credentials, API keys, etc.)
- The project uses CodeQL for security scanning
- Be mindful of common Go security issues (the `gosec` linter helps with this)

## Issue Assignment

When working on issues:
- Focus on the specific problem described in the issue
- Include tests when fixing bugs or adding features
- Update relevant documentation
- Keep changes scoped to what's necessary to resolve the issue
