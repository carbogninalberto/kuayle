FROM alpine:3.23 AS builder
WORKDIR /app
ARG SECURITY_REFRESH=initial
RUN apk add --no-cache nodejs npm
COPY package*.json ./
RUN npm ci
COPY . .
ARG PUBLIC_DEMO_MODE=false
ARG PUBLIC_DEMO_USERS
ENV PUBLIC_DEMO_MODE=$PUBLIC_DEMO_MODE
ENV PUBLIC_DEMO_USERS=$PUBLIC_DEMO_USERS
RUN npm run build

FROM golang:1.26.8-alpine AS caddy-builder
ARG SECURITY_REFRESH=initial
RUN apk add --no-cache git
RUN go install github.com/caddyserver/xcaddy/cmd/xcaddy@v0.4.6
RUN xcaddy build v2.11.4 \
    --with google.golang.org/grpc@v1.83.2 \
    --replace golang.org/x/text=golang.org/x/text@v0.41.0 \
    --replace golang.org/x/net=golang.org/x/net@v0.57.0 \
    --replace golang.org/x/crypto=golang.org/x/crypto@v0.55.0

FROM alpine:3.23
ARG SECURITY_REFRESH=initial
RUN apk update && apk upgrade --no-cache
COPY --from=caddy-builder /go/caddy /usr/bin/caddy
COPY --from=builder /app/build /srv
COPY Caddyfile /etc/caddy/Caddyfile
EXPOSE 3000
CMD ["caddy", "run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]
