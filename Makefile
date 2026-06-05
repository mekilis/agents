BINARY      := agents
PKG         := ./cmd/agents
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE        ?= $(shell date -u +%Y-%m-%d)
LDFLAGS     := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: build install test race vet fmt fmtcheck check clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

install:
	go install -ldflags "$(LDFLAGS)" $(PKG)

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

fmtcheck:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needed on:"; echo "$$unformatted"; exit 1; \
	fi

check: fmtcheck vet test

clean:
	rm -f $(BINARY)
