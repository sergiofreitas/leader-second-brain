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
  transcription: "local:whisper"     # or "toqan" or "cloud:openai"
  ocr: "local:tesseract"             # or "toqan" or "cloud:google"
  vlm: "local:ollama"                # or "toqan" or "cloud:openai"
  embedding: "local:sentence_transformers"
  llm: "local:ollama"                # or "toqan" or "cloud:openai"

  local:
    whisper:
      model: "base"
      device: "cpu"
    tesseract:
      language: "por"
    sentence_transformers:
      model: "all-MiniLM-L6-v2"
      device: "cpu"
      dimensions: 384
    ollama:
      endpoint: "http://localhost:11434"
      models:
        llm: "llama3.2"
        vlm: "llava"

  # Cloud providers (if used):
  openai:
    api_key: "${OPENAI_API_KEY}"
    models:
      vlm: "gpt-4o"
      llm: "gpt-4o-mini"

  # Toqan (if used):
  toqan:
    endpoint: "${TOQAN_MCP_ENDPOINT}"
```

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
  engine: graphlite    # openCypher on SQLite (default, CGO-free)
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

See `examples/` directory:
- `saipos/` — Saipos internal (Toqan, Qulture, stop/start/continue)
- `startup/` — Generic startup (hybrid, freeform)
- `personal/` — Minimal personal (all local, freeform)
