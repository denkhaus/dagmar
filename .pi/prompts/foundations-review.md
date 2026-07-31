---
description: Review ADRs, CONTEXT.md and domain docs for inconsistencies, undefined terms, and gaps; write findings to docs/review/
argument-hint: "[scope-label]"
---

# Foundations Review

Perform a repeatable design-doc review of this project's architecture foundations
(`CONTEXT.md`, `docs/adr/`, `docs/agents/*`). Reproduce the exact review shape used in
`docs/review/2026-07-31-foundations-review.md`.

Scope label for the output filename: `${1:-foundations}`

## Steps

1. **Prime context.** Note the current date from the system context; it is used in the
   output filename. Run `sd list` to learn which findings are *already tracked* so the
   review does not re-surface them as new.

2. **Gather (use context-mode tools — never raw `cat`/`grep` for analysis).**
   Read fully (these are small, must be read verbatim to compare wording):
   - `CONTEXT.md`
   - every file in `docs/adr/`
   - every file in `docs/agents/`
   Use `ctx_batch_execute` for derived facts: `go.mod`, Go source inventory
   (`find . -name '*.go'`), `sd list`, `ml status`, and a
   `grep` for cross-cutting terms (e.g. `hybrid`, `wayfinder`, `autonomy`,
   `credential`, `secret`) to find references without definitions.

3. **Analyse for four classes of issues.** For each, cite the exact file + section/line.
   - **Inconsistencies / contradictions** — internal clashes (e.g. a cardinality stated
     two different ways, a count label that does not match its members).
   - **Undefined / under-defined terms** — a term used as if established but missing
     from the glossary; overlapping concepts with no unifying definition; a referenced
     path/format/schema never pinned.
   - **Referenced-but-missing ADRs** — an architectural decision *assumed* by an
     existing ADR or by `CONTEXT.md` but never recorded (e.g. an execution topology
     named like "Hybrid-C" with no own ADR; credentials/secrets; autonomy policy;
     concurrency/scheduling).
   - **Housekeeping** — missing entry-point docs, duplicated tables/sections, missing
     relationship lines, undocumented field→primitive bridges, coexisting taxonomies.

4. **Tag every finding:** `[FIX]` (internal contradiction, fix now), `[GAP]`
   (referenced but not yet defined/decided, needs ADR/glossary entry), or `[HOUSE]`
   (documentation structure).

5. **Map to existing seeds.** For each finding, note if it is already covered by an
   open seed (from `sd list`), so only *newly surfaced* items count as new.

## Output

Write the report to:

```
docs/review/<YYYY-MM-DD>-${1:-foundations}-review.md
```

Use the **same structure** as `docs/review/2026-07-31-foundations-review.md`:

- Top scope line + tag legend (`[FIX]` / `[GAP]` / `[HOUSE]`).
- **A. Inconsistencies / contradictions**
- **B. Undefined / under-defined terms**
- **C. Referenced-but-missing ADRs**
- **D. Documentation structure / housekeeping**
- **E. Implementation maturity** (optional context, not a defect)
- **Suggested next ADRs** (priority order)
- **Already tracked in seeds** (table) + a "newly surfaced" list

Persisted writing (the report file) is **English**. Cite findings as
`file → section` or `file L##`. Be precise and specific; no generic advice.

## Reply to the user

Reply **in German** (project convention: spoken language German, persisted writing
English). Keep it under ~200 words: file path, the 3–5 most important findings as a
short bulleted summary, and offer to file the highest-value new findings as seeds.
