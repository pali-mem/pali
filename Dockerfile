# syntax=docker/dockerfile:1.7

FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY cmd/ ./cmd/
COPY deploy/ ./deploy/
COPY internal/ ./internal/
COPY pkg/ ./pkg/
COPY web/ ./web/

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w" -o /out/pali ./cmd/pali

FROM alpine:3.21

ARG VERSION=dev
ARG REVISION=unknown
ARG CREATED=unknown

LABEL org.opencontainers.image.title="Pali" \
      org.opencontainers.image.description="Open memory infrastructure for LLMs." \
      org.opencontainers.image.source="https://github.com/pali-mem/pali" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version=$VERSION \
      org.opencontainers.image.revision=$REVISION \
      org.opencontainers.image.created=$CREATED

RUN apk add --no-cache ca-certificates tzdata wget && \
    addgroup -S pali && \
    adduser -S -G pali -h /var/lib/pali pali && \
    mkdir -p /app /etc/pali /var/lib/pali

WORKDIR /app

COPY --from=builder /out/pali /app/pali
COPY deploy/docker/pali.container.yaml /etc/pali/pali.yaml

RUN chown -R pali:pali /app /etc/pali /var/lib/pali

USER pali

EXPOSE 8080
VOLUME ["/var/lib/pali"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/health || exit 1

ENTRYPOINT ["/app/pali"]
CMD ["-config", "/etc/pali/pali.yaml"]
