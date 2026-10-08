---
name: pdi-gap-map
version: 0.1.0
description: |
  Conducts a conversational PDI (Individual Development Plan) assessment,
  classifying the led's job level across valences from a free-text description
  by the leader, and generating a visual gap map (radar chart) for the next
  level. Uses Second Brain recall to enrich the assessment with accumulated
  evidence. Use when the user mentions "PDI", "avaliação", "gap de evolução",
  "job level", or "progressão de carreira".
---

# PDI — Gap Map Assessment

## Prerequisites

1. Call `recall(person_name, context: "pdi")` to get historical context
   including previous assessments, feedbacks, and observations
2. Use this evidence to support or challenge the leader's descriptions

## Flow

### Step 1 — Basic info
Ask: name, current job level, track (technical or leadership).

### Step 2 — Valence-by-valence assessment
For each valence, one at a time:
1. Ask the leader to describe how the person operates in that valence
2. Wait for 2-3+ sentences of free description
3. Classify the level using the framework definitions
4. Compare with previous assessment (if recall returned one)

### Step 3 — Gap analysis
For each valence, identify:
- Current score vs target score
- Gap severity (low/medium/high)
- Evidence from recall (observations, feedbacks, patterns)

### Step 4 — Output
- Radar chart (PNG) showing current vs target across valences
- Descriptive report with gaps, evidence, and development suggestions

## Historical enrichment

The recall results provide:
- Previous assessment scores for comparison
- Observations that serve as evidence
- Feedbacks that relate to specific valences
- Behavioral patterns that indicate level

## No persistence by default

PDI assessments are snapshots. The assessment itself is stored as a memory
in the graph via `ingest` after completion — the leader doesn't need to
manually save anything.
