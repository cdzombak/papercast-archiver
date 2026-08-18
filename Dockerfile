ARG BIN_NAME=papercast-archiver
ARG BIN_VERSION=<unknown>

FROM golang:1-alpine AS builder
ARG BIN_NAME
ARG BIN_VERSION

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-X main.version=${BIN_VERSION}" -o ./out/${BIN_NAME} .

FROM scratch
ARG BIN_NAME
ARG BIN_VERSION

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /src/out/${BIN_NAME} /usr/bin/${BIN_NAME}

USER 1000:1000

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s \
    CMD ["/usr/bin/papercast-archiver", "healthcheck", "-config", "/config.yaml"]

ENTRYPOINT ["/usr/bin/papercast-archiver"]
CMD ["serve", "-config", "/config.yaml"]

LABEL license="MIT"
LABEL org.opencontainers.image.licenses="MIT"
LABEL maintainer="Chris Dzombak <https://www.dzombak.com>"
LABEL org.opencontainers.image.authors="Chris Dzombak <https://www.dzombak.com>"
LABEL org.opencontainers.image.url="https://github.com/cdzombak/papercast-archiver"
LABEL org.opencontainers.image.documentation="https://github.com/cdzombak/papercast-archiver"
LABEL org.opencontainers.image.source="https://github.com/cdzombak/papercast-archiver"
LABEL org.opencontainers.image.version="${BIN_VERSION}"
LABEL org.opencontainers.image.title="${BIN_NAME}"
LABEL org.opencontainers.image.description="Web companion to papercast that archives or deletes the Instapaper article behind an episode"
