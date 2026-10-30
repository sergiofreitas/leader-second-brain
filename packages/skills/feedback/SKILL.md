---
name: feedback
description: |
  Prepares and records a feedback in the organization's method (the
  playbook): its checks before giving one, its categories in order (e.g.
  stop, continue, start), how each item is phrased (situation, behavior,
  impact, change signal) and how it is recorded. Uses the Second Brain to
  ground each item in recorded observations and to avoid repeating points
  already given. Use when the user wants to "give feedback", "montar
  feedback", "registrar um feedback", or mentions "stop/start/continue",
  "parar/continuar/começar".
---

# Feedback

## 1. The method

Call `get_playbook(rite: "feedback")`. It returns the rite (purpose,
duration, agenda, how it is recorded) and `feedback`: the `categories` in
the order they are given, `items_per_category` (at most), `item_template`
(how each item is phrased) and the `checks` to go through first.

## 2. Checks first

Go through each of the playbook's checks with the leader before building
anything. If one fails, stop and say what it means: a point that would be a
surprise is first raised in a 1:1; serious misconduct is not a feedback
(it goes to HR); a point based only on transcripts needs other evidence.
Don't build the feedback around a failed check.

## 3. Context

Call `recall(person_name, context: "feedback", time_range: "all")` — every
feedback ever given — and look at:
- feedbacks already given (items and dates): don't repeat a point unless it
  is a follow-up, and then say so
- observations that support or contradict what the leader wants to say,
  with their dates and sources
- pending tasks about this person, and overdue ones

## 4. Build the items with the leader

Ask what they observed, with concrete situations. Then, **in the order of
the categories**, at most `items_per_category` items each:
- each item has a situation, an observable behavior (no judgment), its
  impact, a question or request, and a change signal — how both will know
  it changed
- phrase it with the playbook's `item_template`, so it can be shared with
  the person

Point out items without a concrete example, items based on what someone
else said (they need the leader's own evidence), and items that repeat a
previous feedback.

Prepare the conversation in the rite's agenda, with the time of each block.
Evidence with date and source stays in the leader's notes, not in the
script.

## 5. Store it

When the leader approves, call `ingest` with:
- `memory_type: "feedback"`, `about_person`
- `content`: the feedback as discussed, in the leader's words
- `feedback_items`: each with `category`, `content` (the phrased item) and
  `situation`, `behavior`, `impact`, `change_signal`
- `feedback_from`: who gave it, when it came from someone else (a peer, a
  report of the person)
- `topics` reusing the words already used for this person
- `tasks` for agreed follow-ups, with `owner`, `about_person` and
  `due_date`
- `occurred_at` (`YYYY-MM-DD`) when the feedback was given on another day;
  `source_url` for the meeting notes, if any

Call `list_people` first if anyone new is mentioned, to reuse stored names.

## 6. Output

The feedback in the categories' order, ready to be recorded as the rite's
`record` says (e.g. in Qulture, separately from the 1:1). When the rite has
a `record_template`, fill it section by section, in its order and headings.
Answer in the user's language (default: Portuguese).
