# syntax=docker/dockerfile:1

# --- build stage -------------------------------------------------------------
FROM golang:1.27-alpine AS build
WORKDIR /src

# go.sum comes with go.mod. Without it `go mod download` cannot verify checksums, so it
# writes its own go.sum and this layer caches on an unverified module graph.
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Stamped so the running container can say which revision it is on. The .git directory
# is not in the build context, so the caller supplies it — `make docker` passes
# `git describe` automatically, and a plain `docker build` gets the honest "dev".
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/smart-serve ./cmd/smartserve

# --- runtime stage -----------------------------------------------------------
# smartmontools (GPLv2) is bundled here; see docs/DECISIONS.md ADR-012.
# util-linux provides lsblk.
FROM alpine:3.20
RUN apk add --no-cache smartmontools util-linux ca-certificates tzdata

COPY --from=build /out/smart-serve /usr/local/bin/smart-serve

# The container reads raw device nodes (/dev/...) and therefore needs device
# access; see docs/DEPLOYMENT.md for the privilege options.
VOLUME ["/data"]
EXPOSE 8080

ENV SMARTSERVE_ADDR=":8080" \
    SMARTSERVE_DB="/data/smartserve.db"

# Ask the HTTP server, not the binary. `smart-serve version` exits 0 for a process whose
# server is wedged, so it would report healthy while the API is unreachable.
#
# The address may carry a host (`0.0.0.0:8080`), so everything up to the last colon is
# stripped. The base path is normalised to a leading slash and no trailing one — the
# same shape the server itself expects (ADR-015) — so an unset base path yields
# `/healthz` and `/smartserve` yields `/smartserve/healthz`.
HEALTHCHECK --interval=60s --timeout=5s --start-period=10s --retries=3 CMD \
  b="/${SMARTSERVE_BASE_PATH#/}"; b="${b%/}"; \
  wget -qO- "http://127.0.0.1:${SMARTSERVE_ADDR##*:}$b/healthz" > /dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/smart-serve"]
CMD ["serve"]
