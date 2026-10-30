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

An embedding provider that can't start — an unknown provider, a missing
required field, an `api_key` whose environment variable isn't set — doesn't
stop the server: it runs with keyword search only and says why, in the
`search_memories` description and results (`semantic_unavailable`) and in
`ingest`'s `semantic_index`, so the MCP host can tell the user what to fix.
An environment variable set after the MCP host was started isn't seen by
the server: restart the host (and the terminal or editor it runs in).

### feedback

```yaml
feedback:
  format: stop_start_continue    # or "freeform" or "sandwich"
  categories:
    - { id: "stop", label: "Parar de fazer", color: "#d97757" }
    - { id: "continue", label: "Continuar fazendo", color: "#6a9bcc" }
    - { id: "start", label: "Começar a fazer", color: "#788c5d" }
  items_per_category: 2           # at most
  target_system: markdown         # or "qulture" or "lattice" or "json"
  item_template: "In [situation], I noticed [behavior]. The impact was [impact]. We'll know it changed when [signal]."
  checks:                         # answered before preparing a feedback
    - "Nothing in it is a surprise to the person."
```

The categories are given in the order they are listed. `ingest` accepts
each item with its `situation`, `behavior`, `impact` and `change_signal`.

### The method: rites, assessment, alerts

How the leader's rites are run, how people are scored and when the team
view alerts. The skills read it with the `get_playbook` tool and follow
it, so the method lives in the configuration, not in the skills. A profile
that defines no rites gets generic ones (`one_on_one`, `weekly`,
`skip_level`, `feedback`).

```yaml
rites:                       # keyed by the memory type that records them
  one_on_one:
    name: "1:1"
    purpose: "The led's development: performance, culture, career."
    cadence_days: 60
    duration_minutes: 60
    preparation:
      - "48 h before, the led answers: what went well, where they got stuck, what they need from me."
    agenda:
      - { block: "Rapport", minutes: "5", guidance: "Open with the previous agreements." }
      - { block: "Performance", minutes: "15-20", guidance: "The led assesses first." }
      - { block: "Agreements", minutes: "5-10", guidance: "Each with an owner and a date." }
    max_agreements: 3
    record: "In the HR system within a week, separately from the feedback."
    record_template: |       # optional: the text the record follows
      HOW THEY ARRIVED
      [...]
      AGREEMENTS
      - [action] — owner: [who] — due: [date]
    guidelines:
      - "The 1:1 is the led's: they talk first in each block."

assessment:
  name: "Culture score"
  scale_min: 1
  scale_max: 5
  divergence_threshold: 2    # leader vs self-assessment gaps flagged from this
  visibility: leader_only    # or "shared": who may see the scores
  guidelines: ["Justify divergences with practical evidence."]
  criteria:                  # none: any criterion is accepted
    - name: "Have courage"
      group: "Ownership"
      description: "Act without having every answer, responsibly."
      question: "Do I propose the smallest experiment to test a new idea?"
      example: "A new framework could speed up delivery..."

alerts:
  quiet_days: 60             # nothing recorded about someone for this long
  stale_task_days: 30        # a task pending for this long
```

`ingest` checks an `assessment` against this: known criteria (by id or
name; the id is the name in lowercase, without accents, hyphenated), scores
within the scale. It stores the scores with their averages and the
divergences, and `recall` returns them marked with their visibility.

#### The playbook file: what an organization doesn't publish

The profiles are embedded in the binary and published with the repository.
An organization's internal method — its forms, its culture behaviors —
goes in a **playbook file** each leader keeps on their computer, by
default `~/.second-brain/playbook.yaml` (or the path in `playbook_file`).
It has the same sections — `rites`, `feedback`, `assessment`, `alerts` —
and overlays the profile: what it sets replaces what the profile set (a
rite it names replaces that whole rite, a list replaces the list), the
rest is kept.

```yaml
playbook_file: ~/.second-brain/playbook.yaml   # in config.yaml (optional)
```

The default file is optional; a `playbook_file` that doesn't exist is an
error. A playbook with an error (an unknown key, a repeated criterion, an
inverted scale) doesn't stop the server: it runs with the profile's
method and `get_playbook` returns the error in `playbook_error`. Restart
the MCP host after changing the file.

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
- `saipos` — Saipos internal (host extraction, embeddings through the LiteLLM gateway, Qulture, stop/continue/start, the cadence of its rites and a 1-5 culture score seen only by the leader; the forms and the culture behaviors come from the playbook file Saipos hands its leaders)
- `startup` — Generic startup (host extraction, OpenAI embeddings, freeform)
- `personal` — Minimal personal (host extraction, local Ollama embeddings, freeform)
