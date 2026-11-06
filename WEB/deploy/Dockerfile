# Builds and serves the prerendered marketing site.
# Build from the WEB directory: docker build -f deploy/Dockerfile -t kuayle-web .
FROM node:22-alpine AS builder
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
# validate = unit tests + build + SEO checks; the build resolves the current release from the manifest.
RUN npm run validate

FROM caddy:2-alpine
COPY --from=builder /app/build /srv
COPY deploy/Caddyfile /etc/caddy/Caddyfile
EXPOSE 80
CMD ["caddy", "run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]
