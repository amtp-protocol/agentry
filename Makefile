# Agentry Makefile

# Variables
BINARY_NAME=agentry
ADMIN_BINARY_NAME=agentry-admin
DOCKER_IMAGE=agentry
DOCKER_TAG=latest
GO_VERSION=1.21

# Build variables
BUILD_DIR=build
MAIN_PATH=./main.go
ADMIN_MAIN_PATH=./cmd/agentry-admin/main.go
LDFLAGS=-ldflags "-X main.version=$(shell git describe --tags --always --dirty 2>/dev/null || echo 'dev') -X main.buildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)"

# Test variables
COVERAGE_DIR=coverage
COVERAGE_PROFILE=$(COVERAGE_DIR)/coverage.out
COVERAGE_HTML=$(COVERAGE_DIR)/coverage.html

.PHONY: help build clean test test-coverage lint fmt vet deps docker docker-build docker-run dev run install

# Default target
help: ## Show this help message
	@echo "Agentry Build System"
	@echo ""
	@echo "Available targets:"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# Build targets
build: ## Build the binary
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	@go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "Binary built: $(BUILD_DIR)/$(BINARY_NAME)"

build-admin: ## Build the admin tool
	@echo "Building $(ADMIN_BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	@go build $(LDFLAGS) -o $(BUILD_DIR)/$(ADMIN_BINARY_NAME) $(ADMIN_MAIN_PATH)
	@echo "Admin binary built: $(BUILD_DIR)/$(ADMIN_BINARY_NAME)"

build-all-tools: build build-admin ## Build all tools

build-linux: ## Build for Linux
	@echo "Building $(BINARY_NAME) for Linux..."
	@mkdir -p $(BUILD_DIR)
	@GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 $(MAIN_PATH)
	@echo "Linux binary built: $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64"

build-darwin: ## Build for macOS
	@echo "Building $(BINARY_NAME) for macOS..."
	@mkdir -p $(BUILD_DIR)
	@GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 $(MAIN_PATH)
	@GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 $(MAIN_PATH)
	@echo "macOS binaries built"

build-windows: ## Build for Windows
	@echo "Building $(BINARY_NAME) for Windows..."
	@mkdir -p $(BUILD_DIR)
	@GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe $(MAIN_PATH)
	@echo "Windows binary built: $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe"

build-all: build-linux build-darwin build-windows ## Build for all platforms

# Development targets
dev: ## Run in development mode with hot reload
	@echo "Starting development server..."
	@go run $(MAIN_PATH)

run: build ## Build and run the binary
	@echo "Running $(BINARY_NAME)..."
	@./$(BUILD_DIR)/$(BINARY_NAME)

install: ## Install the binary to GOPATH/bin
	@echo "Installing $(BINARY_NAME)..."
	@go install $(LDFLAGS) $(MAIN_PATH)

# Testing targets
test: ## Run tests
	@echo "Running tests..."
	@go test -v ./...

test-short: ## Run short tests
	@echo "Running short tests..."
	@go test -short -v ./...

test-race: ## Run tests with race detection
	@echo "Running tests with race detection..."
	@go test -race -v ./...

test-coverage: ## Run tests with coverage
	@echo "Running tests with coverage..."
	@mkdir -p $(COVERAGE_DIR)
	@go test -coverprofile=$(COVERAGE_PROFILE) -covermode=atomic ./...
	@go tool cover -html=$(COVERAGE_PROFILE) -o $(COVERAGE_HTML)
	@echo "Coverage report generated: $(COVERAGE_HTML)"

benchmark: ## Run benchmarks
	@echo "Running benchmarks..."
	@go test -bench=. -benchmem ./...

# Code quality targets
lint: ## Run linter
	@echo "Running linter..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed. Installing..."; \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; \
		golangci-lint run; \
	fi

fmt: ## Format code
	@echo "Formatting code..."
	@go fmt ./...

vet: ## Run go vet
	@echo "Running go vet..."
	@go vet ./...

# Dependency management
deps: ## Download dependencies
	@echo "Downloading dependencies..."
	@go mod download

deps-update: ## Update dependencies
	@echo "Updating dependencies..."
	@go get -u ./...
	@go mod tidy

deps-verify: ## Verify dependencies
	@echo "Verifying dependencies..."
	@go mod verify

deps-clean: ## Clean module cache
	@echo "Cleaning module cache..."
	@go clean -modcache

# Docker targets
docker: docker-build ## Build Docker image

docker-build: ## Build Docker image
	@echo "Building Docker image $(DOCKER_IMAGE):$(DOCKER_TAG)..."
	@docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

docker-run: ## Run Docker container
	@echo "Running Docker container..."
	@docker run -p 8443:8443 --rm $(DOCKER_IMAGE):$(DOCKER_TAG)

docker-push: ## Push Docker image
	@echo "Pushing Docker image..."
	@docker push $(DOCKER_IMAGE):$(DOCKER_TAG)

docker-clean: ## Clean Docker images
	@echo "Cleaning Docker images..."
	@docker rmi $(DOCKER_IMAGE):$(DOCKER_TAG) || true

# Utility targets
clean: ## Clean build artifacts
	@echo "Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR)
	@rm -rf $(COVERAGE_DIR)
	@go clean

clean-all: clean docker-clean ## Clean everything

generate: ## Run go generate
	@echo "Running go generate..."
	@go generate ./...

# Security targets
security-scan: ## Run security scan
	@echo "Running security scan..."
	@if command -v gosec >/dev/null 2>&1; then \
		gosec ./...; \
	else \
		echo "gosec not installed. Installing..."; \
		go install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest; \
		gosec ./...; \
	fi

# Documentation targets
docs: ## Generate documentation
	@echo "Generating documentation..."
	@if command -v godoc >/dev/null 2>&1; then \
		echo "Documentation server will be available at http://localhost:6060"; \
		godoc -http=:6060; \
	else \
		echo "godoc not installed. Installing..."; \
		go install golang.org/x/tools/cmd/godoc@latest; \
		echo "Documentation server will be available at http://localhost:6060"; \
		godoc -http=:6060; \
	fi

# CI/CD targets
ci: deps lint vet test-race test-coverage ## Run CI pipeline

pre-commit: fmt vet lint test ## Run pre-commit checks

release: clean build-all test-coverage ## Prepare release

# Environment setup
setup: ## Setup development environment
	@echo "Setting up development environment..."
	@go mod download
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@go install github.com/securego/gosec/v2/cmd/gosec@latest
	@go install golang.org/x/tools/cmd/godoc@latest
	@echo "Development environment setup complete!"

# Version info
version: ## Show version information
	@echo "Go version: $(shell go version)"
	@echo "Git commit: $(shell git rev-parse HEAD)"
	@echo "Git tag: $(shell git describe --tags --always)"
	@echo "Build time: $(shell date -u +%Y-%m-%dT%H:%M:%SZ)"