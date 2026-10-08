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
├── .claude-plugin/           Claude Code marketplace (lists the plugin)
├── install.ps1 / install.sh  Binary installers (Windows / macOS, Linux)
├── docs/                    Documentation
├── Makefile                 Build, test, install, cross-compile
└── README.md
```

Configuration profiles (`default`, `saipos`, `startup`, `personal`) live in
`packages/mcp-server/configs/profiles/` and are embedded in the binary.

## Install

### 1. The binary

**Windows** (PowerShell):

```powershell
irm https://raw.githubusercontent.com/sergiofreitas/leader-second-brain/main/install.ps1 | iex
```

**macOS / Linux**:

```bash
curl -fsSL https://raw.githubusercontent.com/sergiofreitas/leader-second-brain/main/install.sh | sh
```

The installer downloads the binary for your platform from the latest GitHub
release, checks its SHA-256 against the release's `checksums.txt`, and puts
it in `~/.second-brain/bin` (added to your PATH on Windows; the script tells
you how on macOS/Linux). Run `second-brain version` to check.

### 2. A config (optional)

Without a config the server uses the defaults: data in `~/.second-brain/`,
search by keyword, nothing sent anywhere. To pick a profile or turn on
semantic search:

```bash
second-brain init --profile saipos                    # stop/start/continue, Qulture, LiteLLM gateway
second-brain init --profile saipos --embedding openai # same, with OpenAI embeddings
second-brain init --help                              # all profiles and options
```

`init` tells you which environment variables the config reads (API keys)
and whether they are set. See [docs/configuration.md](docs/configuration.md).

### 3. The plugin for your harness

**Claude Code**:

```bash
claude plugin marketplace add sergiofreitas/leader-second-brain
claude plugin install second-brain@second-brain
```

Restart Claude Code, then try: *"Anota que o Evandro resolveu sozinho um bug
de TEF hoje"* or `/second-brain:briefing Evandro`.

### Build from source

```bash
make build    # CGO-free binary at ./second-brain
make test
make install  # copies it to /usr/local/bin
```

## Other harnesses

Install the binary and (optionally) a config as above, then register the MCP
server. It reads `~/.second-brain/config.yaml` by default.

### OpenAI Codex

Add to `~/.codex/config.toml`:
```toml
[mcp_servers.second-brain]
command = "second-brain"
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
      "command": ["second-brain"],
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
