FROM golang:1.25.12-alpine AS build
WORKDIR /src
COPY collector/main.go .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /collector main.go

FROM alpine:3.22
RUN apk add --no-cache ca-certificates coreutils git \
    && addgroup -g 1000 kuayle \
    && adduser -D -u 1000 -G kuayle kuayle
COPY --from=build /collector /usr/local/bin/kuayle-collector
COPY common/kuayle-service-entrypoint /usr/local/bin/kuayle-service-entrypoint
RUN chmod 0755 /usr/local/bin/kuayle-service-entrypoint /usr/local/bin/kuayle-collector

USER 1000:1000
ENV HOME=/home/kuayle
EXPOSE 8091
CMD ["/usr/local/bin/kuayle-collector"]
