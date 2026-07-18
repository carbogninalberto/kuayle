FROM golang:1.25.12-alpine AS build
WORKDIR /src
COPY egress/main.go .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /egress main.go

FROM alpine:3.22
RUN apk add --no-cache ca-certificates \
    && addgroup -g 1000 kuayle \
    && adduser -D -u 1000 -G kuayle kuayle
COPY --from=build /egress /usr/local/bin/kuayle-egress
COPY common/kuayle-service-entrypoint /usr/local/bin/kuayle-service-entrypoint
RUN chmod 0755 /usr/local/bin/kuayle-service-entrypoint /usr/local/bin/kuayle-egress

USER 1000:1000
EXPOSE 3128
CMD ["/usr/local/bin/kuayle-egress"]
