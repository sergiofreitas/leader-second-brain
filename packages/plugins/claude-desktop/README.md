# Second Brain — Claude Desktop extension

Memory for leadership in Claude Desktop (chat and Cowork): capture what you
observe about your team and recall it before 1:1s, PDIs and feedback
sessions. The extension carries the MCP server binary, so there is nothing
else to install.

## Install

1. **The extension** — open `second-brain.mcpb` (from the release, or built
   with `make mcpb`) with Claude Desktop, or drag it into
   Settings > Extensions, and click Install.
2. **The options** (optional) — in Settings > Extensions > Second Brain:
   - **Profile**: `saipos` by default (stop/start/continue, Qulture).
   - **Embedding gateway URL and key**: turn on semantic search through the
     LiteLLM gateway. Left empty, search is by keyword only and nothing is
     sent anywhere.
3. **The skills** — upload each zip from `dist/skills/` (built with
   `make skills-zip`) in Settings > Capabilities > Skills. Extensions only
   carry the server; the skills are what teach Claude the method (capture,
   briefing, 1:1, feedback, PDI).

The data stays on the computer, in `~/.second-brain/`. A config written by
`second-brain init` at `~/.second-brain/config.yaml` takes precedence over
the extension's profile, so Claude Code and Claude Desktop share the same
config and memory.

## Platforms

Windows (x64) and macOS (Apple Silicon and Intel: `second-brain-darwin.sh`
starts the binary for the Mac's architecture).
