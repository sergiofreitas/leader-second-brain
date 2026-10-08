.PHONY: build test clean install-skills install-plugins sync-skills check-skills

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
	@echo "Optionally create a config: second-brain init --help"

# Plugins are copied into the harness on install, so each one carries its own
# copy of the skills. packages/skills is the source of truth.
PLUGIN_SKILLS_DIRS=$(PLUGIN_DIR)/claude-code/skills

sync-skills:
	@for d in $(PLUGIN_SKILLS_DIRS); do rm -rf $$d && mkdir -p $$d && cp -r $(SKILLS_DIR)/* $$d/ && echo "Synced skills into $$d"; done

check-skills:
	@for d in $(PLUGIN_SKILLS_DIRS); do diff -r $(SKILLS_DIR) $$d || { echo "$$d is out of date: run make sync-skills"; exit 1; }; done

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
