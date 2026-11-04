FROM golang:1.26.10-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# The release the assistant reports; release builds pass it, local builds report "dev".
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-X github.com/kuayle/kuayle-backend/internal/buildinfo.Version=${VERSION}" -o server ./cmd/server

FROM alpine:3.23
ARG SECURITY_REFRESH=initial
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates && \
    addgroup -S appgroup && adduser -S appuser -G appgroup
WORKDIR /app
COPY --from=builder /app/server .
COPY --from=builder /app/migrations ./migrations
RUN mkdir -p /app/uploads && chown -R appuser:appgroup /app
USER appuser
EXPOSE 8080
CMD ["./server"]
