# Hector Makefile
# Build and release management for the Hector AI agent platform

.PHONY: help build build-release build-studio build-unrestricted install test clean fmt vet lint release version test-coverage test-coverage-summary test-package test-race test-verbose dev ci install-lint quality pre-commit build-ui

# Build flags
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS_VERSION := -X 'github.com/verikod/hector.Version=$(VERSION)' -X 'github.com/verikod/hector.BuildDate=$(BUILD_DATE)' -X 'github.com/verikod/hector.GitCommit=$(GIT_COMMIT)'
LDFLAGS_RELEASE := -s -w $(LDFLAGS_VERSION)

# Default target
help:
	@echo "Hector Build System"
	@echo ""
	@echo "Available targets:"
	@echo "  build              - Build the hector binary (development with debug symbols)"
	@echo "  build-release      - Build the hector binary (production, stripped)"
	@echo "  build-studio       - Build for Hector Studio (production, sandbox enforced)"
	@echo "  build-unrestricted - Build with sandbox disabled (DANGEROUS - advanced users only)"
	@echo "  install            - Install hector to GOPATH/bin"
	@echo "  test      - Run all tests"
	@echo "  test-coverage - Run tests with coverage report"
	@echo "  test-coverage-summary - Run tests with coverage summary"
	@echo "  test-package - Run tests for specific package (PACKAGE=pkg/name)"
	@echo "  test-race - Run tests with race detection"
	@echo "  test-verbose - Run tests with verbose output"
	@echo "  clean     - Clean build artifacts"
	@echo "  fmt       - Format Go code"
	@echo "  vet       - Run go vet"
	@echo "  lint      - Run golangci-lint (if installed)"
	@echo "  release   - Build release binaries"
	@echo "  version   - Show version information"
	@echo "  deps      - Download dependencies"
	@echo "  mod-tidy  - Tidy go.mod"
	@echo ""
	@echo "Configuration:"
	@echo "  validate-configs    - Validate all example configs"
	@echo "  validate-configs    - Validate all example configs"



# Build the binary (development with debug symbols)
build:
	@echo "Building hector (development)..."
	go build -ldflags "$(LDFLAGS_VERSION)" -o hector ./cmd/hector
	@ls -lh hector

# Build the binary (production, stripped)
build-release:
	@echo "Building hector (production - stripped)..."
	@echo "Generating embedded assets..."
	@echo "Building hector (production - stripped)..."
	go build -ldflags "$(LDFLAGS_RELEASE)" -o hector ./cmd/hector
	@ls -lh hector
	@echo "Binary size optimized for production (debug symbols stripped)"

# Build for Hector Studio distribution (sandbox mode permanently enforced)
# This is the default build - SandboxEnforced = true
build-studio: build-release
	@echo ""
	@echo "✅ Studio build complete (sandbox mode enforced)"
	@echo "   Dangerous commands (rm, sudo, etc.) are permanently blocked."

# Build with sandbox disabled (DANGEROUS - for advanced users)
# WARNING: This allows config to completely override security defaults
build-unrestricted:
	@echo "⚠️  WARNING: Building with UNRESTRICTED mode!"
	@echo "    This build allows config to bypass sandbox protections."
	@echo "    Only use in controlled environments."
	@echo ""
	go build -tags=unrestricted -ldflags "$(LDFLAGS_RELEASE)" -o hector-unrestricted ./cmd/hector
	@ls -lh hector-unrestricted
	@echo ""
	@echo "⚠️  UNRESTRICTED build complete: hector-unrestricted"
	@echo "    This binary allows full command execution - use with caution!"

# Install to GOPATH/bin (production build)
install:
	@echo "Installing hector (production)..."
	@echo "Generating embedded assets..."
	@echo "Installing hector (production)..."
	go install -ldflags "$(LDFLAGS_RELEASE)" ./cmd/hector

# Install to system PATH (requires sudo)
install-system:
	@echo "Installing hector to /usr/local/bin..."
	@sudo cp hector /usr/local/bin/hector
	@echo "Hector installed successfully!"

# Uninstall from system PATH
uninstall:
	@echo "Uninstalling hector from /usr/local/bin..."
	@sudo rm -f /usr/local/bin/hector
	@echo "Hector uninstalled successfully!"

