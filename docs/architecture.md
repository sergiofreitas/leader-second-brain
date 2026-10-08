# Architecture

## Overview

Second Brain is a local-first MCP server that captures leadership observations
and recalls them contextually. The entire system runs as a single Go binary
with one SQLite file for all data.

## Layers

```
MCP Host (Claude Code, Codex, OpenCode, Toqan)
    │
    │ MCP (stdio)
    ▼
MCP Server (Go binary)
    │
    ├── Tools: ingest | recall | get_team_context | search_memories
    │
    ├── Graph Engine: Graphlite (openCypher on SQLite)
    ├── Vector Search: sqlite-vec (embeddings)
    ├── Keyword Search: FTS5 (SQLite native)
    │
    └── Provider Abstraction:
        transcription | ocr | vlm | embedding | llm
        (local: Whisper, Tesseract, Ollama, sentence-transformers)
        (cloud: OpenAI, Anthropic)
        (proxy: Toqan)
```

## Data flow

### Remember (ingest)

1. Input arrives as text, audio, image, or video
2. Provider normalizes to text (transcribe / OCR / describe)
3. LLM extracts entities: persons, topics, tasks, relationships, feedback items
4. Embedding generated and stored in sqlite-vec
5. Content indexed in FTS5 for keyword search
6. Graph nodes and edges created in Graphlite (same SQLite file)

### Recall

1. Leader requests context for a person + situation
2. Graph traversal: hierarchy, memories, tasks, feedbacks, assessments
3. FTS5 search: keyword matches on person name
4. Vector search: semantic similarity to the context type
5. Hybrid retriever fuses results and assembles a briefing
6. Context-specific recommendation generated

## Single-file storage

All data lives in one SQLite database file:
- Graph nodes and edges (Graphlite tables)
- Memories and metadata (structured tables)
- FTS5 index (full-text search virtual table)
- Vector embeddings (sqlite-vec virtual table)

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
Switching from Toqan to local Ollama is a one-line config change.
