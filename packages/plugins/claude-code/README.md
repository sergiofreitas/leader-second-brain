# Second Brain — Claude Code Plugin

A leadership memory plugin for Claude Code. Capture observations about your team and recall context before 1:1s, PDIs, and feedback sessions.

## Installation

```bash
claude plugin install github.com/second-brain/second-brain/packages/plugins/claude-code
```

Or clone and install locally:

```bash
claude plugin install /path/to/second-brain/packages/plugins/claude-code
```

## First-time setup

1. Build the MCP server:
   ```bash
   cd packages/mcp-server
   CGO_ENABLED=0 go build -o ../plugins/claude-code/bin/second-brain ./cmd/second-brain
   ```

2. Copy a config profile:
   ```bash
   mkdir -p ~/.second-brain
   cp examples/saipos/config.yaml ~/.second-brain/config.yaml
   # Edit as needed
   ```

## Tools

| Tool | Purpose |
|------|---------|
| `ingest` | Capture a memory (text, audio, image, video) |
| `recall` | Get context about a person |
| `get_team_context` | Overview of a leader's team |
| `search_memories` | Search by keyword or semantic meaning |

## Commands

| Command | Purpose |
|---------|---------|
| `/briefing [name]` | Get a briefing before a 1:1 |
| `/observe` | Record a quick observation |

## Skills included

- `second-brain` — Base skill: capture & recall guidance
- `one-on-one` — 1:1 synthesis with historical context
- `pdi-gap-map` — PDI assessment with evidence from graph
- `feedback` — Feedback structuring with configured format

## Configuration

See `examples/` in the repository root for config profiles:
- `saipos/` — Host extraction, Qulture integration
- `startup/` — Hybrid (local + OpenAI)
- `personal/` — Minimal, all-local, zero API keys

## Privacy

All data stays on your machine in a single SQLite file. Only AI processing
(transcription, OCR, VLM) goes to the configured provider — structured data
never leaves your device.
