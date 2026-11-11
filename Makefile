BINARY := rss-tui
GO ?= go

.PHONY: build run passive test vet fmt check clean

build:
	$(GO) build -o $(BINARY) ./cmd/rss-tui

run:
	$(GO) run ./cmd/rss-tui

passive:
	$(GO) run ./cmd/rss-tui --passive

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w ./cmd ./internal

check: fmt vet test

clean:
	rm -f $(BINARY)
