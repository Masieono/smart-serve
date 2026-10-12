GO      ?= go
NODE    ?= node
BINARY  ?= smart-serve

.PHONY: all build test test-js vet fmt run clean docker

all: fmt vet test build

build:
	$(GO) build -trimpath -ldflags "-s -w" -o bin/$(BINARY) ./cmd/smartserve

test:
	$(GO) test ./...

test-js:
	$(NODE) --test internal/webui/jstest/

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

run:
	$(GO) run ./cmd/smartserve scan

clean:
	rm -rf bin

docker:
	docker build -t smart-serve:dev .
