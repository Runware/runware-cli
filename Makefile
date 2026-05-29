VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS = -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.date=$(DATE)

.PHONY: build run test lint clean install snapshot docs

build:
	go build -ldflags "$(LDFLAGS)" -o runware .

run:
	go run -ldflags "$(LDFLAGS)" . $(ARGS)

test:
	go test -race ./...

lint:
	golangci-lint run

clean:
	rm -f runware

install:
	go install -ldflags "$(LDFLAGS)" .

snapshot:
	goreleaser build --snapshot --clean

docs:
	go run ./internal/tools/docgen -out ./docs -format markdown
