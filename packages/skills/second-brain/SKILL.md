---
name: second-brain
version: 0.1.0
description: |
  Base skill for the Second Brain memory system. Guides the leader to capture
  observations (text, voice, image, video) using the `ingest` MCP tool and
  retrieve context using `recall` before 1:1s, PDIs, and feedback sessions.
  Use this skill when the user wants to "remember something about a person",
  "get a briefing", "search memories", or "review their team".
---

# Second Brain — Memory Capture & Recall

## When to use this skill

The user wants to:
- Record an observation about a team member
- Get context before a 1:1, PDI, or feedback session
- Search their memory for specific topics or keywords
- Review their team's status

## Tools available

| Tool | Purpose |
|------|---------|
| `ingest` | Capture a memory (text, audio, image, video) |
| `recall` | Get context about a person for a specific situation |
| `get_team_context` | Overview of all reports under a leader |
| `search_memories` | Search by keyword or semantic similarity |

## How to ingest

Simply ask the user to describe what they observed, or accept an audio/image/video file.

```
User: "Anotei que o Evandro resolveu sozinho um bug de TEF hoje"
→ Call ingest(modality: "text", content: "...", about_person: "Evandro")

User: "Gravei um áudio da conversa que tive com o Bernardo"
→ Call ingest(modality: "audio", file_path: "/path/to/audio.mp3")

User: "Tirei foto do quadro da retrospectiva"
→ Call ingest(modality: "image", file_path: "/path/to/photo.jpg")
```

The system will:
1. Transcribe/OCR/describe the input
2. Extract persons, topics, and tasks
3. Detect content type (observation, feedback, 1:1, etc.)
4. Store in the knowledge graph + vector index

## How to recall

Before any leadership activity, call `recall` with the person's name and context:

```
User: "Vou fazer 1:1 com o Sérgio amanhã"
→ Call recall(person_name: "Sérgio", context: "1:1")

User: "Preciso montar o PDI do Bernardo"
→ Call recall(person_name: "Bernardo", context: "pdi")

User: "Como está minha equipe?"
→ Call get_team_context(leader_name: "Sérgio")
```

## What the recall returns

- **Hierarchy**: who manages this person, who reports to them
- **Memories**: all observations, conversations, and notes about them
- **Tasks**: pending tasks targeting this person
- **Feedbacks**: previously recorded feedbacks
- **Assessments**: last PDI evaluations with gaps
- **Patterns**: behavioral patterns detected over time
- **Recommendation**: context-aware suggestion for the situation

## Language

Always respond in the user's language (default: Portuguese).
