# Detect operating system
ifeq ($(OS),Windows_NT)
    DETECTED_OS := Windows
    BINARY_EXT := .exe
    PATH_SEP := \\
else
    DETECTED_OS := $(shell uname -s)
    BINARY_EXT :=
    PATH_SEP := /
endif

# Default target
.PHONY: all
all: build

# Run development environment for studio
.PHONY: studio
studio:
ifeq ($(DETECTED_OS),Windows)
	air -c .\.studio_server_air.toml
else
	air -c ./.studio_server_air_darwin.toml
endif


# install dev dependencies
.PHONY: install-dev
install-dev:
	go install -v github.com/wailsapp/wails/v3/cmd/wails3@latest

# Create/overwrite a git tag and push to trigger CI
# Usage: make tag V=0.4.24-alpha
.PHONY: tag
tag:
ifndef V
	$(error Usage: make tag V=0.4.24-alpha)
endif
	git tag -f v$(V)
	git push origin -f v$(V)