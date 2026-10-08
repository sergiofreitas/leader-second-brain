# Second Brain — OpenCode Plugin

A leadership memory plugin for OpenCode.

## Installation

### Option A: Add to project config

Add to `opencode.json` or `opencode.jsonc` in your project root:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "second-brain": {
      "type": "local",
      "command": ["second-brain", "--config", "~/.second-brain/config.yaml"],
      "enabled": true
    }
  }
}
```

### Option B: Global config

Add to `~/.config/opencode/opencode.json`:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "second-brain": {
      "type": "local",
      "command": ["second-brain", "--config", "~/.second-brain/config.yaml"],
      "enabled": true
    }
  }
}
```

### Option C: .opencode directory

Place the plugin in `.opencode/plugins/second-brain/` — OpenCode auto-discovers
plugins in that directory.

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

3. Add skills to `.opencode/skills/`:
   ```bash
   cp -r packages/skills/* .opencode/skills/
   ```

## Tools

| Tool | Purpose |
|------|---------|
| `ingest` | Capture a memory (text, audio, image, video) |
| `recall` | Get context about a person |
| `get_team_context` | Overview of a leader's team |
| `search_memories` | Search by keyword or semantic meaning |

## Skills

OpenCode discovers skills in `.opencode/skills/`. Copy the skill folders:

```bash
cp -r packages/skills/second-brain .opencode/skills/
cp -r packages/skills/one-on-one .opencode/skills/
cp -r packages/skills/pdi-gap-map .opencode/skills/
cp -r packages/skills/feedback .opencode/skills/
```

## Environment variables

| Variable | Purpose | Default |
|----------|---------|---------|
| `SECOND_BRAIN_CONFIG` | Path to config.yaml | `~/.second-brain/config.yaml` |

## Privacy

All data stays local in a single SQLite file. Only AI processing goes to
the configured provider.
