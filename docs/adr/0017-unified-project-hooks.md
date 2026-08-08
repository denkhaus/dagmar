# ADR-0017: Unified Project Hooks — everything is Dagger code

- **Status:** decided
- **Date:** 2026-08-08
- **Resolved in:** seed dagmar-e8f3 (spike) + this ADR
- **Evidence:** grilling session 2026-08-08; builds on ADR-0003 (ProjectManifest), ADR-0009 §2
  (gate conformance), ADR-0012 §4 (always-Dagger gate-family), ADR-0014 (module boundary),
  ADR-0011 (hermeticity).

## Context

dagmar's Project-conformance surface grew two mechanisms for the same concept — "entry points a
Project exposes to dagmar":

1. **Manifest hooks** (ADR-0003): `checkables:` in `.dagmar/project.yaml` — declarative YAML
   entries parsed by dagmar-gate at runtime and dispatched generically as bash commands.
2. **Module hooks** (ADR-0009 §2 / ADR-0012 §4): `dagmar-bootstrap` and `dagmar-gate` — Go
   functions in the Project's Dagger module, called by convention.

This split was never explicitly decided; it emerged because checkables predated the module-hook
pattern. It creates friction: checkables are limited to parameterless bash commands with
exit-code semantics, while module hooks can express arbitrary Dagger-Graph construction, typed
parameters, and error handling. A Project that needs a checkable with aggregation logic, API
calls, or conditional flows cannot express it in a YAML `command:` line — it must work around
the manifest.

The spike `dagmar-e8f3` asked whether Project Hook Service tool-wrapping (sd/ml/cn) should be data-driven
(manifest-declared bash commands) or code (module functions). The answer surfaced the deeper
question: why have two mechanisms at all?

## Decision

### 1. All Project Hooks are Dagger module functions

There is one mechanism: **Project Hooks are Go functions in the Project's Dagger module** (the
`.dagmar/` conformance module, ADR-0014). dagmar calls them by convention (function name). The
Project implements them in Go — its native module language. No YAML-declared bash hooks.

This follows the **"everything is Dagger code"** paradigm: a Project expresses its conformance
contract as code, not as data. Code handles arbitrary complexity — bash commands, multi-step
flows, API calls, typed I/O, proper error handling — uniformly.

### 2. Project Hook registry (convention)

Project Hooks fall into two categories by **caller**:

**Programmatic hooks** — called by dagmar's controller/infrastructure, not exposed to the LLM:

| Hook | Function name | Purpose | ADR |
|------|---------------|---------|-----|
| Bootstrap | `dagmar-bootstrap` | Roll out the project's toolchain into the gate container | ADR-0009 §2 / ADR-0012 §4 |
| Gate | `dagmar-gate` | Run the project's self-verification (build/test/lint/scan) | ADR-0009 §2 / ADR-0012 §4 |

**LLM-Tool hooks** — exposed as Dagger tools on the agent's `Env`, called by the LLM during the
loop:

