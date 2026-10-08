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

The workflow runs the tests first, builds with the version stamped in
(`second-brain version`), and publishes a `checksums.txt` (SHA-256) with the
binaries.

### Install scripts (recommended)

`install.ps1` (Windows) and `install.sh` (macOS, Linux) at the repository
root download the binary for the platform, verify it against
`checksums.txt`, and install it in `~/.second-brain/bin` — on Windows also
adding it to the user PATH. Options, as environment variables:

| Variable | Effect |
|---|---|
| `SECOND_BRAIN_VERSION` | install this release tag instead of the latest |
| `SECOND_BRAIN_INSTALL_DIR` | install somewhere else |
| `SECOND_BRAIN_NO_MODIFY_PATH=1` | leave the PATH alone (Windows) |
| `SECOND_BRAIN_BASE_URL` | download from a mirror of the release files |
| `SECOND_BRAIN_BINARY` | install a local file (to test a build before releasing it) |

Plugins can't ship the binary: harnesses copy plugins as plain folders,
with no install step, and binaries are per platform. So the binary is
installed once, on the PATH, and every harness starts it as `second-brain`.

### Build from source

```bash
git clone https://github.com/sergiofreitas/leader-second-brain.git
cd leader-second-brain
make build        # current platform
make build-all    # all 5 platforms
make install      # build + copy to /usr/local/bin
```

## 2. Harness plugins

Each harness has its own plugin distribution mechanism:

### Claude Code

The repository is itself a Claude Code marketplace
(`.claude-plugin/marketplace.json`), listing the plugin in
`packages/plugins/claude-code`:

```bash
claude plugin marketplace add sergiofreitas/leader-second-brain
claude plugin install second-brain@second-brain
```

The plugin has its manifest in `.claude-plugin/plugin.json`, the MCP server
in `.mcp.json` (`"command": "second-brain"`), the skills in `skills/` and two
commands in `commands/`. Claude Code copies a plugin into its cache on
install and the plugin can't reference files outside its folder, so
`skills/` is a copy of `packages/skills`, kept in sync with
`make sync-skills` and checked by `make check-skills` in the release
workflow. Bump `version` in `plugin.json` on each release, so installed
plugins update.

Check before publishing:

```bash
claude plugin validate packages/plugins/claude-code --strict
claude plugin validate . --strict          # the marketplace
claude --plugin-dir packages/plugins/claude-code   # try it in a session
```

### OpenAI Codex

**Marketplace:**

```bash
# Add the marketplace
codex plugin marketplace add sergiofreitas/leader-second-brain

# Install the plugin
codex plugin add second-brain
```

Or manual via `~/.codex/config.toml`:

```toml
[mcp_servers.second-brain]
command = "second-brain"
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
      "command": ["second-brain"],
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

The profiles live in `packages/mcp-server/configs/profiles/` and are
embedded in the binary; `second-brain init` writes one to
`~/.second-brain/config.yaml`:

```bash
second-brain init                                     # default: keyword search only, nothing sent anywhere
second-brain init --profile saipos                    # stop/start/continue, Qulture, LiteLLM gateway
second-brain init --profile saipos --embedding openai # same, with OpenAI embeddings
second-brain init --profile startup                   # freeform, OpenAI embeddings
second-brain init --profile personal                  # freeform, local Ollama embeddings
```

`--embedding none|openai|ollama` replaces the profile's semantic search
provider and keeps the rest; `--force` overwrites an existing config.
Without any config the server runs with the `default` settings.

## 4. First-run experience

```bash
# 1. Install the binary (Windows: irm .../install.ps1 | iex)
curl -fsSL https://raw.githubusercontent.com/sergiofreitas/leader-second-brain/main/install.sh | sh

# 2. Optionally, write a config (it lists the env vars it needs)
second-brain init --profile saipos

# 3. Install the harness plugin
claude plugin marketplace add sergiofreitas/leader-second-brain
claude plugin install second-brain@second-brain

# 4. Restart the harness and start using it:
#    "Anotei que o Evandro resolveu sozinho um bug de TEF"
```

## 5. Versioning

- Binary and plugins share the same version (git tags: `v0.1.0`, `v0.2.0`, etc.)
- GitHub Releases include the cross-compiled binaries and `checksums.txt` for each tag
- Plugin marketplaces point to the same repository; bump the plugin's
  `version` with each release so installed plugins update
- npm package (OpenCode) is published separately with the same version
