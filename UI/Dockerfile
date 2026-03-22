FROM node:22-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM node:22-alpine
RUN apk update && apk upgrade --no-cache && npm install -g npm@latest
RUN addgroup -S appgroup && adduser -S appuser -G appgroup
WORKDIR /app
COPY --from=builder /app/build ./build
COPY --from=builder /app/package*.json ./
RUN npm ci --omit=dev && chown -R appuser:appgroup /app
USER appuser
EXPOSE 5173
CMD ["npx", "sirv", "build", "--host", "0.0.0.0", "--port", "5173", "--single"]
