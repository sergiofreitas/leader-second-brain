# Plugin Development

Second Brain supports multiple AI harnesses through MCP. Each harness has
its own plugin format, but all connect to the same MCP server binary.

## How plugins work

```
Harness (Claude Code / Codex / OpenCode)
    │
    │ Plugin provides:
    │   1. MCP server config (how to launch the binary)
    │   2. Skills (SKILL.md files that guide the LLM)
    │   3. Commands (harness-specific shortcuts)
    │   4. AGENTS.md (instructions for the harness's LLM)
    │
    ▼
MCP Server (second-brain binary)
    │
    └── Tools: ingest | recall | get_team_context | search_memories
```

## Adding a new harness

1. Create a directory under `packages/plugins/<harness-name>/`
2. Add the harness's MCP config format
3. Copy skills from `packages/skills/`
4. Add a README with installation instructions
5. Add AGENTS.md with instructions for the harness's LLM

## Skill format

Skills are portable — the same `SKILL.md` works across harnesses. Each skill:

- Has YAML frontmatter with name, version, description
- Contains instructions for the LLM on when and how to use the MCP tools
- Is harness-agnostic (no Claude Code or Codex specifics)

## Harness-specific differences

| Feature | Claude Code | Codex | OpenCode |
|---------|-------------|-------|----------|
| MCP config | `.mcp.json` (JSON) | `mcp.json` (JSON) or `config.toml` (TOML) | `opencode.json` (JSON) |
| Plugin manifest | `plugin.json` | `plugin.json` | Not needed (config-based) |
| Skills | `skills/` directory | `skills/` directory | `.opencode/skills/` |
| Commands | `commands/` directory | N/A | N/A |
| Agent instructions | Commands/Skills | `AGENTS.md` | `.opencode/AGENTS.md` |
| Path placeholders | `${CLAUDE_PLUGIN_ROOT}` | `${...}` env vars | Direct paths |
