VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS = -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.date=$(DATE)

GOFLAGS = -trimpath

BINARY=runware

.PHONY: build build-all windows-amd64 windows-arm64 darwin darwin-arm64 darwin-amd64 linux-amd64 linux-arm64 run test lint clean install snapshot docs go-tidy

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

lint:
	golangci-lint run

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
