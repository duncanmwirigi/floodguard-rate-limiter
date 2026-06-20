# Contributing to floodguard

Thank you for your interest in contributing to floodguard. This document covers the basics for getting your changes merged.

## Development setup

1. Install Go 1.22 or later.
2. Clone the repository and enter the module root:

   ```bash
   git clone https://github.com/ultimateprogrammer/floodguard.git
   cd floodguard
   ```

3. Optional: start Redis for the example server and Redis-backed tests:

   ```bash
   redis-server
   ```

## Running checks locally

Before opening a pull request, run the same checks as CI:

```bash
go vet ./...
go test -race ./...
golangci-lint run ./...
```

Install golangci-lint from [golangci-lint.run](https://golangci-lint.run/welcome/install/).

## Code guidelines

- Follow standard Go conventions (`gofmt`, idiomatic error handling).
- Use sentinel errors with `errors.Is` / `errors.As`; wrap operational errors with `%w`.
- Add godoc comments to every exported type, function, and package.
- Keep changes focused; prefer small, reviewable pull requests.
- Add or update tests for behavior changes.

## Pull request process

1. Fork the repository and create a feature branch from `main`.
2. Make your changes with tests and documentation updates.
3. Ensure CI passes (`go vet`, `go test`, `golangci-lint`).
4. Open a pull request describing the problem, solution, and test plan.
5. Address review feedback promptly.

## Reporting issues

When filing a bug report, include:

- Go version and operating system
- Steps to reproduce
- Expected vs. actual behavior
- Relevant logs or stack traces

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).
