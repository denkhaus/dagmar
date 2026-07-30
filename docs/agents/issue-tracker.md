# Issue tracker: seeds (`sd`)

Issues for this repo live in **seeds** — a git-native CLI tracker whose store is `.seeds/` (JSONL, committed). GitHub (`denkhaus/dagmar`) holds only the code; issue tracking moved here. Use the `sd` CLI for all operations.

## Conventions

- **Create an issue**: `sd create --title "..." --type task|feature|bug|epic --priority 0-4 --labels "a,b" --description "..."` (multi-line via `"$(cat file.md)"`).
- **Read an issue**: `sd show <id>` (ids look like `dagmar-80dd`).
- **List issues**: `sd list`; the open + unblocked frontier = `sd ready`; blocked = `sd blocked`; full-text = `sd search "<q>"`.
- **Update / label / assign**: `sd update <id> [--status open|in_progress|closed] [--add-label ...] [--assignee <name>] [--description ...]`; labels also via `sd label add <id> <label>`.
- **Dependencies / blocking**: `sd block <id> --by <blocker-id>` (native, visible in `sd ready` / `sd blocked`); or `sd dep add <id> <depends-on>`.
- **Close**: `sd close <id>` (or `sd update <id> --status closed`).
- **Commit changes**: `sd sync` (validates, stages, commits `.seeds/`). Do NOT infer the tracker from `git remote` — the tracker is sd, not GitHub.
- **JSON output**: append `--json` (or `--format json`) to any command for scripting.

## Pull requests as a triage surface

**PRs as a request surface: no.** GitHub PRs are the code-review surface, not a triage queue. Triage operates on sd issues only.

## When a skill says "publish to the issue tracker"

Run `sd create ...`.

## When a skill says "fetch the relevant ticket"

Run `sd show <id>`.

## Wayfinding operations

Used by `/wayfinder`. The **map** is a single sd issue of type `epic` (`dagmar-80dd`); tickets are sd issues whose body begins with `Part of dagmar-80dd`.

- **Map**: `sd create --type epic --labels wayfinder:map ...` — the canonical index (Destination / Notes / Decisions-so-far / Not yet specified / Out of scope).
- **Child ticket**: an sd issue with `Part of dagmar-80dd` at the top of its body and a `wayfinder:<type>` label (`research` / `prototype` / `grilling` / `task`). Labels are freeform strings in sd.
- **Blocking**: native — `sd block <child> --by <blocker-id>`. A ticket is unblocked when every blocker is closed; the live frontier is `sd ready`.
- **Frontier query**: `sd ready`, then drop any with an assignee; first in list order wins.
- **Claim**: `sd update <id> --assignee <you>` — the session's first write.
- **Resolve**: record the decision in **mulch** (`ml record <domain> --type decision --evidence-seeds <id> --title "..." --rationale "..."`), append a one-line gist to the map body (`sd update dagmar-80dd --description ...`), then `sd close <id>`. sd issues have no comment thread — the durable answer lives in mulch, linked back to the sd id via `--evidence-seeds`.
