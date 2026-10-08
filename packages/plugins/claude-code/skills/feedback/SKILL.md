---
name: feedback
description: |
  Structures feedback about a person in the organization's configured
  format (stop/start/continue, freeform or custom), using the Second Brain
  to avoid repeating points already given and to ground each point in
  recorded observations. Use when the user wants to "give feedback",
  "montar feedback", "registrar um feedback", or mentions
  "stop/start/continue".
---

# Feedback

## 1. Context first

Call `recall(person_name, context: "feedback", time_range: "all")` — every
feedback ever given, not only the last 90 days — and look at:
- feedbacks already given (their items and dates) — don't repeat a point
  unless it is a follow-up, and then say so
- observations that support, or contradict, what the leader wants to say
- pending tasks about this person

## 2. Build the feedback with the leader

Ask what they observed, with concrete situations. Split it into items in the
**configured categories** — they are listed in the `ingest` tool
description (e.g. `stop`, `start`, `continue`). Each item should be
specific, about behavior, and phrased so it can be shared with the person.

Point out items without a concrete example, and items that repeat a
previous feedback.

## 3. Store it

When the leader approves, call `ingest` with:
- `memory_type: "feedback"`, `about_person`
- `content`: the feedback as discussed, in the leader's words
- `feedback_items`: the items, each with its `category`
- `feedback_from`: who gave it, when it came from someone else (a peer, a
  report of the person)
- `topics` reusing the words already used for this person (see the recall)
- `tasks` for agreed follow-ups, with `owner` and `about_person`
- `occurred_at` (`YYYY-MM-DD`) when the feedback was given on another day

Call `list_people` first if anyone new is mentioned, to reuse stored names.

## 4. Output

The feedback, in the configured format, ready to be pasted into the
organization's system (e.g. Qulture). Answer in the user's language
(default: Portuguese).
