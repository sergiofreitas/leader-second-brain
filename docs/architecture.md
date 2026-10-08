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
    ├── Vector Search: in-memory cosine similarity over passages (optional)
    ├── Indexer: embeds new passages in the background
    ├── Keyword Search: FTS5 (SQLite native)
    │
    └── Provider Abstraction:
        transcription | ocr | vlm | embedding | llm
        (host: the MCP host transcribes, describes and extracts)
        (embedding: OpenAI-compatible API — OpenAI, LiteLLM, Ollama...)
```

## Data flow

### Remember (ingest)

1. Input arrives as text, audio, image, or video
2. The MCP host normalizes it to text (transcribes / describes) and passes it in `content`
3. The host extracts entities (persons, topics, tasks, relationships, feedback items),
   reusing known names from `list_people`; the server validates them and resolves
   names ignoring case and accents
   (the server's own LLM provider is only a fallback when the host sends none)
4. Content split into passages (~200 words, or the host's `segments` for
   long transcripts) and indexed in FTS5 for keyword search
5. Graph nodes and edges created in the graph tables (same SQLite file)
6. Steps 4-5 run in a single transaction: everything is stored or nothing is
7. In the background, the indexer embeds the new passages with the
   configured embedding provider and adds them to the in-memory vector index
   (only when semantic search is enabled — see below)

### Recall

1. Leader requests context for a person + situation
2. Graph traversal: hierarchy, memories, tasks (with owner), feedbacks (with
   items and who gave them), assessments, recurring topics
3. FTS5 search: keyword matches on the person's name
4. The retriever assembles a briefing with a context-specific recommendation

### Search

`search_memories` searches by keyword (FTS5, BM25 ranking) or, when an
embedding provider is configured, by meaning: the query is embedded and
compared with every passage (exact cosine similarity in Go). Each memory is
returned once, with its best matching passage as the excerpt — for a long
transcript, the part that matters rather than the whole text.

## Semantic search

Semantic search is optional and off by default. Embedding providers are
OpenAI-compatible APIs (`/v1/embeddings`): OpenAI, a gateway such as
LiteLLM, or a local Ollama — see `docs/configuration.md`.

- **Passages.** Models embed a few hundred tokens well; a one-hour 1:1
  (~13k tokens) embedded whole would average away its subjects. Content is
  split into passages of whole sentences with a small overlap, falling back
  to word windows for unpunctuated automatic transcripts. The host can split
  long content by subject itself (`segments`), which gives better passages.
- **Background indexing.** Embedding many passages can take longer than a
  tool call should block, so `ingest` only stores them. The indexer embeds
  pending passages in batches, woken after each ingest, when a retry is due,
  and every minute.
- **The queue is the data.** A passage is pending when it has no vector for
  the current model. Nothing is lost on restart; failures back off
  exponentially (1 min up to 6 h) and are given up after 8 attempts.
- **Vectors per model.** Each vector records the model that produced it.
  Changing model or dimensions drops the old vectors and re-embeds
  everything; vectors of different models are never compared.
- **Search cost.** The vectors of the current model are kept in memory; a
  search over 75k passages of 384 dimensions takes ~13 ms.

## Single-file storage

All data lives in one SQLite database file:
- Graph nodes and edges (graph_nodes, graph_edges tables)
- Memories and metadata (structured tables)
- FTS5 index (full-text search virtual table)
- Passages and their vectors (memory_chunks, chunk_embeddings, embedding_failures)

No external databases, no servers, no network calls for storage.

## Provider abstraction

Each AI capability is behind an interface:

```go
type TranscriptionProvider interface { Transcribe(path string) (*Result, error) }
type OCRProvider interface { Extract(path string) (*Result, error) }
type VLMProvider interface { Describe(path string, prompt string) (string, error) }
type EmbeddingProvider interface {
    Embed(query string) ([]float32, error)          // a search query
    EmbedBatch(passages []string) ([][]float32, error)
    Model() string                                  // identifies the vectors
}
type LLMProvider interface { ExtractEntities(text string, cfg) (*Extraction, error) }
```

Configuration in `config.yaml` selects which provider to use for each capability.
By default the MCP host does transcription, description and entity extraction
(`host`), so the server needs no AI provider of its own; a server-side provider
can take over a capability with a one-line config change once it is wired.
Embedding is the one capability the host can't provide (an MCP host exposes
no embedding vectors); it is wired through an OpenAI-compatible provider.
