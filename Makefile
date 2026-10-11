GO      ?= go
BINARY  ?= smart-serve

.PHONY: all build test vet fmt run clean docker

all: fmt vet test build

build:
	$(GO) build -trimpath -ldflags "-s -w" -o bin/$(BINARY) ./cmd/smartserve

test:
	$(GO) test ./...

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
