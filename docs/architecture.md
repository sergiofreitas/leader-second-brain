# Architecture

## Overview

Second Brain is a local-first MCP server that captures leadership observations
and recalls them contextually. The entire system runs as a single Go binary
with one SQLite file for all data.

## Layers

```
MCP Host (Claude Code, Codex, OpenCode)
    │
    │ MCP (stdio)
    ▼
MCP Server (Go binary)
    │
    ├── Tools: ingest | list_people | recall | get_team_context | search_memories
    │
    ├── Graph Engine: SQLite tables + recursive CTEs
    ├── Vector Search: in-memory cosine similarity (embeddings in SQLite)
    ├── Keyword Search: FTS5 (SQLite native)
    │
    └── Provider Abstraction:
        transcription | ocr | vlm | embedding | llm
        (local: Whisper, Tesseract, Ollama, sentence-transformers)
        (cloud: OpenAI, Anthropic)
```

## Data flow

### Remember (ingest)

1. Input arrives as text, audio, image, or video
2. The MCP host normalizes it to text (transcribes / describes) and passes it in `content`
3. The host extracts entities (persons, topics, tasks, relationships, feedback items),
   reusing known names from `list_people`; the server validates them and resolves
   names ignoring case and accents
   (the server's own LLM provider is only a fallback when the host sends none)
4. Embedding generated, stored in SQLite and added to the in-memory vector index
5. Content indexed in FTS5 for keyword search
6. Graph nodes and edges created in the graph tables (same SQLite file)
7. Steps 4-6 run in a single transaction: everything is stored or nothing is

### Recall

1. Leader requests context for a person + situation
2. Graph traversal: hierarchy, memories, tasks, feedbacks, assessments
3. FTS5 search: keyword matches on person name
4. Vector search: semantic similarity to the context type
5. Hybrid retriever fuses results and assembles a briefing
6. Context-specific recommendation generated

## Single-file storage

All data lives in one SQLite database file:
- Graph nodes and edges (graph_nodes, graph_edges tables)
- Memories and metadata (structured tables)
- FTS5 index (full-text search virtual table)
- Vector embeddings (memory_embeddings table)

No external databases, no servers, no network calls for storage.

## Provider abstraction

Each AI capability is behind an interface:

```go
type TranscriptionProvider interface { Transcribe(path string) (*Result, error) }
type OCRProvider interface { Extract(path string) (*Result, error) }
type VLMProvider interface { Describe(path string, prompt string) (string, error) }
type EmbeddingProvider interface { Embed(text string) ([]float32, error) }
type LLMProvider interface { ExtractEntities(text string, cfg) (*Extraction, error) }
```

Configuration in `config.yaml` selects which provider to use for each capability.
By default the MCP host does transcription, description and entity extraction
(`host`), so the server needs no AI provider of its own; a server-side provider
can take over a capability with a one-line config change once it is wired.
