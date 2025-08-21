# GO = go
# BUILD_DIR = bin
# BUILD_BINARY = $(BUILD_DIR)/agent
# PKG = ./...

# For linux arm 7
GO=go
GOARCH=arm
GOARM=7
GOOS=linux
VERSION=1.3.0
LDFLAGS=-ldflags "-X main.Version=$(VERSION)"
BUILD_DIR = bin
BUILD_BINARY = $(BUILD_DIR)/agent

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

test-coverage:
	@echo "[INFO] Running tests with coverage across all packages..."
	$(GO) test ./... -coverprofile=coverage.out -covermode=atomic -v
	@echo "[INFO] Coverage summary:"
	@$(GO) tool cover -func=coverage.out | awk '/total:/ {print}'
	@echo "[SUCCESS] Coverage report generated at coverage.out"

coverage-html:
	@echo "[INFO] Rendering coverage HTML..."
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "[SUCCESS] Open coverage.html in a browser to view detailed coverage."

# build:
# 	@echo "[INFO] Building the project..."
# 	@mkdir -p $(BUILD_DIR)
# 	$(GO) build -o $(BUILD_BINARY) ./cmd/agent
# 	@echo "[SUCCESS] Build completed. Binary available at $(BUILD_BINARY)"

build:
	@echo "[INFO] Building the project..."
	@mkdir -p $(BUILD_DIR)
	@GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM=$(GOARM) $(GO) build $(LDFLAGS) -o $(BUILD_BINARY) ./cmd/agent
	@echo "[SUCCESS] Build completed. Binary available at $(BUILD_BINARY)"

clean:
	@echo "[INFO] Cleaning up..."
	@rm -rf $(BUILD_DIR)
	@echo "[SUCCESS] Cleanup completed."

.PHONY: all test build clean deps lint