# Run tests
test:
	@echo "Running tests..."
	go test -v ./pkg/... ./cmd/...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run tests with coverage and show summary
test-coverage-summary:
	@echo "Running tests with coverage summary..."
	go test -v -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f hector
	rm -f coverage.out coverage.html
	go clean

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Ensure static assets exist for go vet


# Run go vet
vet:
	@echo "Running go vet..."
	go vet ./pkg/... ./cmd/...

# Build release binaries for multiple platforms
release:
	@echo "Building release binaries (stripped for production)..."
	@echo "Generating embedded assets..."
	@echo "Building release binaries (stripped for production)..."
	@mkdir -p dist

	# Linux
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS_RELEASE)" -o dist/hector-linux-amd64 ./cmd/hector
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS_RELEASE)" -o dist/hector-linux-arm64 ./cmd/hector

	# macOS
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS_RELEASE)" -o dist/hector-darwin-amd64 ./cmd/hector
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS_RELEASE)" -o dist/hector-darwin-arm64 ./cmd/hector

	# Windows
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS_RELEASE)" -o dist/hector-windows-amd64.exe ./cmd/hector

	@echo ""
	@echo "Release binaries built in dist/ (stripped):"
	@ls -lh dist/

# Show version information
version:
	@echo "Version Information:"
	@go run -ldflags "-X 'github.com/verikod/hector.BuildDate=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)' -X 'github.com/verikod/hector.GitCommit=$(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)'" ./cmd/hector version

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	go mod download

# Tidy go.mod
mod-tidy:
	@echo "Tidying go.mod..."
	go mod tidy

# Development workflow
dev: fmt vet lint test build
	@echo "Development build complete"

# CI workflow
ci: deps fmt vet lint test
	@echo "CI checks complete"

# Test package coverage
test-package:
	@echo "Running tests for specific package..."
	@if [ -z "$(PACKAGE)" ]; then \
		echo "Usage: make test-package PACKAGE=pkg/config"; \
		exit 1; \
	fi
	go test -v -coverprofile=coverage.out ./$(PACKAGE)/...
	go tool cover -func=coverage.out

# Test with race detection
test-race:
	@echo "Running tests with race detection..."
	go test -race -v ./...

# Test with verbose output
test-verbose:
	@echo "Running tests with verbose output..."
	go test -v ./...

# Install golangci-lint
install-lint:
	@echo "Installing golangci-lint..."
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(shell go env GOPATH)/bin v1.55.2

# Run golangci-lint
lint:
	@echo "Running golangci-lint..."
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "golangci-lint not found. Installing..."; \
		$(MAKE) install-lint; \
	fi
	export PATH=$$PATH:$(shell go env GOPATH)/bin && golangci-lint run --timeout=5m ./pkg/... ./cmd/...

# Run all quality checks
quality: fmt vet lint test
	@echo "All quality checks passed"

# Pre-commit checks (what CI runs)
pre-commit: deps fmt vet lint test build
	@echo "Pre-commit checks complete - ready to push"

# Validate all example configurations
.PHONY: validate-configs validate-config-examples

validate-configs:
	@echo "🔍 Validating all configuration examples..."
	@FAILED=0; \
	for config in configs/*.yaml; do \
		echo "  Checking $$config..."; \
		if ./hector validate "$$config" --format=compact > /dev/null 2>&1; then \
			echo "  ✅ $$config is valid"; \
		else \
			echo "  ❌ $$config has errors"; \
			./hector validate "$$config" --format=verbose; \
			FAILED=$$((FAILED + 1)); \
		fi; \
	done; \
	if [ $$FAILED -gt 0 ]; then \
		echo ""; \
		echo "❌ $$FAILED configuration(s) failed validation"; \
		exit 1; \
	else \
		echo ""; \
		echo "✅ All configurations are valid!"; \
	fi

# Alias for validate-configs
validate-config-examples: validate-configs



# A2A Protocol Compliance Verification
.PHONY: a2a-tests

a2a-tests:
	@echo "🧪 Running A2A compliance tests..."
	@go test -v ./pkg/a2a -run TestCompliance

