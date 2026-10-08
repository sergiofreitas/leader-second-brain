---
name: one-on-one
description: |
  Prepares and synthesizes 1:1 (one-on-one) meetings: a briefing from the
  Second Brain before, and after it a synthesis of the transcript (summary,
  topics, tasks for leader and led, divergences) that is stored back in
  memory. Use when the user mentions "1:1", "one-on-one", "síntese da 1:1",
  "preparar a 1:1", or provides a meeting transcript.
---

# 1:1

## Before: briefing

Call `recall(person_name, context: "1:1")` and present:
- the last 1:1s and what was agreed
- pending tasks (who owns each), and which previous ones look done
- recent observations and feedbacks
- recurring topics (how many times each came up)
- suggested points for this 1:1

## After: synthesis

### Input

A transcript (pasted text or a file) or the leader's notes. You can't listen
to audio: for a recording, ask for the transcript the meeting tool or the
1:1 app produces.

### What to extract

1. **Resumo** — 3 to 5 sentences: what was discussed, emotional climate,
   alignments and agreements.
2. **Tópicos discutidos** — what was actually discussed, not just the
   planned agenda.
3. **Tarefas do líder** — what the leader committed to ("vou", "me
   encarrego").
4. **Tarefas do liderado** — what the led committed to.
5. **Divergências** — for each behavior where the leader's and the led's
   scores differ by 2 or more points: the relevant passages, both
   perspectives, and how it was addressed.
6. **Anotações** — engagement signals, standout moments, points to follow
   up in the next 1:1.

Compare with the briefing: repeated topics, tasks done or still open,
patterns over time.

### Store it

Call `list_people` if new people are mentioned, then `ingest` with:
- `modality: "audio"` for a transcript of a recording (`file_path` if there
  is a file), or `"text"`
- `memory_type: "one_on_one"`, `about_person`: the led
- `content`: the full transcript or notes
- `segments`: the transcript split by subject into consecutive passages of a
  few paragraphs — essential for long transcripts, so search can find each
  subject later
- `summary`: the Resumo in one sentence
- `topics`: the discussed topics, reusing words already used for this person
- `tasks`: the leader's tasks (`owner`: the leader) and the led's tasks
  (`owner`: the led), with `about_person`: the led
- `feedback_items` only if explicit feedback was given in the 1:1
- `occurred_at` (`YYYY-MM-DD`): the day of the 1:1, when it wasn't today

Tasks from the briefing that the 1:1 shows were done: call `complete_task`
with their `id` (from `pending_tasks`).

## Output

The synthesis in Markdown, ready to paste into the organization's HR system
(e.g. Qulture). Answer in the user's language (default: Portuguese).
