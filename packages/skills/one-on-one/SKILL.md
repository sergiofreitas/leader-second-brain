---
name: one-on-one
description: |
  Prepares and records the leader's recurring conversations — 1:1s, weekly
  syncs and skip-levels — following the organization's method (the
  playbook): before, a script and a private support page built from the
  Second Brain; after, the record of the meeting (summary, agreements with
  owner and due date, scores) stored back in memory and ready for the HR
  system. Use when the user mentions "1:1", "one-on-one", "preparar a 1:1",
  "síntese da 1:1", "semanal", "skip-level", or provides a meeting
  transcript.
---

# 1:1, weekly and skip-level

**Follow the organization's method, not one of your own.** Start every time
with `get_playbook(rite: "one_on_one")` (or `"weekly"`, `"skip_level"`): it
says what the rite is for, how often, how it is prepared, its agenda with
the time of each block, the limit of agreements, how it is recorded and its
guidelines. Where the playbook is silent, keep it simple.

## Before: script and support page

Call `recall(person_name, context: "1:1")` (`time_range: "all"` when the
last one was long ago) and build two things.

**1. The script** — fits one screen, in the rite's agenda order, with the
minutes of each block:
- the goal of the conversation in one sentence
- previous agreements with owner, due date and status — overdue first
- the rite's preparation (e.g. questions the led answers beforehand)
- for each block, at most 2 points the leader brings, each with a fact
- if the rite discusses scores: only the criteria where leader and self
  diverge by the playbook's `divergence_threshold` or more (from the last
  assessment's `divergences`)
- the rite's guidelines that apply (e.g. "the led talks first")

**2. The support page — for the leader only:**
- evidence for each point, with date and source (`occurred_at`,
  `source_url`); literal quotes go only here
- what changed and what persists: recurring topics against completed tasks
- already resolved, don't bring back
- doubts to check before the meeting
- up to 3 recommendations for the leader

The script never contains: scores of an assessment with visibility
`leader_only`, anything someone else said about the person (a relayed
feedback, `feedback_from`, a third party's observation) or literal quotes.
Those stay on the support page.

Say what is missing rather than filling gaps: no previous 1:1, no
self-assessment, no recent observation.

## After: the record

### Input

A transcript (pasted, a file, or the meeting tool's notes) or the leader's
notes. You can't listen to audio: for a recording, ask for the transcript
the meeting tool produces.

### What to extract

1. **Summary** — 3 to 5 sentences: what was discussed, the climate,
   decisions.
2. **By block** — what each block of the agenda actually covered; what was
   planned and not covered.
3. **Agreements** — at most the rite's `max_agreements`, each with owner
   (leader or led) and due date. More than that: ask the leader which stay.
4. **Previous agreements** — which were done, which remain open.
5. **Scores** — if discussed: each criterion's score by the leader and by
   the led, and the evidence where they diverge.
6. **Notes** — engagement signals, points to follow up next time.

Compare with the briefing: repeated topics, agreements done or still open.

### Store it

Call `list_people` if new people are mentioned, then `ingest` with:
- `memory_type`: `one_on_one`, `weekly` or `skip_level`; `about_person`:
  the led (for a skip-level, the person met)
- `content`: the full transcript or notes; `segments` split by subject for
  long transcripts
- `source_url`: the link to the meeting notes or transcript, if any
- `summary`, `topics` (reusing the words already used for this person)
- `tasks`: the agreements, each with `owner`, `about_person` (the led) and
  `due_date` (`YYYY-MM-DD`)
- `assessment` when scores were given: `scores` with `criterion` (from the
  playbook), `leader`, `self` and `evidence`; `cycle` if there is one
- `feedback_items` only if explicit feedback was given
- `occurred_at` (`YYYY-MM-DD`): the day of the meeting, when it wasn't today

Previous agreements the meeting shows were done: `complete_task` with their
`id` (from `pending_tasks`).

## Output

The record in the shape the rite's `record` describes, ready to paste into
the organization's system (the playbook's `feedback.target_system`, e.g.
Qulture): summary, agreements with owner and date. Scores with visibility
`leader_only` are not part of it. Answer in the user's language (default:
Portuguese).
