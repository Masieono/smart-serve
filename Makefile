GO      ?= go
NODE    ?= node
BINARY  ?= smart-serve
# Explicit file paths, never the directory: `node --test <dir>` only traverses
# directories on Node 26+, and on the current LTS it treats the path as a single
# module and fails with MODULE_NOT_FOUND.
JSTEST  ?= internal/webui/jstest/*.test.mjs

# Stamped into the binary so a running container can say which revision it is on.
# `git describe` gives a tag (v1.2.0) once tags exist, and degrades to a bare commit id
# before then — either way it identifies the build, which "0.0.0-dev" never did.
# Override it for a release: make build VERSION=v1.0.0
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  = -s -w -X main.version=$(VERSION)

.PHONY: all build test test-js vet fmt run clean docker ci

all: fmt vet test build

build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/smartserve

test:
	$(GO) test ./...

test-js:
	$(NODE) --test $(JSTEST)

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

# ci runs exactly what .github/workflows/ci.yml runs, so a green run here means a
# green run there. Keep the two in step — a CI-only failure is expensive to debug.
ci:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then echo "gofmt wants these formatted:"; echo "$$unformatted"; exit 1; fi
	$(GO) vet ./...
	$(GO) test -count=1 ./...
	$(NODE) --test $(JSTEST)

run:
	$(GO) run ./cmd/smartserve scan

clean:
	rm -rf bin

docker:
	docker build --build-arg VERSION=$(VERSION) -t smart-serve:dev .
