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

| Role | Privileged | Writable | dir I/O | Container | dagmar-issues | dagmar-memory | dagmar-gate | dagmar-bootstrap | Purpose |
|------|-----------|----------|---------|----------|---------------|---------------|-------------|-----------------|---------|
| **Prompter** | ✅ | ❌ | read | ❌ blocked | read, search | read, search | ❌ blocked | ❌ blocked | Synthesize tailored prompts from project context |
| **Coder** | ✅ | ✅ | read+write | ✅ (go build) | read, search | read, search | ❌ blocked | ❌ blocked | Implement code changes |
| **Reviewer** | ✅ | ❌ | read | ❌ blocked | read, search | read, search | ❌ blocked | ❌ blocked | Read code changes; approve or veto |
| **Adjudicator** | ✅ | ❌ | read | ❌ blocked | read, search | read, search | ❌ blocked | ❌ blocked | Investigate gate↔reviewer disagreement |

**Justification per role:**

- **Prompter** — read-only (ADR-0023 D1): it reads source + hooks to synthesize a prompt, never
  modifies anything.
- **Coder** — writable (ADR-0021 D2): it modifies the workspace and saves the result. Gets
  `dagmar-issues`/`dagmar-memory` to ground its work in real project context. Keeps Container
  for `go build` verification (build only — no test/lint, per the coder meta-prompt). Gate
  feedback flows through the revise loop, not in-loop self-verification (D5).
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

### D5 — Tool-surface enforcement via WithBlockedFunction

Dagger v0.21.8 exposes `LLM.WithBlockedFunction(typeName, function)` — a blacklist API that
removes specific functions from the LLM's tool surface after `WithMainModule` has registered
them all. There is **no whitelist API**: `WithMainModule` always registers every function, and
undesired ones must be individually blocked.

A centralized `blockForRole(llm, role)` function in `.dagger/internal/app/tools.go` applies
the per-role blocks:

```go
func blockForRole(llm *dagger.LLM, role Role) *dagger.LLM {
    // Every LLM role: block infrastructure + gate functions.
    llm = llm.WithBlockedFunction("Dagmar", "dagmarBootstrap").
        WithBlockedFunction("Dagmar", "dagmarGate")

    // Read-only roles: block Container.withExec (no network exec).
    if role != RoleCoder {
        llm = llm.WithBlockedFunction("Container", "withExec")
    }
    return llm
}
```

**Why `dagmar-gate` is blocked for all LLM roles:** ADR-0023 D5 defines the gate as a
deterministic post-loop pipeline step (step 3), dispatched by the controller — not an in-loop
LLM tool. The controller is the authority over gate results (termination message → controller
parses GateResult JSON). An in-loop gate call would (a) cost LLM API calls for build/test
execution, (b) be skippable by the LLM, and (c) duplicate the deterministic gate step.
Coder feedback on gate-RED flows through the revise loop (new coder round with gate result),
not through in-loop self-verification.

**Why `dagmar-bootstrap` is blocked for all LLM roles:** bootstrap is infrastructure setup
(mise toolchain rollout) — a deterministic step invoked by the controller or CI before the
Run starts. It is never an LLM concern.

**Why `Container.withExec` is blocked for read-only roles:** the prompter, reviewer, and
adjudicator only read files via the core Dagger API (Directory, File). They must not execute
containers — `Container.WithExec()` always has outbound network (ADR-0011 ProbeNet). The coder
keeps Container because it needs `go build` for compilation verification.

This supersedes ADR-0011 §2's "carve-out" (which assumed in-loop gate calls) and ADR-0021 D6's
"env.Checks() self-verification" (which assumed the LLM runs the gate in-loop). The gate is
now exclusively a deterministic controller step.

### D6 — Implementation sequence

1. **dagmar-issues + dagmar-memory hooks** (this ADR + ADR-0019 D2) — ✅ implemented.
2. **blockForRole** (D5) — ✅ implemented in `.dagger/internal/app/tools.go`.
3. **review() function** (D4) — new function on `.dagger/`, read-only Env + RoleReviewer blocks.
4. **Controller wiring** — reviewer Sub-Runs dispatch `review` instead of `code`.
5. **dagmar-issues/memory write restrictions** — block create/update/write actions via
   `WithBlockedFunction` on sub-functions (Phase 3).

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
