# CLAUDE.md

## Project overview

Runware CLI — a Go command-line tool for the Runware inference API. Single binary, built with Cobra, released via GoReleaser.

## Build & test

```bash
make build      # Build binaries -> ./bin
make test       # Run all tests (go test ./...)
make lint       # golangci-lint
make snapshot   # GoReleaser snapshot build
```

## Project structure

- `main.go` — entrypoint, version vars injected via ldflags
- `cmd/` — Cobra command definitions (one file per command)
- `internal/api/` — API client and request handling
- `internal/config/` — Config loading/saving (~/.runware/config.yaml)
- `internal/output/` — Output formatting (table/json/yaml)
- `internal/cache/` — Caching layer

## Spec

`runware-cli-v1-spec.md` is the v1 feature specification. It defines the full command roadmap, API task type mappings, and architecture principles. Reference it when adding new commands or planning features. The tracking epic is Jira RUN-6978.

## Conventions

- Commands use camelCase (e.g. `imageInference`, `modelSearch`, `audioInference`)
- Commit messages follow conventional commits (`feat:`, `fix:`, `docs:`, `test:`)
- Tests live alongside source files (`*_test.go`)
- Config shorthand keys in CLI map to nested yaml paths (e.g. `model` → `defaults.model`)

## Release process

Tag with `vX.Y.Z` and push — GitHub Actions runs GoReleaser to build and publish.
