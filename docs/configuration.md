# Configuration

Second Brain is configured via a single YAML file. The default location is
`~/.second-brain/config.yaml`.

## Sections

### storage

```yaml
storage:
  sqlite:
    path: "~/.second-brain/memoria.db"
  media:
    path: "~/.second-brain/media/"
    max_size_mb: 5000
    retention_days: 365
```

### providers

```yaml
providers:
  transcription: "host"
  ocr: "host"
  vlm: "host"
  llm: "host"
  embedding: none        # see "Semantic search (embedding)" below
```

`host` means the MCP host does that step: it transcribes audio, describes
images and extracts the entities (people, topics, tasks, relationships,
feedback items) before calling `ingest`, guided by the tool's description.
This is how the server works today; `transcription`, `ocr`, `vlm` and `llm`
are reserved for server-side providers that are not wired yet.

#### Semantic search (embedding)

Keyword search always works. Semantic search ("observações sobre dificuldade
de comunicação" finding "ele não explica as decisões ao time") needs an
embedding model, and is **off by default**: with `embedding: none` nothing
is sent anywhere by the server.

```yaml
providers:
  embedding:
    provider: openai-compatible   # none | openai | ollama | openai-compatible
    base_url: "https://litellm.example.com/v1"
    model: "text-embedding-3-small"
    api_key: "${SECOND_BRAIN_EMBEDDING_KEY}"
    # Optional:
    dimensions: 0          # shorter vectors, for models that support it (text-embedding-3)
    query_prefix: ""       # e.g. "query: " for e5 models
    document_prefix: ""    # e.g. "passage: " for e5 models
    headers: {}            # extra HTTP headers, e.g. for a gateway
    batch_size: 64         # texts per request
    timeout_seconds: 60    # per request
```

| Provider | Where the text goes | Defaults |
|---|---|---|
| `none` | nowhere — semantic search is off | — |
| `ollama` | stays on the machine (a model served by [Ollama](https://ollama.com)) | `base_url: http://localhost:11434/v1`; `model` is required — `bge-m3` is a good multilingual choice (`ollama pull bge-m3`) |
| `openai` | OpenAI | `base_url: https://api.openai.com/v1`, `model: text-embedding-3-small`; `api_key` is required |
| `openai-compatible` | whatever `base_url` points to: a gateway such as LiteLLM, a proxy, LM Studio, vLLM... | `base_url` and `model` are required |

**What leaves the machine.** With `openai` or `openai-compatible` pointing to
a cloud service, the text of every memory (split in passages) is sent to it
to be embedded, and each semantic search sends the query. Note that the MCP
host (Claude, Codex...) already reads that content too. Memories about
people are sensitive: pick a provider your organization allows for that
data, or `ollama` to keep it local.

**API keys** belong in environment variables, referenced as `${NAME}`:
`${...}` is expanded when the config is read, so the key is never written in
the file. The server never logs or returns the key.

**How it works.** Each memory is split into passages of ~200 words (the host
can split long transcripts by subject itself, with `ingest`'s `segments`).
Passages are embedded in the background, so `ingest` returns at once even
for a one-hour transcript; failures (gateway down, rate limits) are retried
with backoff. When semantic search runs while passages are still pending or
failing, its result includes an `index_status` with the progress and the
last error. Changing `model` or `dimensions` re-embeds every passage with the
new model; switching between providers that serve the same model doesn't.

The server refuses to start with an unknown provider or a missing required
field, with a message saying what to fix (e.g. an `api_key` whose
environment variable isn't set).

### feedback

```yaml
feedback:
  format: stop_start_continue    # or "freeform" or "sandwich"
  categories:
    - { id: "stop", label: "Parar de fazer", color: "#d97757" }
    - { id: "start", label: "Começar a fazer", color: "#788c5d" }
    - { id: "continue", label: "Continuar fazendo", color: "#6a9bcc" }
  items_per_category: 2
  target_system: markdown         # or "qulture" or "lattice" or "json"
```

### graph

```yaml
graph:
  engine: sqlite       # graph tables + recursive CTEs in the same SQLite file (default, CGO-free)
```

### transport

```yaml
transport:
  type: stdio    # or "http" (with port: 8080)
```

### skills

```yaml
skills:
  second_brain: true
  one_on_one: true
  pdi_gap_map: true
  feedback: true
```

## Example profiles

Built-in profiles (`second-brain init --profile NAME`, see `packages/mcp-server/configs/profiles/`):
- `saipos` — Saipos internal (host extraction, embeddings through the LiteLLM gateway, Qulture, stop/start/continue)
- `startup` — Generic startup (host extraction, OpenAI embeddings, freeform)
- `personal` — Minimal personal (host extraction, local Ollama embeddings, freeform)
