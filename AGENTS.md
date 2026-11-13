# AGENTS.md

## Project overview

Runware CLI: a Go command-line tool for the Runware inference API. Single binary, built with Cobra, released via GoReleaser.

## Build & test

```bash
make build           # Build binary for current platform -> ./bin/runware
make build-all       # Build all platforms -> ./bin
make test            # Run all tests (go test -race ./...)
make lint            # golangci-lint
make fix             # Modernize Go code with go fix
make check-go-fix    # Fail when go fix would change Go source
make snapshot        # GoReleaser snapshot build
make docs            # Generate command reference docs
make install         # go install for current platform
make run ARGS="..."  # Run without building
make setup           # Once per clone: enable the git hooks in .githooks
make check-public    # Check this branch for text that must not be public
```

### Platform targets (via `make build-all`)

- `darwin-arm64`, `darwin-amd64`
- `linux-amd64`, `linux-arm64`
- `windows-amd64`, `windows-arm64`

## Project structure

- `main.go`: entrypoint, version vars injected via ldflags
- `cmd/`: Cobra command definitions (one file per command)
- `internal/api/`: API client and request handling
- `internal/config/`: Config loading/saving (~/.runware/config.yaml)
- `internal/output/`: Output formatting (table/json/yaml)
- `internal/cache/`: Caching layer

## This repository is public

- Everything written here is public and permanent: code, tests and fixtures, commit messages, branch names, PR titles and descriptions, review comments and replies. GitHub keeps every earlier revision of an edited description or comment readable by anyone, and force-pushed commits stay reachable by SHA until GitHub Support purges them. Write everything as if it will be quoted.
- Never put in: links to or names of private repositories, their documents, PR numbers or paths; Jira, Notion, Slack or Google Docs links (bare ticket keys are fine); internal hostnames; infrastructure and provider names the product does not publish; environment, rollout or beta state; unpublished API operations; credentials, real IPs, real customer or organization identifiers.
- Sync the serverless spec only with `make sync-serverless-spec` (the published bundle), never by copying the source file. Unreleased commands built on unpublished operations stay on a local branch.
- Before pushing or posting: the hooks run `tools/checkpublic` automatically (the git pre-push hook, and Claude Code and Codex hooks on `git push`, `gh` writes and GitHub MCP or connector tools that post a title, body, review or comment; Codex runs `.codex/hooks.json` only in a trusted project, after you approve the hook once in `/hooks` and again whenever the file changes); run `make check-public` before opening a PR. Also read every description and comment before posting it: a pattern cannot tell that a sentence describes internal state.
- Reviewers: apply the same rule to the PR title, description, commit messages and your own review comments, not only the code. The public-content CI check must pass.
- A reference to a Runware repository is allowed only when the repository is public: add it to `tools/checkpublic/public-repos.txt` only then. For a false positive in a file, add `checkpublic:allow` to the line with the reason.
- Names that must not be published but cannot be written here either live in the `CHECKPUBLIC_DENY` repository variable (one regular expression per line, `#` comments), which only collaborators can read. The local hooks and `make check-public` read it with your `gh` login and cache it for a day; without access they warn and apply the other rules. CI does not check these names, because its log is public.
- Codex does not load `.codex/hooks.json` in a git worktree: there only the pre-push hook checks, so `gh` posts and GitHub tool calls are not blocked. Post from a regular clone.
- If something private was pushed or posted: do not add a cleanup commit (it republishes the text and advertises it). Rewrite the branch with neutral messages and force-push, edit the text, delete every old revision in the GitHub UI ("edited" menu, open a revision, "Delete revision from history"), and ask a maintainer about a GitHub Support purge of the unreachable commits.
- Run `make setup` once per clone.

## Spec

`runware-cli-v1-spec.md` is the v1 feature specification. It defines the full command roadmap, API task type mappings, and architecture principles. Reference it when adding new commands or planning features. The tracking epic is Jira RUN-6978.

## Conventions

- Commands use camelCase (e.g. `imageInference`, `modelSearch`, `audioInference`)
- Commit messages follow conventional commits (`feat:`, `fix:`, `docs:`, `test:`)
- Tests live alongside source files (`*_test.go`)
- Config shorthand keys in CLI map to nested yaml paths (e.g. `model` → `defaults.model`)
- Run `make fix` after every change that touches Go code and retain its modernizations; `make check-go-fix` and CI reject code that `go fix` would change
- Run `go fmt` on any files you modify before considering work complete
- Run `make lint` before considering work complete; fix all lint errors
- Error naming follows https://go.dev/wiki/Errors: error types end in `"Error"` (e.g. `type NotTabularError struct`), error variables start with `"Err"` (e.g. `var ErrNotFound = errors.New(...)`)
- Use `map[string]struct{}` for set/presence maps, not `map[string]bool`
- Write map literals with one key per line for readability
- Write struct literals with one field per line for readability
- Do not use inline anonymous structs; define named types instead

## Release process

Tag with `vX.Y.Z` and push: GitHub Actions runs GoReleaser to build and publish.
