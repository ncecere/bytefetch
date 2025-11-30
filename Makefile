GO ?= go
CONFIG ?= config.dev.yaml
BIN ?= bin/crawler

.PHONY: build run-api run-worker run-all test tidy

build:
	$(GO) build -o $(BIN) ./cmd/crawler

run-api:
	$(GO) run ./cmd/crawler --mode=api --config=$(CONFIG)

run-worker:
	$(GO) run ./cmd/crawler --mode=worker --config=$(CONFIG)

run-all:
	$(GO) run ./cmd/crawler --mode=all --config=$(CONFIG)

test:
	$(GO) test ./...

tidy:
	$(GO) mod tidy
