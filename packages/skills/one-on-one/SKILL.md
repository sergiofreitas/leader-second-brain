---
name: one-on-one
version: 0.1.0
description: |
  Synthesizes a 1:1 (one-on-one) meeting from a transcript or conversation,
  extracting discussed topics, tasks for leader and led, summary, and
  culture score divergences. Uses Second Brain recall to enrich the
  synthesis with historical context. Use when the user mentions "1:1",
  "one-on-one", "síntese da 1:1", or provides a meeting transcript.
---

# 1:1 Synthesis

## Prerequisites

1. Call `recall(person_name, context: "1:1")` to get historical context
2. Present the briefing to the leader before processing the transcript

## Input

The user provides either:
- A transcript (text or file)
- An audio recording (the system transcribes via `ingest`)
- A JSON package from a 1:1 app

## What to extract

### 1. Resumo
3-5 sentences capturing the essence: what was discussed, emotional state,
alignment points, and agreements.

### 2. Tópicos Discutidos
Themes actually discussed — not just planned agenda items.

### 3. Tarefas do Líder
Actions the leader committed to ("vou fazer", "me encarrego de").

### 4. Tarefas do Liderado
Actions the led committed to ("vou", "pretendo", "meu plano é").

### 5. Discussão de Divergências
For each behavior with ≥2 point difference between leader and led scores:
- Find the relevant transcript passages
- Synthesize both perspectives
- Note how the difference was addressed

### 6. Anotações
Relevant observations: emotional climate, engagement signals, standout moments,
items for follow-up in the next 1:1.

## Output format

Markdown, ready to paste into the organization's HR system (Qulture, Lattice, etc.).

## Historical enrichment

Use the recall results to:
- Compare with previous 1:1s (are we repeating topics?)
- Check if previous tasks were completed
- Identify patterns over time
- Flag recurring issues
