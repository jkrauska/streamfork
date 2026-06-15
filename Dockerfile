# syntax=docker/dockerfile:1

FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /streamfork ./cmd/streamfork

# trixie ships ffmpeg 7.x (bookworm is 5.x — too old for Enhanced RTMP HEVC copy).
FROM debian:trixie-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ffmpeg tini ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=bluenviron/mediamtx:1.11.3 /mediamtx /usr/local/bin/mediamtx
COPY --from=build /streamfork /usr/local/bin/streamfork

EXPOSE 8787 8854 8935 8890/udp
VOLUME ["/data"]

ENV STREAMFORK_CONFIG=/data/streamfork.yml

ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/streamfork"]
