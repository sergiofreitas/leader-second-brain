# Distribution

Second Brain has two distribution layers: the **MCP server binary** (Go) and the **harness plugins** (config + skills).

## 1. MCP server binary

### GitHub Releases (primary)

Every `git tag v*` triggers the GitHub Actions workflow that cross-compiles the binary for 5 platforms:

| Platform | Binary |
|----------|--------|
| Linux AMD64 | `second-brain-linux-amd64` |
| Linux ARM64 | `second-brain-linux-arm64` |
| macOS AMD64 | `second-brain-darwin-amd64` |
| macOS ARM64 | `second-brain-darwin-arm64` |
| Windows AMD64 | `second-brain-windows-amd64.exe` |

Users download from the GitHub Releases page and place in their PATH.

### Build from source

```bash
git clone https://github.com/second-brain/second-brain.git
cd second-brain
make build        # current platform
make build-all    # all 5 platforms
make install      # build + copy to /usr/local/bin
```

### Homebrew (future)

```bash
brew install second-brain/tap/second-brain
```

## 2. Harness plugins

Each harness has its own plugin distribution mechanism:

### Claude Code

**Marketplace (recommended):**

```bash
# Add the marketplace
claude plugin marketplace add second-brain/second-brain

# Install the plugin
claude plugin install second-brain
```

Or manual:
```bash
claude plugin install /path/to/second-brain/packages/plugins/claude-code
```

The `marketplace.json` at `packages/plugins/claude-code/marketplace.json` lists the plugin. The `plugin.json` and `.mcp.json` configure the MCP server launch.

### OpenAI Codex

**Marketplace:**

```bash
# Add the marketplace
codex plugin marketplace add second-brain/second-brain

# Install the plugin
codex plugin add second-brain
```

Or manual via `~/.codex/config.toml`:

```toml
[mcp_servers.second-brain]
command = "second-brain"
args = ["--config", "~/.second-brain/config.yaml"]
```

The `marketplace.json` at `packages/plugins/codex/marketplace.json` lists the plugin.

### OpenCode

**npm package (recommended):**

```json
{
  "$schema": "https://opencode.ai/config.json",
  "plugin": ["second-brain-opencode"],
  "mcp": {
    "second-brain": {
      "type": "local",
      "command": ["second-brain", "--config", "~/.second-brain/config.yaml"],
      "enabled": true
    }
  }
}
```

The npm package `second-brain-opencode` runs `setup.js` on install, which copies skills to `.opencode/skills/`.

Or manual:
```bash
cp -r packages/skills/* .opencode/skills/
```

Add MCP config to `opencode.json` as shown above.

## 3. Configuration profiles

Users copy a profile from `examples/` to `~/.second-brain/config.yaml`:

```bash
# Saipos (Toqan + Qulture)
cp examples/saipos/config.yaml ~/.second-brain/config.yaml

# Startup (hybrid + freeform)
cp examples/startup/config.yaml ~/.second-brain/config.yaml

# Personal (all-local, minimal)
cp examples/personal/config.yaml ~/.second-brain/config.yaml
```

## 4. First-run experience

```bash
# 1. Install the binary
make install   # or download from GitHub Releases

# 2. Copy a config
mkdir -p ~/.second-brain
cp examples/personal/config.yaml ~/.second-brain/config.yaml

# 3. Install the harness plugin (pick one)
claude plugin marketplace add second-brain/second-brain && claude plugin install second-brain
# or
codex plugin marketplace add second-brain/second-brain && codex plugin add second-brain
# or
# Add to opencode.json (see above)

# 4. Start using
# Open your harness and say: "Anotei que o Evandro resolveu sozinho um bug de TEF"
```

## 5. Versioning

- Binary and plugins share the same version (git tags: `v0.1.0`, `v0.2.0`, etc.)
- GitHub Releases include the cross-compiled binaries for each tag
- Plugin marketplaces point to the same repository, so `plugin install` always gets the latest
- npm package (OpenCode) is published separately with the same version
