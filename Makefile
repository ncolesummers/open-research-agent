# Open Research Agent - Makefile

# Variables
BINARY_NAME=ora
BUILD_DIR=./bin
GO_FILES=$(shell find . -name '*.go' -type f)
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME}"

# Emojis
TADA=🎉
BUILD=🏗️
CLEAN=🧹
TEST=🧪
PLUG=🔌
CHART=📊
UP_CHART=📈
TEST_MAG=🧪🔍
MAG=🔎
CHECK=✅
EYES=👀
DEPS=📦
TOOLS=🛠️
FMT=🎨
VET=🩺
LINT=🚨
RUN=🚀
DEV=🔥
CONFIG=⚙️
DOCKER=🐳
OLLAMA=🦙
BENCH=⏱️
SKIP=⏭️
HELP=❓

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=gofmt
GOLINT=golangci-lint

# Targets
.PHONY: all build clean test coverage deps fmt lint run docker-build docker-run help

# Default target
all: deps fmt lint test build

# Build the binary
build: ## Build the binary
	@echo "$(BUILD) Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	@$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) -v ./cmd/ora

# Build for multiple platforms
build-all: ## Build for multiple platforms
	@echo "$(BUILD) Building for multiple platforms..."
	@mkdir -p $(BUILD_DIR)
	@GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/ora
	@GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/ora
	@GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/ora
	@GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/ora
	@GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/ora
	@echo "Build complete for all platforms"

# Clean build artifacts
clean: ## Clean build artifacts
	@echo "$(CLEAN) Cleaning..."
	@$(GOCLEAN)
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out coverage.html

# Run tests
test: ## Run tests
	@echo "$(TEST) Running tests..."
	@./scripts/test-with-emojis.sh -v -race -coverprofile=coverage.out ./...

# Run unit tests only
test-unit: ## Run unit tests only
	@echo "$(TEST) Running unit tests..."
	@./scripts/test-with-emojis.sh -v -short -race ./...

# Run integration tests only
test-integration: ## Run integration tests only
	@echo "$(PLUG) Running integration tests..."
	@./scripts/test-with-emojis.sh -v -run Integration -race ./...

# Run tests with coverage report
coverage: test ## Run tests with coverage report
	@echo "$(CHART) Generating coverage report..."
	@go tool cover -html=coverage.out -o coverage.html
	@go tool cover -func=coverage.out
	@echo "$(UP_CHART) Coverage report generated: coverage.html"

# Run tests with coverage and fail if below threshold
test-coverage-check: ## Run tests and check coverage threshold
	@echo "$(TEST_MAG) Running tests with coverage check..."
	@./scripts/test-with-emojis.sh -v -race -coverprofile=coverage.out ./...
	@echo "$(MAG) Checking coverage threshold (70%)..."
	@go tool cover -func=coverage.out | grep "total:" | awk '{print $3}' | sed 's/%//' | awk '{if ($1 < 70) exit 1}'
	@echo "$(CHECK) Coverage check passed!"

# Run tests in watch mode (requires entr or similar)
test-watch: ## Run tests in watch mode
	@echo "$(EYES) Running tests in watch mode..."
	@find . -name '*.go' | entr -c ./scripts/test-with-emojis.sh -v ./...

# Install dependencies
deps: ## Install dependencies
	@echo "$(DEPS) Installing dependencies..."
	@$(GOMOD) download
	@$(GOMOD) tidy

# Install development tools
install-tools: ## Install development tools
	@echo "$(TOOLS) Installing development tools..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@go install golang.org/x/tools/cmd/goimports@latest
	@go install github.com/air-verse/air@latest
	@echo "$(CHECK) Tools installed successfully"

# Format code
fmt: ## Format code
	@echo "$(FMT) Formatting code..."
	@$(GOFMT) -s -w .
	@goimports -w .

# Run go vet
vet: ## Run go vet
	@echo "$(VET) Running go vet..."
	@go vet ./...

# Run linter
lint: ## Run linter
	@echo "$(LINT) Running linter..."
	@$(GOLINT) run ./...

# Run the application
run: build ## Build and run the application
	@echo "$(RUN) Running $(BINARY_NAME)..."
	@$(BUILD_DIR)/$(BINARY_NAME)

# Run with hot reload (requires air)
dev: ## Run with hot reload (requires air)
	@echo "$(DEV) Running with hot reload..."
	@air

# Run with specific config
run-config: build ## Run with custom config
	@echo "$(CONFIG) Running with custom config..."
	@$(BUILD_DIR)/$(BINARY_NAME) -config configs/default.yaml

# Docker build
docker-build: ## Build Docker image
	@echo "$(DOCKER) Building Docker image..."
	@docker build -t open-research-agent:$(VERSION) .
	@docker tag open-research-agent:$(VERSION) open-research-agent:latest

# Docker run
docker-run: ## Run Docker container
	@echo "$(DOCKER) Running Docker container..."
	@docker run -it --rm \
		-v $(PWD)/configs:/app/configs \
		-v $(PWD)/data:/app/data \
		open-research-agent:latest

# Start Ollama locally
ollama-start: ## Start Ollama server
	@echo "$(OLLAMA) Starting Ollama..."
	@ollama serve &

# Pull Ollama model
ollama-pull: ## Pull Ollama model
	@echo "$(OLLAMA) Pulling Ollama model..."
	@ollama pull llama3.2

# Run benchmarks
bench: ## Run benchmarks
	@echo "$(BENCH) Running benchmarks..."
	@$(GOTEST) -bench=. -benchmem ./...

# Initialize project (first time setup)
init: deps install-tools ollama-pull ## Initialize project (first time setup)
	@echo "$(TADA) Project initialized successfully!"
	@echo "Run 'make run' to start the application"

# Help target
help: ## Show this help message
	@echo 'Open Research Agent - Available targets:'
	@echo ''
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $1, $2}' $(MAKEFILE_LIST)
	@echo ''
	@echo 'Environment variables:'
	@echo '  OLLAMA_BASE_URL     - Ollama server URL (default: http://localhost:11434)'
	@echo '  OLLAMA_MODEL        - Ollama model to use (default: llama3.2)'
	@echo '  API_PORT            - API server port (default: 8080)'
	@echo '  ENVIRONMENT         - Environment (development/production)'
