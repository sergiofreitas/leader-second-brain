---
description: Record an observation about someone on your team
argument-hint: "[what you observed]"
---

# Record an observation

Capture what the leader observed, following the `second-brain` skill.

1. If the observation isn't in the arguments ($ARGUMENTS), ask what they
   observed and about whom.
2. Call `list_people` and reuse the stored names.
3. Extract the entities (about_person, persons, topics, tasks,
   relationships, feedback items when it is feedback) and call `ingest`
   with the leader's words as `content`.
4. Confirm in one or two lines what was stored, and ask if there is
   anything else.
