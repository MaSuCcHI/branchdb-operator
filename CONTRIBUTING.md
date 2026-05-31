# Contributing to branchdb-operator

Thank you for your interest in contributing!

## Development Setup

### Prerequisites

- Go 1.26+
- Node.js 22+ (for the web console)
- Docker / Colima (for E2E tests)

```bash
git clone https://github.com/MaSuCcHI/branchdb-operator
cd branchdb-operator
go mod download
cd web && npm ci && cd ..
```

### Running Tests

```bash
# Unit tests
go test ./internal/... ./cmd/... -count=1

# Coverage (≥95% required per package)
go test ./internal/... -cover

# Web build check
cd web && npm run build
```

### TDD Workflow

This project follows t-wada style TDD. Each change must go through:

1. **Red** — write one failing test
2. **Green** — write the minimum code to pass it
3. **Refactor** — clean up while keeping tests green

**Coverage must stay at 95% or above** in every package under `internal/`. Run `go test ./internal/... -cover` and fix any package that drops below before opening a PR.

### Code Style

- `gofmt` — run before committing (`gofmt -l .` must produce no output)
- `go vet ./...` — must pass
- No comments explaining *what* the code does — only *why* when non-obvious
- No error handling for impossible cases; trust internal guarantees

## Architecture

Dependencies flow inward only:

```
infrastructure/ → interface/ → domain/
```

- `domain/` — interfaces only, zero external dependencies
- `infrastructure/` — concrete implementations (ZFS Agent, Kubernetes)
- `interface/` — operator (Reconciler) and API (REST + SPA) adapters

## Pull Request Process

1. Fork and create a branch from `main`
2. Follow the TDD workflow above
3. Ensure `go test ./internal/... -cover` passes with ≥95% per package
4. Run `gofmt -l .` and fix any reported files
5. Open a PR against `main` — the CI workflow will run automatically

## Reporting Issues

Use the [issue tracker](https://github.com/MaSuCcHI/branchdb-operator/issues). For security vulnerabilities, see [SECURITY.md](SECURITY.md).

## License

By contributing you agree that your contributions will be licensed under the [MIT License](LICENSE).
