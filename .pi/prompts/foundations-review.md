---
description: Review ADRs, CONTEXT.md and domain docs for inconsistencies, undefined terms, and gaps; write findings to docs/review/
argument-hint: "[scope-label]"
---

# Foundations Review

Perform a repeatable design-doc review of this project's architecture foundations
(`CONTEXT.md`, `docs/adr/`, `docs/agents/*`). Reproduce the exact review shape used in
`docs/review/01-2026-07-31-59a2465-foundations.md`.

Scope label for the output filename: `${1:-foundations}`

## Role boundary — advise freely, but don't execute the planning

This command **reviews and gives critical, opinionated advice**, but it does **not
execute** planning work. The reviewer's job is to observe, recommend, prioritize, and
write the report — including sharp, prioritized suggestions. What it must **not** do is
take that planning work on itself:

- Do **not** create / update / close seeds (`sd create|update|close`) — not even to file
  your own findings.
- Do **not** implement fixes or otherwise change the design docs.
- Do **not** offer to carry out the planning ("shall I file these as seeds?" / "shall I
  fix A1?") — that hand-off belongs to a separate planning agent.

Recommendations, prioritized next-ADR suggestions, and critical counsel in the report
are **expected and welcome** — that is the reviewer contributing as an advisor. Only the
**execution** (seed-filing, fixing, deciding) is out of scope; the report is that
planning agent's input.

## Steps

1. **Prime context.** Note the current date and compute the output filename per the
   convention in **Output** below: `NN` = `max(existing NN) + 1` over `docs/review/`,
   `<shorthash>` = `git rev-parse --short HEAD`. Run `sd list` to learn which findings
   are *already tracked* so the review does not re-surface them as new.

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

### Filename convention (decided 2026-08-01)

Review reports are **numbered, dated, and pinned to the reviewed commit** so they sort
chronologically and trace to an exact doc-tree state:

```
docs/review/NN-YYYY-MM-DD-<shorthash>-<scope>.md
```

- `NN` — zero-padded 2-digit running sequence across **all reviews** (chronological
  order). Compute as `max(existing NN) + 1` (list `docs/review/` and take the highest `NN`).
- `YYYY-MM-DD` — date the review is performed.
- `<shorthash>` — 7-char short SHA of the commit the review was performed against, i.e.
  `git rev-parse --short HEAD` at review time (the committed baseline; the review reads
  the working tree but pins the baseline commit for reproducibility).
- `<scope>` — the invocation scope label (lowercase); default `foundations`.

Examples: `01-2026-07-31-59a2465-foundations.md`, `04-2026-08-01-8e8d35d-mache.md`.

**Only review reports carry the `NN-…` scheme.** A session handoff / resolution doc
(companion to a review, recording what a resolution pass changed) is a *different
artifact*: keep it `<YYYY-MM-DD>-<scope>-resolution.md` (unnumbered) so it is not
confused with a review.

Write the report to:

```
docs/review/NN-YYYY-MM-DD-<shorthash>-${1:-foundations}.md
```

Use the **same structure** as `docs/review/01-2026-07-31-59a2465-foundations.md`:

- Top scope line + tag legend (`[FIX]` / `[GAP]` / `[HOUSE]`).
- **A. Inconsistencies / contradictions**
- **B. Undefined / under-defined terms**
- **C. Referenced-but-missing ADRs**
- **D. Documentation structure / housekeeping**
- **E. Implementation maturity** (optional context, not a defect)
- **Suggested next ADRs (priority order)** — critical advice: state what is undecided or
  missing **and** the recommended sequence. Prioritized recommendations are welcome; the
  planning agent decides and executes.
- **Already tracked in seeds** (table) + a "newly surfaced" list (observations only —
  these are **not** filed as seeds by this command).

Persisted writing (the report file) is **English**. Cite findings as
`file → section` or `file L##`. Be precise and specific; no generic advice.

## Reply to the user

Reply **in German** (project convention: spoken language German, persisted writing
English). Keep it under ~200 words: file path + a short bulleted summary of the 3–5
most important findings **and your critical recommendations**. Do **not** offer to file
seeds, implement fixes, or otherwise take on the planning yourself — that is the
planning agent's job. State the report is ready as input, then stop.
