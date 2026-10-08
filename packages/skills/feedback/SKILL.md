---
name: feedback
version: 0.1.0
description: |
  Structures feedback using the organization's configured format
  (stop/start/continue, freeform, or custom). Uses Second Brain recall
  to check previous feedbacks and avoid repetition. Use when the user
  wants to "give feedback", "montar feedback", or mentions
  "stop/start/continue".
---

# Feedback Structuring

## Prerequisites

1. Call `recall(person_name, context: "feedback")` to check:
   - What feedbacks have already been given
   - What observations exist about this person
   - What development needs are active

## How it works

1. Ask the leader to describe what they observed
2. The `ingest` tool detects feedback content and structures it
   according to the configured format (from config.yaml)
3. Categories come from config — not hardcoded:
   - stop_start_continue → [stop, start, continue]
   - freeform → [general]
   - custom → whatever the organization defined

## Avoiding repetition

Before structuring new feedback, check if:
- The same point was already given in a previous feedback
- The development need it addresses is already being worked on
- There are observations that support or contradict the feedback

## Output

The feedback is stored in the graph automatically via `ingest`:
- Feedback node with format and date
- FeedbackItem nodes for each category item
- ABOUT edge connecting to the person
- Links to any skills the feedback addresses

## Language

Respond in the user's language. Default: Portuguese.
