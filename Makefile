GO = go
BUILD_DIR = bin
BUILD_BINARY = $(BUILD_DIR)/agent
PKG = ./...

all: deps lint test build

deps:
	@echo "[INFO] Installing dependencies..."
	$(GO) mod tidy
	@echo "[SUCCESS] Dependencies installed."

vendor:
	@echo "[INFO] Creating/updating vendor directory...]"
	$(GO) mod vendor
	@echo "[SUCCESS] Vendor directory updated.]"

lint:
	@echo "[INFO] Running linter..."
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	golangci-lint run
	@echo "[SUCCESS] Linting completed."

test:
	@echo "[INFO] Running tests..."
	$(GO) test $(PKG) -v
	@echo "[SUCCESS] Tests completed."

build:
	@echo "[INFO] Building the project..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build -o $(BUILD_BINARY) ./cmd/agent
	@echo "[SUCCESS] Build completed. Binary available at $(BUILD_BINARY)"

clean:
	@echo "[INFO] Cleaning up..."
	@rm -rf $(BUILD_DIR)
	@echo "[SUCCESS] Cleanup completed."

.PHONY: all test build clean deps lint
