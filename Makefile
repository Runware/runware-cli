VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS = \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.date=$(DATE)

GOFLAGS = -trimpath

BINARY=runware

.PHONY: build build-all windows-amd64 windows-arm64 darwin darwin-arm64 darwin-amd64 linux-amd64 linux-arm64 run test lint fix check-go-fix clean install snapshot docs go-tidy generate-serverless sync-serverless-spec setup check-public

build:
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/${BINARY} ./cmd/runware

build-all: windows-amd64 windows-arm64 darwin linux-amd64 linux-arm64

windows-amd64:
	GOARCH=amd64 GOOS=windows go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/${BINARY}-windows-amd64.exe ./cmd/runware

windows-arm64:
	GOARCH=arm64 GOOS=windows go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/${BINARY}-windows-arm64.exe ./cmd/runware

darwin: darwin-arm64 darwin-amd64

darwin-arm64:
	GOARCH=arm64 GOOS=darwin go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/${BINARY}-darwin-arm64 ./cmd/runware

darwin-amd64:
	GOARCH=amd64 GOOS=darwin go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/${BINARY}-darwin-amd64 ./cmd/runware

linux-amd64:
	GOARCH=amd64 GOOS=linux go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/${BINARY}-linux-amd64 ./cmd/runware

linux-arm64:
	GOARCH=arm64 GOOS=linux go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/${BINARY}-linux-arm64 ./cmd/runware

run:
	go run -ldflags "$(LDFLAGS)" ./cmd/runware $(ARGS)

test:
	go test -race ./...

lint: check-go-fix
	golangci-lint run

fix:
	go fix ./...

check-go-fix:
	go fix -diff ./...

clean:
	rm -rf bin dist

install:
	go install $(GOFLAGS) -ldflags "$(LDFLAGS)" ./cmd/runware

snapshot:
	goreleaser build --snapshot --clean

docs:
	rm -rf ./docs && go run ./tools/docgen -out ./docs -format markdown

go-tidy:
	go mod tidy && go mod verify

# Run once per clone: enables the hooks in .githooks (pre-push runs check-public).
setup:
	git config core.hooksPath .githooks

# Check what this branch would publish (diff, commit messages and branch name
# since the merge-base with origin/main) for text that must not be public.
check-public:
	go run ./tools/checkpublic range

# Regenerate the Serverless API client from api/serverless/openapi.yaml.
generate-serverless:
	go tool oapi-codegen -config api/serverless/oapi-codegen.yaml api/serverless/openapi.yaml

# Copy the published Serverless API bundle from a serverless checkout and
# regenerate the client. The source openapi.yaml there is not the published
# contract: copy only bin/openapi.public.yaml, which `make bundle-api-spec`
# builds. Usage: make sync-serverless-spec SERVERLESS_DIR=../serverless
sync-serverless-spec:
	@test -n "$(SERVERLESS_DIR)" || (echo "set SERVERLESS_DIR to a serverless checkout" >&2; exit 1)
	$(MAKE) -C $(SERVERLESS_DIR) bundle-api-spec
	cp $(SERVERLESS_DIR)/bin/openapi.public.yaml api/serverless/openapi.yaml
	$(MAKE) generate-serverless
