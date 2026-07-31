---
description: Triage open seeds issues — assign the canonical triage labels (needs-triage/needs-info/ready-for-agent/ready-for-human/wontfix)
argument-hint: "[issue-id]"
---

# Seeds Triage

Walk the open issue backlog in seeds (`sd`) and assign the canonical triage labels
defined in `docs/agents/triage-labels.md`. Every open issue should end up with exactly
one of the five labels.

Optional target: `${1:-}` (a single issue id like `dagmar-80dd`). If empty, triage the
whole open backlog.

## Canonical labels (authority: `docs/agents/triage-labels.md`)

| Label | Apply when |
|-------|------------|
| `needs-info` | Specification incomplete — missing repro, acceptance criteria, or context; blocked on the reporter. |
| `ready-for-agent` | Fully specified and well-scoped; implementation is mechanical/clear enough for an AFK agent. |
| `ready-for-human` | Requires human judgement or decision (architecture, policy, UX); deliberately human implementation. |
| `wontfix` | Out of scope, duplicate, or will not be actioned. |
| `needs-triage` | Genuinely unclear — maintainer must evaluate by hand. (Default only when no other label fits.) |

Do **not** remove wayfinder labels (`wayfinder:map`, `wayfinder:<type>`) — they
coexist with the triage label. Apply exactly one triage label per issue.

## Steps

1. **Gather the queue (use context-mode tools for any parsing).**
   - If `$1` is set: `sd show $1`.
   - Else: `sd list` (and `sd ready` / `sd blocked` for context). Identify open issues
     that carry **none** of the five triage labels — those are the queue. Re-triaging an
     already-labelled issue is allowed only when `$1` targets it explicitly.

2. **Read each issue.** `sd show <id>` — read title, description, type, priority,
   dependencies (`sd blocked`), and existing labels. Compare against the criteria
   above. Check `sd search "<related terms>"` for duplicates when `wontfix` is likely.

3. **Apply the label.**
   - **Clear-cut cases:** assign directly with
     `sd label add <id> <label>` (or `sd update <id> --add-label <label>`). Record the
     reasoning in one line.
   - **Ambiguous cases:** do **not** guess. Either ask the user via `ask_user_question`
     (batch all ambiguous ids into one question with one option per candidate label),
     or leave the issue on `needs-triage` with a one-line note on what is unclear.

4. **Side effects while triaging (only if the evidence is immediate):**
   - Missing info → `needs-info`; optionally note what is missing.
   - Duplicate → `wontfix`; link the canonical id in the description if useful.
   - Dependency wiring found missing → `sd block <id> --by <blocker-id>` / `sd dep add`.

5. **Commit.** Once labels are applied, run `sd sync` (validates, stages, commits
   `.seeds/`). Show the user what will be committed first if more than a handful of
   issues changed.

## Reply to the user

Reply **in German** (project convention). Keep it compact:

- How many issues were in the queue and how many were labelled.
- A small table or list: `id → label → one-line reason` (especially for anything not
  `ready-for-agent`).
- Any items left on `needs-triage` and why, plus any duplicates/missing-deps found.
- Offer to file follow-up seeds for surfaced gaps if relevant.

Do **not** produce a Markdown report file — the triage decisions are their own audit
trail (git-native via `sd sync`). The chat summary is enough.
