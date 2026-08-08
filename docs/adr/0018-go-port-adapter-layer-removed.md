# ADR-0018: Go port/adapter layer removed — Project Hook Services are Dagger functions

- **Status:** decided
- **Date:** 2026-08-09
- **Resolved in:** seed dagmar-624c (closed) + this ADR
- **Evidence:** grilling session 2026-08-08; supersedes ADR-0001 §"Tier B", ADR-0005 (os-eco
  adapter ports), ADR-0010 §3/§5/§6/§8 (Go port layer); builds on ADR-0017 (Unified Project
  Hooks).
- **Supersedes:** ADR-0001 Tier-B port discipline, ADR-0005 (cross-store merge via os-eco
  ports), ADR-0010 §3 (os-eco ports in layering), §5 (OsEcoBinding constructor seam), §6
  (mockgen for os-eco ports), §8 (OsEcoConfig env-override).

## Context

ADR-0017 established that **all Project Hooks are Dagger module functions**, not YAML or
bash. This raised a follow-up question (seed dagmar-624c): if the LLM-Tool hooks
(dagmar-issues, dagmar-memory, dagmar-prompt) are native Dagger functions exposed via
`Env.WithMainModule()`, does dagmar still need a Go port/adapter layer (IssueTracker, Memory,
Prompts interfaces) to abstract them?

The Go port layer (ADR-0001 "Tier B", ADR-0005, ADR-0010 §3) was designed before the
Dagger-native tools pattern was confirmed. It created three Go interfaces, adapter stubs, and
binding types (OsEcoBinding, OsEcoConfig) that duplicated what Dagger's own introspection and
module system already provide.

## Decision

### D1 — Go-Port-Layer removed

The three Go interfaces (`IssueTracker`, `Memory`, `Prompts`), their adapters
(`adapters/oseco/`), binding types (`OsEcoBinding`, `OsEcoConfig`), and generated mocks are
**deleted**. The LLM-Tool hooks are registered natively as Dagger tools via
`Env.WithMainModule()`; prompt composition reads `.canopy/` directly from mounted source. The
abstraction boundary for a Project Hook Service is the **Dagger function name**, not a Go
interface.

### D2 — Tracer/Span retained

`Tracer` and `Span` are **observability** interfaces, not Project Hook Services. They survive
in `ports/` (moved to `ports/tracer.go`). The default implementation (`adapters/otel/`, currently a placeholder) will wrap
Dagger's built-in OpenTelemetry; Langfuse remains a deferred opt-in behind this port.

### D3 — Programmatic hooks untouched

`dagmar-bootstrap` and `dagmar-gate` are programmatic hooks called by the controller via
`dagger call -m <ref>`. They never had a Go port and need none — the controller calls the
function directly.

### D4 — Conformance via Dagger introspection

dagmar verifies at runtime via `Module.Functions()` that the required hook functions with
expected parameters exist. No shared Go interface package mediates this. Conformance is
introspection-based (detailed in ADR-0019).

### D5 — Terminology: "os-eco" → "Project Hook Services"

The term "os-eco" (short for "open-source ecosystem backing services") is replaced by
**"Project Hook Services"** in all code and documentation. The concept survives — dagmar
consumes seeds (issues), mulch (memory), and canopy (prompts) at runtime — but they are defined
as **hook functions in the Project module**, not as Go ports in dagmar's platform code.

### D6 — Dagger constructor simplified

The `New()` constructor drops the `seeds`, `mulch`, `canopy` parameters and the `OsEcoBinding`
struct. Only the `project` parameter remains. The `Dagmar` struct retains only the `Project`
field. Project Hook Services are not bound at construction time — they live in the Project
module and are discovered via introspection.

## Consequences

- **Simpler platform code:** three interfaces, three adapters, two binding types, and their
  mocks are gone. The `ports/` package shrinks to one interface (Tracer).
- **No mock churn for hooks:** LLM-Tool hooks are tested via real Dagger module calls, not Go
  interface mocks.
- **Tier B redefined:** "Tier B" (ADR-0001) no longer means "Go adapter ports." It means
  "Project Hook Services — Dagger functions in the Project module, discovered by name and
  verified by introspection." Tier A (Dagger primitives) is unchanged.
- **ADR-0019 scopes the follow-up:** function signatures for the five Project Hooks and the
  introspection conformance check.
