# Second Brain — Memory for Leadership

A local-first, open-source MCP server that gives leaders a **second brain** for managing people. Capture observations as text, voice, image, or video; the system builds a structured knowledge graph and retrieves context when you need it — before a 1:1, a PDI assessment, or a feedback session.

## Monorepo structure

```
second-brain/
├── packages/
│   ├── mcp-server/          Go MCP server (single binary, CGO-free)
│   │   ├── cmd/              Entry point + MCP tool definitions
│   │   ├── internal/         Core logic (config, providers, store, retrieve)
│   │   ├── configs/          Default config
│   │   └── tests/            End-to-end tests
│   │
│   ├── skills/              Portable skill definitions (SKILL.md)
│   │   ├── second-brain/     Base: how to use ingest/recall
│   │   ├── one-on-one/       1:1 synthesis with historical context
│   │   ├── pdi-gap-map/      PDI assessment with evidence from graph
│   │   └── feedback/         Feedback structuring with configured format
│   │
│   └── plugins/             Harness-specific plugins
│       ├── claude-code/      Claude Code plugin (plugin.json + .mcp.json + commands)
│       ├── codex/            OpenAI Codex plugin (plugin.json + mcp.json + AGENTS.md)
│       └── opencode/         OpenCode plugin (opencode.json + .opencode/)
│
├── examples/                Configuration profiles per organization
│   ├── saipos/              Toqan provider, Qulture integration
│   ├── startup/             Hybrid (local + OpenAI)
│   └── personal/            Minimal, all-local, zero API keys
│
├── docs/                    Documentation
├── Makefile                 Build, test, install, cross-compile
└── README.md
```

## Quick start

```bash
# Build the MCP server (CGO-free)
make build

# Run tests
make test

# Install
make install

# Copy a config profile
mkdir -p ~/.second-brain
cp examples/saipos/config.yaml ~/.second-brain/config.yaml
```

## Harness plugins

### Claude Code

```bash
claude plugin install github.com/second-brain/second-brain/packages/plugins/claude-code
```

Or manual: see `packages/plugins/claude-code/README.md`

### OpenAI Codex

```bash
codex plugin install github.com/second-brain/second-brain/packages/plugins/codex
```

Or add to `~/.codex/config.toml`:
```toml
[mcp_servers.second-brain]
command = "second-brain"
args = ["--config", "~/.second-brain/config.yaml"]
```

See `packages/plugins/codex/README.md`

### OpenCode

Add to `opencode.json`:
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

See `packages/plugins/opencode/README.md`

## Configuration profiles

| Profile | Providers | API Keys | Feedback format | Graph |
|---------|-----------|----------|-----------------|-------|
| `saipos` | Toqan + local ST | 0 (proxy) | stop/start/continue → Qulture | SQLite |
| `startup` | Hybrid (local + OpenAI) | 1-2 | freeform → markdown | SQLite |
| `personal` | All local (Whisper, Ollama, ST) | 0 | freeform → markdown | SQLite |

## MCP tools

| Tool | Purpose |
|------|---------|
| `ingest` | Capture a memory (text, audio, image, video) — detects type, extracts entities, stores in graph + vector |
| `recall` | Retrieve context about a person for 1:1, PDI, feedback, or team review |
| `get_team_context` | Overview of all reports under a leader (recursive hierarchy) |
| `search_memories` | Search by keyword (FTS5) or semantic similarity (vector) |

## Architecture

- **MCP server**: Go, single binary, `CGO_ENABLED=0` — cross-compiles to Windows, Linux, macOS
- **Graph engine**: SQLite tables + recursive CTEs (same database file, CGO-free)
- **Vector search**: exact cosine similarity in pure Go (embeddings stored in the same SQLite file, no extension needed)
- **Keyword search**: FTS5 (SQLite native full-text search)
- **Storage**: One file — `memoria.db` — contains graph, memories, vectors, and FTS index
- **Provider abstraction**: Pluggable transcription, OCR, VLM, embedding, LLM — local or cloud via config

## License

MIT
