# Second Brain — Claude Code plugin

Memory for leadership in Claude Code: capture what you observe about your
team and recall it before 1:1s, PDIs and feedback sessions.

## Install

1. **The binary** — the plugin starts the `second-brain` MCP server, which
   must be on your PATH:

   ```powershell
   # Windows (PowerShell)
   irm https://raw.githubusercontent.com/sergiofreitas/leader-second-brain/main/install.ps1 | iex
   ```

   ```bash
   # macOS / Linux
   curl -fsSL https://raw.githubusercontent.com/sergiofreitas/leader-second-brain/main/install.sh | sh
   ```

2. **A config** (optional — without one, data stays in `~/.second-brain/` and
   search is by keyword only):

   ```bash
   second-brain init --profile saipos
   second-brain init --help   # profiles and semantic search options
   ```

3. **The plugin**:

   ```bash
   claude plugin marketplace add sergiofreitas/leader-second-brain
   claude plugin install second-brain@second-brain
   ```

4. Restart Claude Code (from a new terminal on Windows, so it sees the PATH
   the installer updated) and check the server with `/mcp`.

## What it adds

**MCP tools**

| Tool | Purpose |
|------|---------|
| `ingest` | Store a memory with the entities Claude extracted from it |
| `list_people` | Known people, to reuse their stored names |
| `recall` | Context about a person for a 1:1, PDI, feedback... |
| `get_team_context` | Everyone under a leader |
| `search_memories` | Search by keyword (plus synonyms) and, if configured, by meaning |

**Skills** — `second-brain` (capture and recall), `one-on-one`,
`pdi-gap-map`, `feedback`. Claude uses them on its own when the conversation
calls for them.

**Commands** — `/second-brain:observe [what you observed]` and
`/second-brain:briefing [name]`.

## Privacy

Memories are stored on your machine in one SQLite file
(`~/.second-brain/memoria.db`). Claude reads what you capture, as with
anything you tell it. The server itself only sends text out if you configure
semantic search with a cloud embedding provider — see
[docs/configuration.md](../../../docs/configuration.md).

## Development

The skills here are a copy of `packages/skills` (plugins are copied on
install, so they can't reference files outside their folder). Edit them
there, then run `make sync-skills`; `make check-skills` fails if the copy is
out of date.

Try local changes without publishing:

```bash
claude --plugin-dir packages/plugins/claude-code
claude plugin validate packages/plugins/claude-code --strict
```
