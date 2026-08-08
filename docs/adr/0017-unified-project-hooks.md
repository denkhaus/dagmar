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

The spike `dagmar-e8f3` asked whether os-eco tool-wrapping (sd/ml/cn) should be data-driven
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

| Hook | Function name | Purpose | ADR |
|------|---------------|---------|-----|
| Bootstrap | `dagmar-bootstrap` | Roll out the project's toolchain into the gate container | ADR-0009 §2 / ADR-0012 §4 |
| Gate | `dagmar-gate` | Run the project's self-verification (build/test/lint/scan) | ADR-0009 §2 / ADR-0012 §4 |
| Issues | `dagmar-issues` | Read/create issues (the project's Task handle) | This ADR |
| Memory | `dagmar-memory` | Read/write project expertise | This ADR |
| Prompt | `dagmar-prompt` | Compose the agent's prompt | This ADR |

A Project must expose `dagmar-bootstrap` + `dagmar-gate` (required conformance, ADR-0009). The
os-eco hooks (`dagmar-issues`, `dagmar-memory`, `dagmar-prompt`) are **optional** — a Project
without them operates without those capabilities (no issue tracking, no memory recall, no prompt
composition). When present, dagmar uses them; when absent, dagmar degrades gracefully.

### 3. Checkables move into `dagmar-gate` (manifest checkables superseded)

The `checkables:` section of `.dagmar/project.yaml` (ADR-0003) is **superseded**. Gate logic —
what to check, how to check it, in what order, with what error handling — lives entirely in the
`dagmar-gate` Go function. A Project defines its checkables as code inside `dagmar-gate`, not as
YAML data parsed at runtime.

This lifts the "parameterless bash command with exit code" constraint: a Project's gate can now
express multi-step flows, coverage aggregation, conditional checks, API-verified thresholds —
whatever the Project needs. The exit-code contract (gate returns success/failure + output)
remains; the internal structure is the Project's code.

### 4. The ProjectManifest (`.dagmar/project.yaml`) is simplified

With `checkables:` removed, the ProjectManifest (ADR-0003) carries only **metadata** if it
carries anything at all — os-eco store paths, repo/flow metadata. It is no longer the home of
conformance content; the Dagger module is. The manifest may persist as a thin metadata file, or
be absorbed entirely into module function parameters. This ADR does not mandate its removal but
removes its last load-bearing content (`checkables:`).

### 5. The `manifest` Go module loses its Checkables types

The published shared library `github.com/denkhaus/dagmar/manifest` (ADR-0014 GAP-1) currently
exports `ProjectManifest`, `Checkable`, `ParseManifest`, and `validateWorkdir`. With checkables
moving into `dagmar-gate`, these types lose their consumer. The module is not deleted in this
ADR (it may carry future manifest metadata types), but `Checkable`/`ParseManifest`/`validateWorkdir`
are deprecated and will be removed when `dagmar-gate` is refactored to in-code checks.

### 6. os-eco hooks are hermetic (file operations on the worktree)

`dagmar-issues`, `dagmar-memory`, `dagmar-prompt` operate on the project's worktree
(`.seeds/`, `.mulch/`, `.canopy/` are directories in the mounted source). They are file
operations — read, write, edit local files. No network within the hermetic loop. Remote sync
(`sd sync`, `ml sync`, `git push`) happens outside the loop, as a networked controller action —
exactly like `dagmar-bootstrap`/`dagmar-gate` having network to install tools. This is consistent
with ADR-0011 §9 ("os-eco is read-local inside the hermetic loop; sync is networked outside it").

### 7. Tier-B discipline preserved (ADR-0001)

os-eco CLI names (`sd`, `ml`, `cn`) appear **only** inside the Project's hook implementations —
never in dagmar's domain code. dagmar calls `dagmar-issues`/`dagmar-memory`/`dagmar-prompt`; what
CLI or library backs them is the Project's choice. A Project using Linear instead of seeds
implements `dagmar-issues` differently; dagmar sees the same interface.

## Consequences

- **ADR-0003:** the `checkables:` section is superseded. The ProjectManifest retains only
  metadata. The conformance contract is the Dagger module, not the manifest.
- **ADR-0009 §2:** "manifest = what, dagmar-gate = how" is replaced by "dagmar-gate = what AND
  how." The gate function owns both the checkable definitions and their execution.
- **ADR-0012 §4:** the gate-family remains always-Dagger functions; the change is that gate logic
  is fully in code, not split between YAML (checkables) and code (dispatch).
- **ADR-0014:** the `.dagmar/` project module grows three optional functions
  (`dagmar-issues`/`dagmar-memory`/`dagmar-prompt`). The manifest library (manifest/) loses its
  primary types over time.
- **dagmar-gate refactoring:** the current gate.go reads `.dagmar/project.yaml` and dispatches
  checkables generically. Post-ADR, gate.go contains the checkable definitions directly as Go
  code. This is a Phase-2 implementation task (the current gate works; this ADR is the decision
  to change the pattern).
- **dogmar-bootstrap/`dagmar-gate` today:** continue working as-is. The refactoring to move
  checkables into code happens when Phase 2 begins (coder-loop vertical). No immediate code
  change is required by this ADR.

## Alternatives considered

- **Data-driven manifest tools (original spike question).** Rejected — requires a template engine
  for arg substitution, a generic dispatch function, and a schema for operations. More complexity
  than three well-defined Go functions, for less type safety and no LLM-tool-quality advantage.
- **Keep two mechanisms (manifest hooks + module hooks).** Rejected — the split is unjustified,
  hard to explain, and limits Projects to bash commands for checkables. One mechanism is simpler.
- **MCP server for os-eco tools.** Rejected — adds a runtime dependency, bypasses Dagger's native
  module function tool registration, and provides no benefit over convention-called functions.
- **Go-Wrapper per os-eco command (hardcoded in dagmar).** Rejected — violates Tier-B discipline
  (ADR-0001): dagmar would know `sd`/`ml`/`cn` names. The hooks are Project-scope; dagmar calls
  the abstraction.
