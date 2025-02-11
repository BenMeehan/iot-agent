GO = go
BUILD_DIR = bin
BUILD_BINARY = $(BUILD_DIR)/agent
PKG = ./...


all: test build

test:
	@echo "Running tests..."
	$(GO) test $(PKG) -v

build:
	@echo "Building the project..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build -o $(BUILD_BINARY) ./cmd/agent

deps:
   @echo "Installing dependencies..."
   $(GO) mod tidy

lint:
 	@echo "Running linter..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	golangci-lint run

clean:
	@echo "Cleaning up..."
	@rm -rf $(BUILD_DIR)

.PHONY: all test build clean
