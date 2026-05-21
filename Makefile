.PHONY: all build test e2e-test run-demo fmt vet clean

# Default target
all: fmt vet build test e2e-test

# Build CLI binary
build:
	@echo "==> Building Petals P2P Go binary..."
	@mkdir -p bin
	go build -o bin/connect main.go
	@echo "==> Binary successfully compiled to bin/connect"

# Run all unit and package-level integration tests
test:
	@echo "==> Running standard unit and integration tests..."
	go test -v ./...

# Run the true process-level E2E integration test script
e2e-test: build
	@echo "==> Running black-box E2E CLI integration test script..."
	@bash ./scripts/run_e2e_tests.sh

# Run the single-process interactive demo
run-demo:
	@echo "==> Launching automated single-process demo..."
	go run main.go

# Format the Go source code
fmt:
	@echo "==> Formatting Go source code..."
	go fmt ./...

# Run Go static analyzer (vet)
vet:
	@echo "==> Running static analyzer (go vet)..."
	go vet ./...

# Clean build artifacts
clean:
	@echo "==> Cleaning build artifacts..."
	rm -rf bin
	@echo "==> Clean completed!"