| Hook | Function name | Purpose | ADR |
|------|---------------|---------|-----|
| Issues | `dagmar-issues` | Read/create issues (the project's Task handle) | This ADR |
| Memory | `dagmar-memory` | Read/write project expertise | This ADR |
| Prompt | `dagmar-prompt` | Compose the agent's prompt | This ADR |

When an LLM agent operates on a Project (Phase 2+), **all three LLM-Tool hooks are mandatory** —
dagmar registers them on the `Env` unconditionally. A Project that does not use a capability
implements it as a **noop** (e.g. `dagmar-memory` returning an empty string) rather than omitting
the function. This guarantees the LLM's tool-surface is predictable: the agent always has the same
tools available, regardless of Project. A Project that genuinely lacks issue tracking provides a
noop, not a missing function.

All hooks are vendor-agnostic: the function name is the contract, not the implementation. A
Project using Linear instead of seeds implements `dagmar-issues` with the Linear API; dagmar
calls `dagmar-issues` either way.

### 3. Checkables move into `dagmar-gate` (manifest checkables superseded)

> **Reversal of review-11 GAP-3:** ADR-0012 §4 explicitly rejected "`dagmar-gate` IS the
> checkable" because it would make the manifest `checkables:` section a *second source*. ADR-0017
> removes the manifest section entirely — with no manifest, there is no second source, and the
> rejection's rationale is moot. The gate function owns both definition and execution because
> there is no longer a separate declaration to conflict with.

The `checkables:` section of `.dagmar/project.yaml` (ADR-0003) is **superseded**. Gate logic —
what to check, how to check it, in what order, with what error handling — lives entirely in the
`dagmar-gate` Go function. A Project defines its checkables as code inside `dagmar-gate`, not as
YAML data parsed at runtime.

This lifts the "parameterless bash command with exit code" constraint: a Project's gate can now
express multi-step flows, coverage aggregation, conditional checks, API-verified thresholds —
whatever the Project needs. The exit-code contract (gate returns success/failure + output)
remains; the internal structure is the Project's code.

### 4. The ProjectManifest (`.dagmar/project.yaml`) is slimmed, not removed

With `checkables:` removed, the ProjectManifest (ADR-0003) loses its primary content but is
**retained as a metadata file**. It carries Project metadata dagmar needs at runtime — display
name, description, version, and future declarative configuration that does not fit as a function
parameter. It is no longer the home of conformance logic (the Dagger module is) or the
project-specific binding to Project Hook Services (the hooks are). The manifest is slimmed now; if it
proves to carry no load-bearing content over time, it can be removed then.

**Prompt composition** (ADR-0005) is unaffected: dagmar operational mixins (from dagmar's own
`.canopy/`) are composed with project-content prompts (from the project's `.canopy/`) — this
cross-store merge is dagmar-side Go logic, not manifest-declared. ~~The `dagmar-prompt` hook may
wrap or extend this; the exact relationship is deferred to ADR-0018.~~ — **ADR-0019 resolved:**
`dagmar-prompt` supplements (not replaces/wraps) the merge; it is a runtime LLM tool for on-demand
project prompt rendering, while the merge remains the controller's initial-prompt composer.

### 5. The `manifest` Go module: Checkables types deprecated, metadata types retained

The published shared library `github.com/denkhaus/dagmar/manifest` (ADR-0014 GAP-1) currently
exports `ProjectManifest`, `Checkable`, `ParseManifest`, and `validateWorkdir`. With checkables
moving into `dagmar-gate`, `Checkable`/`validateWorkdir` are **deprecated** and will be removed
when `dagmar-gate` is refactored to in-code checks (Phase 2). The library itself is **retained**
— it carries the manifest metadata types (the slimmed `ProjectManifest`) that the platform and
project modules share. The library's role narrows from "conformance contract types" to "manifest
metadata types". `ParseManifest` is retained but must be refactored in Phase 2 to stop requiring
checkables (its current `len(m.Checkables) == 0` guard is coupled to the deprecated
`Checkable`/`validateWorkdir` types).

### 6. LLM-Tool hooks are hermetic in the tool-surface sense (ADR-0011)

The LLM-Tool hooks (`dagmar-issues`, `dagmar-memory`, `dagmar-prompt`) operate on the project's
worktree
(`.seeds/`, `.mulch/`, `.canopy/` are directories in the mounted source). They are file
operations — read, write, edit local files. They do not *use* network within the hermetic loop.
Remote sync (`sd sync`, `ml sync`, `git push`) happens outside the loop, as a networked controller
action — exactly like `dagmar-bootstrap`/`dagmar-gate` having network to install tools.

Per ADR-0011 §2, "hermetic" is a **tool-surface constraint, not a network air-gap**. The LLM-Tool
hooks are hermetic because they do not include network-capable tools on the agent's `Env` and do
not invoke network operations — not because network is physically impossible (the ProbeNet
residual from ADR-0011 applies: a raw container exec always has outbound network in Dagger
v0.21.8). This is the same calculated residual risk ADR-0011 accepts for all hermetic agents.

### 7. Tier-B discipline preserved (ADR-0001)

Backing-service names (`sd`, `ml`, `cn`) appear **only** inside the Project's hook implementations
— never in dagmar's domain code. dagmar calls `dagmar-issues`/`dagmar-memory`/`dagmar-prompt`; what
CLI, library, or API backs them is the Project's choice. A Project using Linear instead of seeds
implements `dagmar-issues` differently; dagmar sees the same interface.

## Consequences

- **ADR-0003:** the `checkables:` section is superseded. The ProjectManifest retains only
  metadata. The conformance contract is the Dagger module, not the manifest.
- **ADR-0009 §2:** "manifest = what, dagmar-gate = how" is replaced by "dagmar-gate = what AND
  how." The gate function owns both the checkable definitions and their execution.
- **ADR-0012 §4:** the gate-family remains always-Dagger functions; the change is that gate logic
  is fully in code, not split between YAML (checkables) and code (dispatch).
- **ADR-0013 §5 D12:** the manifest-declared bash-command Project Hook Service binding mechanism
  (the five hermeticity rules, `issues_read`/`issues_write` tool names, feasibility gate) is
  **replaced** by ADR-0017's named-function approach. The LLM-Tool hooks are convention-named Dagger
  module functions, not manifest-declared commands wrapped into LLM tools by dagmar.
- **ADR-0014:** the `.dagmar/` project module grows three LLM-Tool hook
  functions (`dagmar-issues`/`dagmar-memory`/`dagmar-prompt`), mandatory when an LLM agent
  operates on the Project. The manifest library (manifest/) loses its primary types over time.
- **dagmar-gate refactoring:** the current gate.go reads `.dagmar/project.yaml` and dispatches
  checkables generically. Post-ADR, gate.go contains the checkable definitions directly as Go
  code. This is a Phase-2 implementation task (the current gate works; this ADR is the decision
  to change the pattern).
- **`dagmar-bootstrap`/`dagmar-gate` today:** continue working as-is. The refactoring to move
  checkables into code happens when Phase 2 begins (coder-loop vertical). No immediate code
  change is required by this ADR. Code comments and `.dagmar/project.yaml` that reference the old
  "manifest = what, gate = how" model are **intentionally stale** until the Phase-2 refactor.

### 8. Hook-vs-port relationship (ADR-0001 Tier B)

The Tier-B adapter ports (`IssueTracker`, `Memory`, `Prompts` in `.dagger/internal/ports/`) are
dagmar's **internal** domain interfaces — they define what dagmar's domain code calls. The project
hooks (`dagmar-issues`/`dagmar-memory`/`dagmar-prompt`) are the **external** conformance surface —
how the project implements the adapter. The adapter implementation in
the platform will call the project's hooks by module-ref, satisfying the
port. This preserves Tier-B discipline: dagmar's domain sees only the port; the adapter bridges
port → project hook. Backing-service CLI names (`sd`, `ml`, `cn`) appear only inside the
project's hook implementation, never in dagmar's adapter or domain code.

> **Asymmetry for `dagmar-prompt`** (resolved by ADR-0019): `dagmar-issues` and
> `dagmar-memory` are thin delegations (port → project hook). `dagmar-prompt` **supplements**
> ADR-0005's cross-store merge — it is a separate runtime tool the LLM calls to render additional
> project prompt content; the merge remains dagmar-side controller logic (pre-loop). The bridge
> model is settled for all three hooks.

**Signature specification.** ~~The exact Go signatures for the LLM-Tool hooks
(`dagmar-issues`/`dagmar-memory`/`dagmar-prompt`: inputs, outputs, error contract) are **deferred
to ADR-0018** (Project Hook contract).~~ — **Resolved by ADR-0019**: all five hook signatures are
specified, and the `dagmar-prompt` asymmetry is resolved (supplements the ADR-0005 merge).

## Alternatives considered

- **Data-driven manifest tools (original spike question).** Rejected — requires a template engine
  for arg substitution, a generic dispatch function, and a schema for operations. More complexity
  than three well-defined Go functions, for less type safety and no LLM-tool-quality advantage.
- **Keep two mechanisms (manifest hooks + module hooks).** Rejected — the split is unjustified,
  hard to explain, and limits Projects to bash commands for checkables. One mechanism is simpler.
- **MCP server for Project Hook Service tools.** Rejected — adds a runtime dependency, bypasses Dagger's native
  module function tool registration, and provides no benefit over convention-called functions.
- **Go-Wrapper per Project Hook Service command (hardcoded in dagmar).** Rejected — violates Tier-B discipline
  (ADR-0001): dagmar would know `sd`/`ml`/`cn` names. The hooks are Project-scope; dagmar calls
  the abstraction.
