# syntax=docker/dockerfile:1.7

FROM golang:1.24-bookworm AS build

WORKDIR /src

RUN apt-get update \
	&& apt-get install -y --no-install-recommends libolm-dev \
	&& rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
	go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN --mount=type=cache,target=/go/pkg/mod \
	--mount=type=cache,target=/root/.cache/go-build \
	CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/easymatrix ./cmd/server

FROM debian:bookworm-slim AS runtime

RUN apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates libolm3 \
	&& rm -rf /var/lib/apt/lists/* \
	&& groupadd --system easymatrix \
	&& useradd --system --gid easymatrix --home-dir /data --create-home --shell /usr/sbin/nologin easymatrix \
	&& mkdir -p /data/gomuks \
	&& chown -R easymatrix:easymatrix /data

WORKDIR /data

COPY --from=build /out/easymatrix /usr/local/bin/easymatrix

ENV GOMUKS_ROOT=/data/gomuks

USER easymatrix:easymatrix

EXPOSE 8080

CMD ["easymatrix"]
