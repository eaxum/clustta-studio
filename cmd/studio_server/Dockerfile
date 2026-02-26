# syntax=docker/dockerfile:1

# ── Build stage ──────────────────────────────────────────────
FROM golang:1.22-bookworm AS build-stage

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
# CGO is required for go-sqlite3. No GOARCH override needed —
# Docker buildx sets the correct target platform automatically.
RUN CGO_ENABLED=1 go build \
    -ldflags "-s -w -X main.Version=${VERSION}" \
    -o /clustta_server ./cmd/studio_server

# ── Release stage ────────────────────────────────────────────
FROM debian:bookworm-slim AS build-release-stage

ENV PROJECTS_DIR=/var/projects/
ENV DATA_DIR=/var/data/
ENV SERVER_HOST=0.0.0.0
ENV SERVER_PORT=7774

RUN mkdir -p /var/projects /var/data \
    && adduser --disabled-password --gecos "" eaxum

WORKDIR /usr/bin

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build-stage /clustta_server ./clustta_server

EXPOSE 7774

RUN chown -R eaxum:eaxum /var/projects/ /var/data/ /usr/bin

USER eaxum

ENTRYPOINT ["./clustta_server", "studio"]