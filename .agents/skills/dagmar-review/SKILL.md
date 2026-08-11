---
name: dagmar-review
description: Spawn an independent subagent to perform a deep, whole-codebase audit of the dagmar project along seven axes — Go code quality & best practices, test coverage, ADR coherence, domain-model consistency, Dagmar-Gate completeness & strictness, gaps/stubs/misconceptions, and mulch/seeds cross-reference. The agent scans all four Go modules, cross-references every ADR, works with Mulch and Seeds, and writes a prioritized report to docs/review/. Use after a major work session, before committing, or whenever you want a rigorous independent quality pass that also analyzes whether the Dagmar-Gate exploits all possibilities and is strict enough.
---

# dagmar-review skill

Spawn the dagmar Deep Review subagent to audit the entire codebase and write a
prioritized report to `docs/review/`.

## When to use

- After a major work session — invoke to get an independent deep audit.
- Before committing a large change — catch gaps the diff review misses.
- Periodically (e.g., monthly) — track architecture drift and debt accumulation.
- When you want to know if the Dagmar-Gate is strict enough and covers all bases.
- When you want to validate that code matches the ADRs and domain model.

The reviewer advises only — it does NOT execute (no seeds, no mulch, no fixes).

## Usage

```python
# From the Prime Agent IPython kernel — default: whole-codebase deep review:
handle = await dagmar_review.run(_rlm=rlm)

# With explicit git range and scope label:
handle = await dagmar_review.run(range="abc1234..HEAD", scope="post-session", _rlm=rlm)
```

The `_rlm` parameter injects the Prime Agent spawn function (a kernel global)
into the skill module. This is required because Python modules cannot see
kernel globals directly.

The agent is spawned in the background. It writes its report to
`docs/review/NN-YYYY-MM-DD-<shorthash>-<scope>.md` and replies in German via
`agent_message` when done. The `handle` returned is the admission confirmation,
not the result.

## What the agent reviews (seven axes)

| Axis | Focus |
|------|-------|
| **A — Go Code Quality** | Idiomatic Go, error handling, interface design, SoC/ADR-0010, naming, concurrency, resource management, Fowler code smells, dead code, TODO markers |
| **B — Test Coverage** | Per-module coverage metrics, critical-path (controller pipeline) coverage, test quality, coverage-ratchet gaps, untested error paths |
| **C — ADR Coherence** | Per-ADR implementation match, supersession chains, cross-ADR consistency, missing ADRs, stale references |
| **D — Domain Model** | CONTEXT.md glossary completeness, code↔model drift, tier violations (A/B/C), relationship accuracy |
| **E — Dagmar-Gate** (special) | Gate completeness (missing checks: `-race`, golangci-lint, govulncheck, license, CRD/deepcopy gen), gate strictness (fail-open risk, coverage floor default, fail-fast vs run-all), contract robustness, ADR alignment |
| **F — Gaps & Misconceptions** | Phase stubs, unimplemented ADR features, code that contradicts ADRs/glossary, dead/obsolete code |
| **G — Mulch & Seeds** | `ml doctor` integrity, unrecorded lessons, seed↔ADR traceability, findings without corresponding seeds |

## Finding tags

| Tag | Meaning |
|-----|---------|
| `[FIX]` | Contradiction, standards breach, or bug — fix now |
| `[GAP]` | Referenced but undecided or unimplemented — needs ADR/decision |
| `[HOUSE]` | Documentation/structure cleanup |
| `[SPEC]` | Deviation from the seed/ADR ask |
| `[RISK]` | Works now but fragile, unscalable, or security-relevant |

## Install

The `dagmar_review.py` module in this directory is the source of truth. To make
it importable in the Prime Agent kernel, copy it to the kernel-venv site-packages:

```bash
mkdir -p ~/.prime/agent/kernel-venv/lib/python3.11/site-packages/dagmar_review
cp .agents/skills/dagmar-review/dagmar_review.py \
   ~/.prime/agent/kernel-venv/lib/python3.11/site-packages/dagmar_review/__init__.py
```
