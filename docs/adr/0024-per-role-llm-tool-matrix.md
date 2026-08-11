# ADR-0024: Per-role LLM tool matrix

- **Status:** decided
- **Date:** 2026-08-11
- **Evidence:** resolves ADR-0011 §4 "Deferred: the exact per-Agent minimal tool-sets";
  builds on ADR-0011 (hermeticity), ADR-0017 (unified hooks), ADR-0019 D2 (hook signatures),
  ADR-0021 D7 (coder env), ADR-0023 D5 (pipeline roles).

## Context

ADR-0011 §4 explicitly deferred "the exact per-Agent minimal tool-sets (Agent CRD `tool-set`
values)" to a follow-up. Since then, four LLM roles have materialized (ADR-0023 D6: prompter,
coder, reviewer, adjudicator), each with distinct responsibilities — but the tool-surface each
role receives has never been pinned in a single place. This ADR closes that gap.

The problem is not theoretical: in the current implementation, the reviewer reuses the coder's
`code()` function (which sets `Writable: true`), giving the reviewer write access to the workspace
— contradicting ADR-0023 D5's read-only reviewer. Without an explicit matrix, these drifts go
unnoticed.

## Decision

### D1 — The tool matrix

Each LLM role receives a specific Env construction and tool-surface. All roles are hermetic
(ADR-0011): network-capable tools (`http`, `git` remote, raw `container`) are never on the Env.
The Privileged flag (ADR-0022) grants core API access (Directory, File) — this is accepted as the
Phase 2 interim position.

| Role | Privileged | Writable | dir I/O | dagmar-issues | dagmar-memory | dagmar-gate | Purpose |
|------|-----------|----------|---------|---------------|---------------|-------------|---------|
| **Prompter** | ✅ | ❌ | read | read, search | read, search | ❌ | Synthesize tailored prompts from project context |
| **Coder** | ✅ | ✅ | read+write | read, search | read, search | ✅ (self-verify) | Implement code changes; self-verify via gate |
| **Reviewer** | ✅ | ❌ | read | read, search | read, search | ❌ | Read code changes; approve or veto |
| **Adjudicator** | ✅ | ❌ | read | read, search | read, search | ❌ | Investigate gate↔reviewer disagreement; resolve or escalate |

**Justification per role:**

- **Prompter** — read-only (ADR-0023 D1): it reads source + hooks to synthesize a prompt, never
  modifies anything.
- **Coder** — writable (ADR-0021 D2): it modifies the workspace and saves the result. Gets
  `dagmar-gate` for in-loop self-verification (ADR-0011 §2 carve-out: named Dagger function ≠
  raw container tool). Gets `dagmar-issues`/`dagmar-memory` to ground its work in real project
  context.
- **Reviewer** — read-only (ADR-0023 D5): the reviewer reads the coder's diff and applies review
  criteria. It must NOT modify code (separation of concerns, ADR-0006 two-green model). Gets
  `dagmar-issues`/`dagmar-memory` to apply project-specific review guidance.
- **Adjudicator** — read-only (ADR-0023 D4): the adjudicator investigates a disagreement. It
  reads source + issues + memory to diagnose root cause, but modifies nothing. Its resolution
  is a verdict string, not a code change.

### D2 — dagmar-issues access scope

All four LLM roles get `dagmar-issues`. The access scope differs:

| Role | dagmar-issues actions |
|------|-----------------------|
| Prompter | read, search |
| Coder | read, search |
| Reviewer | read, search |
| Adjudicator | read, search |

**Write actions (create, update) are NOT available to any LLM role in Phase 2.** Issue creation
and status changes are controller-level decisions (the controller creates Sub-Runs, tracks
pipeline state, escalates to humans). The LLM may read issues to ground its work but must not
mutate the issue tracker — that is a control-plane concern, not a cognition concern.

This means the hook implementation exposes all four actions (per ADR-0019 D2), but the tool-set
policy on the Env (future `WithBlockedFunction` usage, or a wrapper) restricts the LLM to
read/search. Until Dagger's `WithBlockedFunction` is wired, the hook accepts all actions — the
meta-prompts instruct the LLM to use read/search only.

### D3 — dagmar-memory access scope

| Role | dagmar-memory actions |
|------|-----------------------|
| Prompter | read, search |
| Coder | read, search |
| Reviewer | read, search |
| Adjudicator | read, search |

**Write (record) is deferred.** The coder recording expertise mid-loop is a Phase 3 capability.
In Phase 2, expertise is read-only for all roles — the coder focuses on code changes, not on
updating the knowledge base.

### D4 — The reviewer needs its own function

The reviewer currently reuses `code()` (writable). Per this matrix, the reviewer needs a
read-only function. A `review()` function (or a `code()` variant with `Writable: false`) must be
added to the `.dagger` platform module. The controller dispatches `review` for reviewer Sub-Runs
and `code` for coder Sub-Runs.

This resolves the existing drift where the reviewer gets write access it should not have.

### D5 — Implementation sequence

1. **dagmar-issues + dagmar-memory hooks** (this ADR + ADR-0019 D2) — implemented in `.dagmar/`
   module, registered via `WithMainModule`.
2. **review() function** (D4) — new function on `.dagger/`, read-only Env.
3. **Controller wiring** — reviewer Sub-Runs dispatch `review` instead of `code`.
4. **Tool-set enforcement** — `WithBlockedFunction` to restrict write actions on issues/memory
   (Phase 3, when the API is stable).

## Consequences

- **ADR-0011 §4 "Deferred" is resolved.** The per-Agent tool-set is now pinned.
- **ADR-0021 D7 is refined.** The coder's Env is unchanged (Privileged+Writable+hooks); the
  reviewer gets a distinct, read-only Env.
- **ADR-0023 D5 is enforced.** The reviewer is read-only by construction (its own function),
  not just by convention.
- **dagmar-issues/memory write access** is deferred to Phase 3 — the hooks support write, but the
  LLM is instructed (meta-prompt) and later enforced (`WithBlockedFunction`) to use read/search.
- **The `.dagmar/` module grows two functions** (`dagmar-issues`, `dagmar-memory`), making the
  five-hook conformance contract (ADR-0017 §2, minus the removed `dagmar-prompt`) complete:
  `dagmar-bootstrap`, `dagmar-gate` (programmatic); `dagmar-issues`, `dagmar-memory` (LLM-Tool).
