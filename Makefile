VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS = -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.date=$(DATE)

.PHONY: build build-internal run test test-internal lint clean install snapshot

build:
	go build -ldflags "$(LDFLAGS)" -o runware .

# build-internal produces the internal build (build tag: internal), which enables
# the RUNWARE_BASE_URL / base_url API endpoint override. This binary is for
# internal use only and must never be published in a public release.
build-internal:
	go build -tags internal -ldflags "$(LDFLAGS)" -o runware-internal .

run:
	go run -ldflags "$(LDFLAGS)" . $(ARGS)

test:
	go test -race ./...

test-internal:
	go test -tags internal -race ./...

lint:
	golangci-lint run

clean:
	rm -f runware runware-internal

install:
	go install -ldflags "$(LDFLAGS)" .

snapshot:
	goreleaser build --snapshot --clean
