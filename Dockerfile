# syntax=docker/dockerfile:1

FROM golang:1.26.11-alpine3.24 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /geoexporter .

FROM alpine:3.24

# ca-certificates-bundle is already in the alpine base.
RUN apk add --no-cache curl && \
    adduser -D -H -s /sbin/nologin -u 10001 geoexporter && \
    mkdir -p /usr/share/GeoIP && chown geoexporter /usr/share/GeoIP

COPY --from=build --chown=root:root /geoexporter /usr/local/bin/geoexporter

USER geoexporter

EXPOSE 9123

ENTRYPOINT ["geoexporter"]
