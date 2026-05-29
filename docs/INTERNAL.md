# Internal build

The public release of the Runware CLI is hard-locked to the production API endpoint
(`https://api.runware.ai/v1`). The base URL **cannot** be changed in a public binary — the
override code is excluded at compile time via a Go [build tag](https://pkg.go.dev/go/build#hdr-Build_Constraints)
so customers cannot redirect the CLI to staging or internal environments.

The override is available only in the **internal build**, produced with the `internal` build tag.

## Building

```bash
make build-internal      # → ./runware-internal (override enabled)
make test-internal       # run the test suite with -tags internal
```

Equivalent raw commands:

```bash
go build -tags internal -o runware-internal .
go test  -tags internal ./...
```

The default `make build` / `make test` (and the GoReleaser release in
`.github/workflows/release.yml`) build **without** the tag, producing the locked public binary.
Never add `-tags internal` to the public release.

## Overriding the API endpoint (internal build only)

In the internal build, the base URL is resolved in this order (first non-empty wins):

1. `RUNWARE_BASE_URL` environment variable
2. `base_url` key in `~/.runware/config.yaml` (e.g. `runware-internal config set base_url https://...`)
3. Built-in default `https://api.runware.ai/v1`

```bash
RUNWARE_BASE_URL=https://api.staging.example.com/v1 ./runware-internal ping
./runware-internal config set base_url https://api.staging.example.com/v1
```

In the public build, `RUNWARE_BASE_URL` is ignored and `config set base_url …` is rejected
with `unknown config key "base_url"`.

## Linting the tagged files

`golangci-lint` and `go vet` only analyze the files matching the active build context, so the
`internal`-tagged files are skipped by the default `make lint`. To lint them:

```bash
golangci-lint run --build-tags internal
go vet -tags internal ./...
```

## How it works

The override lives in files gated by `//go:build internal`, paired with `//go:build !internal`
no-op/locked variants:

| Concern | Public (`!internal`) | Internal (`internal`) |
|---------|----------------------|-----------------------|
| `internal/config/baseurl_*.go` | `GetBaseURL()` returns the default | `GetBaseURL()` honors env/config override |
| `internal/config/bindenv_*.go` | `bindInternalEnv()` is a no-op | binds `RUNWARE_BASE_URL` |
| `cmd/config_keys_*.go` | `base_url` omitted from `config set` / rejected | `base_url` settable |
