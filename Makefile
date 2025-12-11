.PHONY: all build clean test run-server run-dashboard run-builder

# Variables
BINARY_SERVER=bin/portage-server
BINARY_DASHBOARD=bin/portage-dashboard
BINARY_BUILDER=bin/portage-builder
GO=go
GOFLAGS=-v

all: build

# Build all binaries
build: build-server build-dashboard build-builder

# Build server
build-server:
	@echo "Building Portage Engine Server..."
	@mkdir -p bin
	$(GO) build $(GOFLAGS) -o $(BINARY_SERVER) cmd/server/main.go

# Build dashboard
build-dashboard:
	@echo "Building Portage Engine Dashboard..."
	@mkdir -p bin
	$(GO) build $(GOFLAGS) -o $(BINARY_DASHBOARD) cmd/dashboard/main.go

# Build builder
build-builder:
	@echo "Building Portage Engine Builder..."
	@mkdir -p bin
	$(GO) build $(GOFLAGS) -o $(BINARY_BUILDER) cmd/builder/main.go

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf bin/
	@$(GO) clean

# Run tests
test:
	@echo "Running tests..."
	$(GO) test -v ./...

# Run server
run-server:
	@echo "Starting Portage Engine Server..."
	$(GO) run cmd/server/main.go -config configs/server.yaml

# Run dashboard
run-dashboard:
	@echo "Starting Portage Engine Dashboard..."
	$(GO) run cmd/dashboard/main.go -config configs/dashboard.yaml

# Run builder
run-builder:
	@echo "Starting Portage Engine Builder..."
	$(GO) run cmd/builder/main.go -port 9090

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GO) mod download
	$(GO) mod tidy

# Format code
fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...

# Lint code
lint:
	@echo "Linting code..."
	golangci-lint run ./...

# Install binaries
install: build
	@echo "Installing binaries..."
	@mkdir -p /usr/local/bin
	@cp $(BINARY_SERVER) /usr/local/bin/
	@cp $(BINARY_DASHBOARD) /usr/local/bin/
	@cp $(BINARY_BUILDER) /usr/local/bin/
	@cp scripts/portage-client.sh /usr/local/bin/portage-client
	@chmod +x /usr/local/bin/portage-client
	@echo "Installation complete"

# Build for multiple architectures
build-all: build-linux-amd64 build-linux-arm64 build-darwin-amd64 build-darwin-arm64

build-linux-amd64:
	GOOS=linux GOARCH=amd64 $(GO) build -o bin/portage-server-linux-amd64 cmd/server/main.go
	GOOS=linux GOARCH=amd64 $(GO) build -o bin/portage-dashboard-linux-amd64 cmd/dashboard/main.go
	GOOS=linux GOARCH=amd64 $(GO) build -o bin/portage-builder-linux-amd64 cmd/builder/main.go

build-linux-arm64:
	GOOS=linux GOARCH=arm64 $(GO) build -o bin/portage-server-linux-arm64 cmd/server/main.go
	GOOS=linux GOARCH=arm64 $(GO) build -o bin/portage-dashboard-linux-arm64 cmd/dashboard/main.go
	GOOS=linux GOARCH=arm64 $(GO) build -o bin/portage-builder-linux-arm64 cmd/builder/main.go

build-darwin-amd64:
	GOOS=darwin GOARCH=amd64 $(GO) build -o bin/portage-server-darwin-amd64 cmd/server/main.go
	GOOS=darwin GOARCH=amd64 $(GO) build -o bin/portage-dashboard-darwin-amd64 cmd/dashboard/main.go

build-darwin-arm64:
	GOOS=darwin GOARCH=arm64 $(GO) build -o bin/portage-server-darwin-arm64 cmd/server/main.go
	GOOS=darwin GOARCH=arm64 $(GO) build -o bin/portage-dashboard-darwin-arm64 cmd/dashboard/main.go
