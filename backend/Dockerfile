# The build runs on the builder's own platform and cross-compiles for the
# target, so a multi-arch image does not need emulation. Base images are
# pinned to a digest (the tag is kept for readability) so that a release says
# exactly what it was built from; Dependabot proposes the updates, moving the
# digest and the tag together. The Go version is the toolchain line of
# go.mod, which CI tests with: raise both together.
FROM --platform=$BUILDPLATFORM golang:1.26.11@sha256:7af425da11f4fc1a9308eb704369e3f6d1c44e35bac0f874a59ade3f20fdc07e AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG TARGETOS=linux
ARG TARGETARCH
# modernc.org/sqlite is pure Go, so the binary is static.
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/server ./cmd/server

FROM registry.access.redhat.com/ubi9/ubi-minimal:1794186410@sha256:f26ef64172ea61f5ac5ba4be8c5c738bf8760b22b3e5148ce7b40f7308ab32db
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
