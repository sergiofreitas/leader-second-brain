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
│   ├── saipos/              Host extraction, Qulture integration
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

| Profile | Extraction | Semantic search | API Keys | Feedback format |
|---------|------------|-----------------|----------|-----------------|
| `saipos` | Host | LiteLLM gateway | 1 (gateway) | stop/start/continue → Qulture |
| `startup` | Host | OpenAI | 1 (OpenAI) | freeform → markdown |
| `personal` | Host | Ollama, local | 0 | freeform → markdown |

Without an `embedding` provider (the default), search is by keyword only and
the server sends nothing anywhere.

## MCP tools

| Tool | Purpose |
|------|---------|
| `ingest` | Store a memory (text, or audio/image/video transcribed or described by the host) with the entities the host extracted — persons, topics, tasks, relationships, feedback items |
| `list_people` | List known people, so the host reuses their stored names |
| `recall` | Retrieve context about a person for 1:1, PDI, feedback, or team review |
| `get_team_context` | Overview of all reports under a leader (recursive hierarchy) |
| `search_memories` | Search by keyword (FTS5, with synonyms and inflections added by the host) and, with an embedding provider configured, by meaning — fused into one ranking, with the best matching passage of each memory |

## Architecture

- **MCP server**: Go, single binary, `CGO_ENABLED=0` — cross-compiles to Windows, Linux, macOS
- **Graph engine**: SQLite tables + recursive CTEs (same database file, CGO-free)
- **Semantic search** (optional, off by default): passages embedded in the background through an OpenAI-compatible API (OpenAI, a gateway like LiteLLM, or a local Ollama), searched by exact cosine similarity in pure Go — see [docs/configuration.md](docs/configuration.md)
- **Keyword search**: FTS5 (SQLite native full-text search)
- **Storage**: One file — `memoria.db` — contains graph, memories, vectors, and FTS index
- **AI work**: the MCP host transcribes, describes and extracts entities; the server needs no AI provider except, optionally, for embeddings

## License

MIT
