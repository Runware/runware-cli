# CLAUDE.md

## Project overview

Runware CLI — a Go command-line tool for the Runware inference API. Single binary, built with Cobra, released via GoReleaser.

## Build & test

```bash
make build           # Build binary for current platform -> ./bin/runware
make build-all       # Build all platforms -> ./bin
make test            # Run all tests (go test -race ./...)
make lint            # golangci-lint
make snapshot        # GoReleaser snapshot build
make docs            # Generate command reference docs
make install         # go install for current platform
make run ARGS="..."  # Run without building
```

### Platform targets (via `make build-all`)

- `darwin-arm64`, `darwin-amd64`
- `linux-amd64`, `linux-arm64`
- `windows-amd64`, `windows-arm64`

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
- Run `go fmt` on any files you modify before considering work complete
- Run `make lint` before considering work complete; fix all lint errors
- Error naming follows https://go.dev/wiki/Errors: error types end in `"Error"` (e.g. `type NotTabularError struct`), error variables start with `"Err"` (e.g. `var ErrNotFound = errors.New(...)`)
- Use `map[string]struct{}` for set/presence maps, not `map[string]bool`
- Write map literals with one key per line for readability
- Write struct literals with one field per line for readability
- Do not use inline anonymous structs; define named types instead

## Release process

Tag with `vX.Y.Z` and push — GitHub Actions runs GoReleaser to build and publish.
