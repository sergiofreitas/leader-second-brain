.PHONY: build test clean install-skills install-plugins

BINARY_NAME=second-brain
MCP_DIR=packages/mcp-server
PLUGIN_DIR=packages/plugins
SKILLS_DIR=packages/skills

# Build the MCP server (CGO-free, cross-compilable)
build:
	cd $(MCP_DIR) && CGO_ENABLED=0 go build -o ../../$(BINARY_NAME) ./cmd/second-brain

# Cross-compile for all platforms
build-all:
	cd $(MCP_DIR) && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../../dist/$(BINARY_NAME)-linux-amd64 ./cmd/second-brain
	cd $(MCP_DIR) && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o ../../dist/$(BINARY_NAME)-linux-arm64 ./cmd/second-brain
	cd $(MCP_DIR) && CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o ../../dist/$(BINARY_NAME)-darwin-amd64 ./cmd/second-brain
	cd $(MCP_DIR) && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o ../../dist/$(BINARY_NAME)-darwin-arm64 ./cmd/second-brain
	cd $(MCP_DIR) && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o ../../dist/$(BINARY_NAME)-windows-amd64.exe ./cmd/second-brain

# Run E2E tests
test:
	cd $(MCP_DIR) && go test ./tests/ -v

# Install binary to system path
install: build
	cp $(BINARY_NAME) /usr/local/bin/$(BINARY_NAME)
	@echo "Installed to /usr/local/bin/$(BINARY_NAME)"
	@echo "Copy a config: cp examples/saipos/config.yaml ~/.second-brain/config.yaml"

# Copy skills to a target harness skills directory
install-skills:
	@echo "Usage: make install-skills TARGET=<path>"
	@if [ -z "$(TARGET)" ]; then echo "TARGET is required (e.g., .opencode/skills)"; exit 1; fi
	cp -r $(SKILLS_DIR)/* $(TARGET)/
	@echo "Skills installed to $(TARGET)"

clean:
	rm -f $(BINARY_NAME)
	rm -rf dist/
	cd $(MCP_DIR) && go clean
