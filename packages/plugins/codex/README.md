# Second Brain — Codex Plugin

A leadership memory plugin for OpenAI Codex.

## Installation

### Option A: Codex CLI

```bash
codex plugin install github.com/sergiofreitas/leader-second-brain/packages/plugins/codex
```

### Option B: Manual config

Add to `~/.codex/config.toml`:

```toml
[mcp_servers.second-brain]
command = "second-brain"
args = ["--config", "~/.second-brain/config.yaml"]
```

### Option C: Project-scoped

Add to `.codex/config.toml` in your project (trusted projects only):

```toml
[mcp_servers.second-brain]
command = "/path/to/second-brain"
args = ["--config", ".second-brain/config.yaml"]
```

## Setup

1. Build the binary:
   ```bash
   cd packages/mcp-server
   CGO_ENABLED=0 go build -o /usr/local/bin/second-brain ./cmd/second-brain
   ```

2. Copy a config:
   ```bash
   mkdir -p ~/.second-brain
   second-brain init --profile saipos
   ```

3. Add to your `AGENTS.md`:
   ```
   Use the Second Brain MCP server when I want to record observations about
   team members or get context before leadership activities.
   ```

## Tools

| Tool | Purpose |
|------|---------|
| `ingest` | Capture a memory (text, audio, image, video) |
| `recall` | Get context about a person |
| `get_team_context` | Overview of a leader's team |
| `search_memories` | Search by keyword or semantic meaning |

## Environment variables

| Variable | Purpose | Default |
|----------|---------|---------|
| `SECOND_BRAIN_CONFIG` | Path to config.yaml | `~/.second-brain/config.yaml` |

## Privacy

All data stays local. Only AI processing goes to the configured provider.
