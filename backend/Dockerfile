FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
# modernc.org/sqlite is pure Go, so the binary is static.
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/server ./cmd/server

FROM registry.access.redhat.com/ubi9/ubi-minimal:latest
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
