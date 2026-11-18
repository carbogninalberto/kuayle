FROM golang:1.26.11-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# The release the assistant reports; release builds pass it, local builds report "dev".
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-X github.com/kuayle/kuayle-backend/internal/buildinfo.Version=${VERSION}" -o server ./cmd/server

FROM --platform=$BUILDPLATFORM alpine:3.23 AS codex-login
ARG TARGETARCH
COPY scripts/install-codex-login.sh /tmp/install-codex-login.sh
RUN apk add --no-cache openssl && sh /tmp/install-codex-login.sh "$TARGETARCH"

FROM alpine:3.23
COPY --from=codex-login /usr/local/bin/codex /usr/local/bin/codex
RUN codex --version
ARG SECURITY_REFRESH=initial
# postgresql17-client provides pg_dump for Settings -> Backups; keep its major
# version equal to the postgres image in selfhosting/docker-compose.yml.
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates postgresql17-client && \
    addgroup -S appgroup && adduser -S appuser -G appgroup
WORKDIR /app
COPY --from=builder /app/server .
COPY --from=builder /app/migrations ./migrations
RUN mkdir -p /app/uploads /app/backups && chmod 700 /app/backups && chown -R appuser:appgroup /app
USER appuser
EXPOSE 8080
CMD ["./server"]
