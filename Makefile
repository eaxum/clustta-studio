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

# Build Windows installer using Inno Setup
# Version is auto-detected from the latest git tag.
# Override: make build V=1.0.0
.PHONY: build
build:
ifeq ($(DETECTED_OS),Windows)
ifdef V
	cmd /c build_installer.bat $(V)
else
	cmd /c build_installer.bat
endif
	powershell -ExecutionPolicy Bypass -File ./windows-server-sign.ps1
else
	$(error Windows installer can only be built on Windows)
endif

# Create/overwrite a git tag and push to trigger CI
# Usage: make tag V=0.4.24-alpha
.PHONY: tag
tag:
ifndef V
	$(error Usage: make tag V=0.4.24-alpha)
endif
	git tag -f v$(V)
	git push origin -f v$(V)