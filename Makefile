VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS = \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.date=$(DATE)

GOFLAGS = -trimpath

# A local build reads RUNWARE_VOLUMES_STATEMENT_KEY for a non-production host.
# Only build and run set this tag; the cross-platform targets and the release
# (GoReleaser) never do, so a released binary trusts only its shipped keys.
DEV_TAGS = -tags volumesdevkeys

BINARY=runware

.PHONY: build build-all windows-amd64 windows-arm64 darwin darwin-arm64 darwin-amd64 linux-amd64 linux-arm64 run test lint fix check-go-fix clean install snapshot docs go-tidy generate-serverless sync-serverless-spec setup check-public

build:
	go build $(GOFLAGS) $(DEV_TAGS) -ldflags "$(LDFLAGS)" -o bin/${BINARY} ./cmd/runware

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
	go run $(DEV_TAGS) -ldflags "$(LDFLAGS)" ./cmd/runware $(ARGS)

test:
	go test -race ./...
	go test -race $(DEV_TAGS) ./internal/volumes/keypair/

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

# Run once per clone, and again after pulling changes to tools/checkpublic:
# builds the checker into the clone's common git directory and installs a
# pre-push hook there that runs that copy, so a push never runs code from the
# branch being pushed. Every worktree of the clone shares both.
CHECKPUBLIC_HOOK_MARKER = installed by make setup: runs tools/checkpublic

setup:
	@set -e; \
	common=$$(git rev-parse --path-format=absolute --git-common-dir); \
	bin="$$common/checkpublic/bin/checkpublic"; \
	hook="$$common/hooks/pre-push"; \
	if [ -e "$$hook" ] && ! grep -q '$(CHECKPUBLIC_HOOK_MARKER)' "$$hook"; then \
		echo "make setup: $$hook exists and was not installed by make setup; move it aside and run make setup again" >&2; \
		exit 1; \
	fi; \
	mkdir -p "$$common/checkpublic/bin" "$$common/hooks"; \
	go build -o "$$bin.new" ./tools/checkpublic; \
	mv -f "$$bin.new" "$$bin"; \
	{ \
		echo '#!/bin/sh'; \
		echo '# $(CHECKPUBLIC_HOOK_MARKER) in prepush mode.'; \
		echo '# Run make setup again after pulling changes to the checker.'; \
		echo 'bin="$$(git rev-parse --path-format=absolute --git-common-dir)/checkpublic/bin/checkpublic"'; \
		echo 'if [ ! -x "$$bin" ]; then'; \
		echo '	echo "checkpublic: $$bin is missing; run make setup" >&2'; \
		echo '	exit 1'; \
		echo 'fi'; \
		echo 'exec "$$bin" prepush "$$@"'; \
	} > "$$hook.new"; \
	chmod 755 "$$hook.new"; \
	mv -f "$$hook.new" "$$hook"; \
	hooks_path=$$(git config --get core.hooksPath || true); \
	if [ -n "$$hooks_path" ]; then \
		echo "make setup: core.hooksPath is set to $$hooks_path, and git does not run hooks in the git directory, such as $$hook, while it is set, so the pre-push check would never run; unset it with git config --unset core.hooksPath (add --global if it is set there) and run make setup again" >&2; \
		exit 1; \
	fi; \
	echo "make setup: installed $$hook, running the checker built from commit $$(git rev-parse --short HEAD)"

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
