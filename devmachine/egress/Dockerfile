FROM golang:1.26.11-alpine AS build
WORKDIR /src
COPY BE/go.mod BE/go.sum ./
COPY BE/cmd/machine-egress/main.go ./cmd/machine-egress/main.go
COPY BE/pkg/validate/dial.go ./pkg/validate/dial.go
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /egress ./cmd/machine-egress

FROM alpine:3.23
ARG SECURITY_REFRESH=initial
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates \
    && addgroup -g 1000 kuayle \
    && adduser -D -u 1000 -G kuayle kuayle
COPY --from=build /egress /usr/local/bin/kuayle-egress
COPY devmachine/common/kuayle-service-entrypoint /usr/local/bin/kuayle-service-entrypoint
COPY devmachine/common/start-common.sh /usr/local/lib/kuayle/start-common.sh
RUN chmod 0755 /usr/local/bin/kuayle-service-entrypoint /usr/local/bin/kuayle-egress

USER 1000:1000
EXPOSE 3128
CMD ["/usr/local/bin/kuayle-egress"]
