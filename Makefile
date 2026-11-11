BINARY := rss-tui
GO ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build run passive test race vet fmt fmt-check check clean

build:
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/rss-tui

run:
	$(GO) run ./cmd/rss-tui

passive:
	$(GO) run ./cmd/rss-tui --passive

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w ./cmd ./internal

fmt-check:
	@test -z "$$(gofmt -l ./cmd ./internal)" || { \
		echo "unformatted files:"; gofmt -l ./cmd ./internal; exit 1; }

check: fmt-check vet test race build

clean:
	rm -f $(BINARY)
