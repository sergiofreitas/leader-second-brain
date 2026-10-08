---
name: pdi-gap-map
description: |
  Conducts a conversational PDI (Individual Development Plan) assessment:
  classifies the led's job level in each valence from the leader's free-text
  description, using evidence accumulated in the Second Brain, and builds a
  gap map (radar chart) for the next level. Use when the user mentions
  "PDI", "avaliação", "gap de evolução", "job level" or "progressão de
  carreira".
---

# PDI — gap map

## Context first

Call `recall(person_name, context: "pdi")` for the previous assessments,
feedbacks, observations and recurring topics. Use them as evidence to
support — or question — what the leader describes. For a specific valence,
`search_memories` with `query` and `terms` (synonyms, inflections, prefixes
like `comunic*`) finds older observations.

## Flow

1. **Basic info** — name, current job level, track (technical or
   leadership).
2. **Valence by valence** — for each valence, one at a time:
   - ask the leader how the person operates in it (2-3+ sentences)
   - classify the level with the framework definitions
   - show the evidence from the Second Brain, and compare with the previous
     assessment, if any
3. **Gap analysis** — per valence: current vs target level, gap severity
   (low/medium/high), evidence.
4. **Output** — a radar chart (current vs target) and a report with gaps,
   evidence and development suggestions.

## Store it

When the leader approves the assessment, call `ingest` with
`memory_type: "assessment"`, `about_person`, the report as `content`, the
valences with gaps as `topics`, and the agreed development actions as
`tasks` (`owner` and `about_person`).

Answer in the user's language (default: Portuguese).
