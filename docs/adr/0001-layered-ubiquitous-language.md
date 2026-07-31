# ADR-0001: Layered ubiquitous language (Tier A / B / C)

- **Status:** decided
- **Date:** 2026-07-31
- **Resolved in:** seeds dagmar-4271

## Context

dagmar builds directly on Dagger primitives (`LLM`, `Env`, `CodeWorkspace`,
`checkable`, `Loop`, `TokenUsage`) and binds the os-eco services (seeds, mulch,
canopy) at runtime. The central risk for a shared vocabulary is **double-naming** —
re-coining terms for concepts Dagger or os-eco already own (e.g. inventing a "sandbox"
type that is really the Dagger engine, or a "prompt" type that is really a canopy
composition).

## Decision

dagmar's ubiquitous language is split into three tiers:

- **Tier A — Dagger.** Reused by reference; names are never re-coined.
- **Tier B — os-eco.** Consumed behind adapter ports; os-eco tool names appear only in
  adapter implementations.
- **Tier C — dagmar core.** The only tier where terms are coined.

**Rule:** never coin a Tier-C term for something already named in Tier A or B. If a
concept is Dagger's, reference it; if it is an os-eco service, adapt it behind a port.

## Alternatives considered

- **Flat vocabulary** (one pool of terms, ownership unmarked). Rejected — duplicates
  Dagger/os-eco concepts and drifts from upstream terminology.
- **Single "core" tier with borrow-aliases** (e.g. `Sandbox` aliased to the Dagger
  engine). Rejected — aliases blur ownership and invite the very duplication this ADR
  prevents. Where dagmar adds semantics on top of a Tier-A concept, the Tier-C term is
  a distinct thing that *projects onto* the Tier-A one (e.g. Workspace → CodeWorkspace,
  Prompt spec → canopy composition).

## Consequences

- Every glossary term is tagged with its tier; later issues and code must use Tier-A/B
  names for those concepts and coin new terms only in Tier C.
- `CONTEXT.md` is the authority; `docs/agents/domain.md` tells skill consumers to use
  it and flag drift.
