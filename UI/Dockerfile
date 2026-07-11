FROM alpine:3.23 AS builder
WORKDIR /app
RUN apk add --no-cache nodejs npm
COPY package*.json ./
RUN npm ci
COPY . .
ARG PUBLIC_DEMO_MODE=false
ARG PUBLIC_DEMO_USERS
ENV PUBLIC_DEMO_MODE=$PUBLIC_DEMO_MODE
ENV PUBLIC_DEMO_USERS=$PUBLIC_DEMO_USERS
RUN npm run build

FROM golang:1.26.5-alpine AS caddy-builder
RUN apk add --no-cache git
RUN go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest
RUN xcaddy build latest

FROM alpine:3.23
RUN apk update && apk upgrade --no-cache
COPY --from=caddy-builder /go/caddy /usr/bin/caddy
COPY --from=builder /app/build /srv
COPY Caddyfile /etc/caddy/Caddyfile
EXPOSE 3000
CMD ["caddy", "run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]
