# The build runs on the builder's own platform and cross-compiles for the
# target, so a multi-arch image does not need emulation. Base images are
# pinned to exact tags so that a release says what it was built from;
# Dependabot proposes the updates. The Go version is the toolchain line of
# go.mod, which CI tests with: raise both together.
FROM --platform=$BUILDPLATFORM golang:1.26.9 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG TARGETOS=linux
ARG TARGETARCH
# modernc.org/sqlite is pure Go, so the binary is static.
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/server ./cmd/server

FROM registry.access.redhat.com/ubi9/ubi-minimal:9.8-1791898448
COPY --from=build /out/server /usr/local/bin/server
# OpenShift runs the container with an arbitrary UID in group 0, so the data
# directory is owned by group 0 and group-writable.
RUN mkdir /data && chgrp 0 /data && chmod g+rwX /data
ENV UPTIME_ADDR=:8080 \
    UPTIME_DB_PATH=/data/uptime.db
VOLUME /data
EXPOSE 8080
USER 1001
ENTRYPOINT ["/usr/local/bin/server"]
