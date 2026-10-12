# syntax=docker/dockerfile:1

# --- build stage -------------------------------------------------------------
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/smart-serve ./cmd/smartserve

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

HEALTHCHECK --interval=60s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/usr/local/bin/smart-serve", "version"]

ENTRYPOINT ["/usr/local/bin/smart-serve"]
CMD ["serve"]
