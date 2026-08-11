# ADR-0025: Structured agent output via WithJSONValueOutput

- **Status:** decided
- **Date:** 2026-08-11
- **Evidence:** resolves the >2000-byte termination-log limitation; builds on ADR-0021
  (loop wrapping), ADR-0024 (per-role tools). Empirical reference: greetings-api
  `review.go` (mx-e05f90) uses `WithStringOutput` + `env.Output("review").AsString(ctx)`.

## Context

Agent outputs (reviewer verdicts, adjudicator decisions, future structured results) must flow
from the LLM loop back to the controller. Two existing mechanisms have limitations:

1. **Termination message** (`/dev/termination-log`) — hard 2048-byte limit (K8s). A reviewer
   rationale or adjudicator analysis can exceed this.
2. **LastReply (string)** — unstructured free text. The controller must parse natural language
   to extract decisions. Fragile, no schema enforcement.

## Decision

### D1 — WithJSONValueOutput is the standard output mechanism

All agent roles that return structured data use `env.WithJSONValueOutput(name, description)`
to declare a JSON output binding. The agent fills it via its Save tool during the Loop. After
the Loop, the calling code reads it:

```go
rawJSON := env.Output("verdict").AsJSONValue().AsString(ctx)
```

No byte limit (unlike termination-log). Structured JSON (unlike LastReply). The output flows
through the Dagger function return value, not through pod-level tricks.

The coder remains the exception: it returns a `*dagger.Directory` (modified workspace) via
`WithDirectoryOutput`. That is already a structured output — no JSON needed.

### D2 — Retry on missing/invalid output

Agents may not set the output binding (forgot to call Save), or may produce invalid JSON.
`extractJSONOutput` in `output.go` handles this:

1. After the Loop, read the output binding.
2. If missing/empty/null → retry: re-prompt the agent with a correction prompt, re-run Loop.
3. If present but doesn't unmarshal into the target struct → same retry.
4. After `maxOutputRetries` (2) attempts without success → error (the Run fails).

The correction prompt tells the agent exactly what schema is expected and that it must use the
Save tool. The retry happens in the same LLM context (the conversation history is preserved).

### D3 — Future: DSL-ready structured outputs

Using JSON outputs exclusively (not free-text strings) means controller flows can eventually be
expressed as a DSL that references output fields by path (`$.verdict.decision == "approve"`),
not as hardcoded Go string-matching. This ADR does not specify the DSL — it enables it by
ensuring every agent output is structured JSON.

## Consequences

- **Reviewer** outputs `ReviewVerdict{Decision, Rationale, Issues}` as JSON.
- **Adjudicator** will output a structured verdict (Phase 3) as JSON.
- **Prompter** remains a string output (its prompt text is consumed by the next step, not by
  the controller for decisions — it doesn't need structured JSON).
- **Coder** returns `*dagger.Directory` (unchanged).
- **Termination-log** is used ONLY by the deterministic gate (GateResult JSON), not by LLM
  agents. The gate runs as a shell command, not as a Dagger function with output bindings.
