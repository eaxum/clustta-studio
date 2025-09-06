# syntax=docker/dockerfile:1

# Build the application from source
FROM golang:1.22 AS build-stage

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

# COPY *.go ./
# Copy the rest of the application's source code
COPY . .

RUN CGO_ENABLED=1 GOOS=linux go build -o /clustta_server ./cmd/studio_server

# Run the tests in the container
# FROM build-stage AS run-test-stage
# RUN go test -v ./...

# Deploy the application binary into a lean image
# Use a Debian image with a compatible glibc version
FROM debian:bookworm-slim AS build-release-stage

ENV PROJECTS_DIR=/var/projects/
ENV DATA_DIR=/var/data/
ENV SERVER_HOST=0.0.0.0
ENV SERVER_PORT=5000

RUN mkdir -p /var/projects
RUN mkdir -p /var/data
RUN adduser eaxum

WORKDIR /usr/bin

RUN apt-get update && apt-get install -y \
    ca-certificates \
    dbus \
    dbus-x11 \
    gnome-keyring \
    libsecret-1-0 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build-stage /clustta_server ./clustta_server
EXPOSE 8080

RUN chown -R eaxum:eaxum /var/projects/
RUN chown -R eaxum:eaxum /var/data/
RUN chown -R eaxum:eaxum /usr/bin

USER eaxum

ENTRYPOINT ["./clustta_server", "studio"]