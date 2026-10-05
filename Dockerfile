# Stage 1: Build UI
FROM node:22-alpine AS ui-builder
WORKDIR /app
COPY UI/package*.json ./
RUN npm ci
COPY UI/ .
RUN npm run build

# Stage 2: Build Caddy from source with patched Go
FROM golang:1.26.8-alpine AS caddy-builder
ARG SECURITY_REFRESH=initial
RUN apk add --no-cache git
RUN go install github.com/caddyserver/xcaddy/cmd/xcaddy@v0.4.6
RUN xcaddy build v2.11.4 --with google.golang.org/grpc@v1.83.2 --replace golang.org/x/text=golang.org/x/text@v0.41.0 \
    --replace golang.org/x/net=golang.org/x/net@v0.57.0 \
    --replace golang.org/x/crypto=golang.org/x/crypto@v0.55.0

# Stage 3: Build backend
FROM golang:1.26.8-alpine AS be-builder
WORKDIR /app
COPY BE/go.mod BE/go.sum ./
RUN go mod download
COPY BE/ .
RUN CGO_ENABLED=0 go build -o server ./cmd/server

# Stage 4: Final image
FROM alpine:3.23
ARG SECURITY_REFRESH=initial
RUN apk update && apk upgrade --no-cache && apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=caddy-builder /go/caddy /usr/bin/caddy
COPY --from=be-builder /app/server .
COPY --from=be-builder /app/migrations ./migrations
COPY --from=ui-builder /app/build /srv
RUN mkdir -p /app/uploads

COPY <<'EOF' /etc/caddy/Caddyfile
:3000 {
	root * /srv
	file_server
	try_files {path} /index.html

	header {
		X-Content-Type-Options nosniff
		X-Frame-Options DENY
		Referrer-Policy strict-origin-when-cross-origin
	}

	handle /api/* {
		reverse_proxy localhost:8080
	}

	handle /uploads/* {
		reverse_proxy localhost:8080
	}

	handle /health {
		reverse_proxy localhost:8080
	}

	handle /ready {
		reverse_proxy localhost:8080
	}
}
EOF

COPY <<'ENTRY' /app/entrypoint.sh
#!/bin/sh
./server &
exec caddy run --config /etc/caddy/Caddyfile --adapter caddyfile
ENTRY
RUN chmod +x /app/entrypoint.sh

EXPOSE 3000
CMD ["/app/entrypoint.sh"]
