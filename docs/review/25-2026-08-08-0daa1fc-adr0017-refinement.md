# Review 25 — ADR-0017 Refinement (naming, caller model, manifest decision)

- **Scope:** `8d3bddb..0daa1fc` — two ADR-0017 refinement commits (`b9fd0af` naming/caller-model/mandatory-hooks; `18b5cb6` manifest slimming + prompt composition) + seeds sync (`b0c4b5f`, `0daa1fc`).
- **Baseline:** `0daa1fc` (`git rev-parse --short HEAD`)
- **Changed files:** `docs/adr/0017-unified-project-hooks.md` (±111 net), `.seeds/issues.jsonl` (sync)
- **Tag legend:** `[FIX]` contradiction/standards breach, fix now · `[GAP]` referenced but undecided, needs ADR/glossary · `[HOUSE]` doc structure · `[SPEC]` deviation from the seed/ADR ask

---

## Review-24 tracking: what was addressed

| Review-24 ID | Tag | Finding | Status in this range |
|---|---|---|---|
| A1 | `[FIX]` | GAP-3 reversal not explicitly reconciled | **Resolved** — ADR-0012 §4 L75–80 now carries a forward-pointer naming the reversal |
| C1 | `[FIX]` | `checkable` glossary contradicts §3 | **Resolved** — CONTEXT.md L43–47 updated ("in code inside `dagmar-gate`") |
| C3 | `[FIX]` | `Project` CRD entry stale | **Resolved** — CONTEXT.md L81–82 updated |
| D3 | `[GAP]` | "Degrades gracefully" undefined | **Resolved** — hooks now mandatory (noop-allowed); optional-detection mechanism removed |
| D4 | `[GAP]` | Hook caller ambiguous | **Resolved** — two-caller model (Programmatic vs LLM-Tool) |
| E1 | `[GAP]` | Hook-vs-port undecided | **Resolved** — §8 states ports = internal, hooks = external, adapter bridges |
| E2 | `[GAP]` | Manifest library fate | **Resolved** — §5: library retained, metadata types only |
| E3 | `[GAP]` | §6 "no network" vs ADR-0011 | **Resolved** — §6 now "tool-surface constraint, not air-gap" |
| F1 | `[HOUSE]` | ADR-0003 forward-pointer | **Resolved** — status "partially superseded by ADR-0017" |
| F2 | `[HOUSE]` | ADR-0009 §2 forward-pointer | **Resolved** — L33 blockquote |
| F3 | `[HOUSE]` | ADR-0012 §4 forward-pointer | **Resolved** — L75 blockquote |
| F4 | `[HOUSE]` | ADR-0011 gate-body provenance | **Resolved** — L74 now "(ADR-0017: in-code…)" |
| F5 | `[HOUSE]` | Code comments stale | **Resolved** — ADR-0017 Consequences L157–160 notes intentional staleness |

**Not yet addressed (carried forward):** D1 (glossary terms), B2 (ADR-0013 forward-pointer — ADR-0017 mentions it but ADR-0013 itself is unannotated), F4-equivalent (ADR-0014 forward-pointer).

---

## A. Standards

The refinement is a significant improvement: the two-caller model (Programmatic vs LLM-Tool) resolves review-24's D4 ambiguity cleanly, and the mandatory-noop pattern (replace optional/degrades-gracefully with mandatory-but-noop-allowed) is architecturally sound — it guarantees a predictable LLM tool-surface. The manifest slimming decision (§4) is now clearly articulated: retained as metadata, narrowed scope, explicit "remove later if load-bearing content is zero." §5's library fate is similarly clear.

**[FIX] A1 — Stale "os-eco hooks" in §6 body text (ADR-0017 L125–126).** The refinement renamed the §6 heading to "LLM-Tool hooks" (L116) and the opening sentence (L118), but the body at L125–126 still reads *"The os-eco hooks are hermetic because…"* — the old term survives mid-paragraph. This is a direct naming inconsistency within a single section: the heading says "LLM-Tool hooks," the body says "os-eco hooks."

**[FIX] A2 — Redundant "mandatory" in Consequences (ADR-0017 L150–151).** *"the `.dagmar/` project module grows three **mandatory** LLM-Tool hook functions …, **mandatory** when an LLM agent is involved."* The word appears twice in the same bullet. The second instance adds a qualifier ("when an LLM agent is involved") that the first doesn't carry — making them slightly contradictory rather than redundant. Should read: "three LLM-Tool hook functions …, mandatory when an LLM agent operates on the Project."

Fowler smells: no code changed. The ADR shows residual **Speculative Generality** in §4 L98–99 (*"if it proves to carry no load-bearing content over time, it can be removed then"*) — but this is a deliberate "try slimmed, revisit" stance, not gratuitous hedging. Acceptable as a Phase-1 decision posture.

---

## B. Spec

The refinement stays within ADR-0017's decided scope (ADR-0017 resolves spike `dagmar-e8f3`, now closed). No spec drift from the seed.

**[SPEC] B1 — §8 treats `dagmar-prompt` identically to `dagmar-issues`/`dagmar-memory` in the adapter bridge, but §4 defers its relationship.** §8 L164–169 states all three hooks are the "external conformance surface — how the project implements the adapter" and that the oseco adapter *"will call the project's hooks by module-ref, satisfying the port."* This is architecturally clear for `dagmar-issues` (→ `IssueTracker` port → seeds) and `dagmar-memory` (→ `Memory` port → mulch): the hook IS the adapter implementation. But §4 L101–104 says prompt composition (ADR-0005) is *"dagmar-side Go logic, not manifest-declared"* and that *"The `dagmar-prompt` hook may wrap or extend this; the exact relationship is deferred to ADR-0018."* The `Prompts` port's `Compose` method (`oseco.go` L34–36) is dagmar's own cross-store merge — it is not a thin delegation to the project. So §8's "adapter bridges port → project hook" model is settled for issues/memory but unsettled for prompt. The ADR should note this asymmetry in §8 rather than presenting all three hooks under one uniform bridge model.

---

## C. Inconsistencies / contradictions

**[FIX] C1 — CONTEXT.md L225 ADR-list line uses stale "os-eco hooks".** *"ADR-0017 — Unified Project Hooks (everything is Dagger code; checkables move into dagmar-gate; **os-eco hooks**)"* — should say "LLM-Tool hooks" to match the refined ADR's terminology.

**[FIX] C2 — CONTEXT.md L82 uses stale "os-eco hook implementations".** *"Project-specific content (checkable logic, **os-eco hook implementations**) lives in the project module's code"* — should say "Project Hook implementations" or "LLM-Tool hook implementations."

**[FIX] C3 — CONTEXT.md L129–130 ProjectManifest entry uses pre-refinement wording.** *"The manifest may persist as a thin metadata file (os-eco store paths, repo/flow metadata) or be absorbed into module functions (ADR-0017 §4)."* The refined §4 now commits to "slimmed, not removed" and lists concrete metadata fields (display name, description, version), not "os-eco store paths." The "or be absorbed into module functions" hedge was dropped from the ADR but survives in the glossary.

**[HOUSE] C4 — CONTEXT.md L77 "os-eco binding" on Project CRD is post-0017 ambiguous.** The Project CRD entry says it *"Carries dagmar-oper config only (**os-eco binding**, credentials…)"*. Post-0017, the project-specific os-eco binding is via Project Hooks (the project implements `dagmar-issues` etc.), not CR-level configuration. If "os-eco binding" means credential references (secrets), it should say so; if it means the hook-function convention, it's not CR-carried config.

---

## D. Undefined / under-defined terms

**[GAP] D1 — (Carried from review-24) "Project Hook," "LLM-Tool hook," "Programmatic hook," and "backing-service" absent from CONTEXT.md glossary.** The refinement makes these the central vocabulary of ADR-0017 (the ADR title is "Unified **Project Hooks**"), but none appear in the CONTEXT.md glossary. "os-eco hook" appears twice in CONTEXT.md (L82, L225) — now as a stale term with no replacement entry. The two-caller taxonomy (Programmatic vs LLM-Tool) and the term "backing-service" (which replaces "os-eco" in several ADR-0017 passages: §7 L133, §8 L170) are coined in the ADR but never glossary-pinned. This is the highest-priority gap: the ADR's terminology has no glossary anchor.

**[GAP] D2 — `ParseManifest` status ambiguous in §5.** §5 L109 lists `ParseManifest` as a currently-exported type, then deprecates `Checkable`/`validateWorkdir` (L110) but omits `ParseManifest` from both the deprecated and retained lists. The reader infers retention by omission. However, `ParseManifest` (manifest.go L53–67) is internally coupled to `validateWorkdir` and to checkable validation (`if len(m.Checkables) == 0` at L58). If `ParseManifest` is retained for the slimmed manifest, it must be refactored to stop requiring checkables — the ADR should note this coupling.

**[GAP] D3 — "backing-service" is an unanchored synonym introduced without glossary definition.** §7 L133 and §8 L170 replace "os-eco" with "backing-service" (*"Backing-service names (`sd`, `ml`, `cn`)"*), but CONTEXT.md never defines "backing service." CONTEXT.md Tier B section is titled "os-eco backing services" (L57), which implies they are synonyms — but this should be explicit. Is "backing service" now the preferred Tier-B term, replacing "os-eco"? Or are they interchangeable?

---

## E. Referenced-but-missing ADRs / undecided decisions

**[GAP] E1 — `dagmar-prompt` vs ADR-0005 prompt composition: deferred but tension present.** §4 defers the `dagmar-prompt` × ADR-0005 relationship to ADR-0018. The deferral itself is reasonable (two-step pattern mirroring ADR-0009 → ADR-0012). But the ADR currently presents contradictory signals: §4 says the relationship is deferred, §8 presents it as settled. ADR-0018 must resolve: does `dagmar-prompt` **replace** dagmar's cross-store merge (making prompt composition project-scoped), **wrap** it (project adds to dagmar's merge), or **supplement** it (dagmar composes, project's hook is a separate tool the LLM can call)? Each option has different Tier-B implications.

**[HOUSE] E2 — ADR-0013 §5 has no forward-pointer to ADR-0017 (carried from review-24 B2).** ADR-0017's Consequences (L146–149) now explicitly state that ADR-0013 §5 D12's manifest-declared bash-command tool mechanism is *"replaced by ADR-0017's named-function approach."* But ADR-0013 §5 itself (L174–222) still describes the D12 mechanism in full detail — five hermeticity rules, `issues_read`/`issues_write` tool names, feasibility gate — with no annotation that this is superseded. A reader landing in ADR-0013 sees a live specification that is silently dead.

**[HOUSE] E3 — ADR-0014 has no forward-pointer to ADR-0017.** ADR-0014 §5 (L171–183) describes the manifest library as carrying `ProjectManifest`/`Checkable`/`ParseManifest`/`validateWorkdir` as the gate's conformance contract types. ADR-0017 §5 deprecates `Checkable`/`validateWorkdir` and narrows the library to "metadata types." ADR-0014 has no annotation that its types are being deprecated one ADR later. The GAP-1 resolution (L171) says *"the manifest is now genuinely platform-authority by construction (the shared library IS the contract)"* — post-0017, the "contract" is metadata-only, not conformance.

---

## F. Housekeeping

**[HOUSE] F1 — `.dagmar/project.yaml` header comments still describe the old model (L1–6).** *"manifest = what, dagmar-gate = how"* (L5–6). ADR-0017 Consequences L157–160 notes these are *"intentionally stale until the Phase-2 refactor"* — this is good policy. But the project.yaml itself has no inline note pointing to ADR-0017. A one-line comment (*"# NB: ADR-0017 supersedes the manifest=what model; comments below are stale until Phase 2"*) would prevent confusion for a reader landing in the file without the ADR context.

**[HOUSE] F2 — Mulch record [mx-38d10e] still says "dagmar-gate = wrapper over manifest checkables."** The self-bootstrap trajectory guide states *"dagmar-gate = wrapper over manifest checkables (ADR-0003), not the checkable itself."* This contradicts ADR-0017 §3 ("dagmar-gate = what AND how"). Historical mulch records are dated guidance, but this one is actively referenced in the trajectory guide. Consider updating or annotating it.

---

## Suggested next ADRs (priority order)

1. **ADR-0018: Project Hook contract** (highest priority — unchanged from review-24). Specify
   `dagmar-issues`/`dagmar-memory`/`dagmar-prompt` Go signatures (inputs, outputs, error contract).
   Must also resolve the `dagmar-prompt` × ADR-0005 tension (E1): replace, wrap, or supplement
   dagmar's cross-store merge. The ADR-0017 two-step deferral is sound but the §8 vs §4 asymmetry
   (B1) should be explicitly acknowledged as an open question ADR-0018 closes.

2. **ADR-0013 §5 + ADR-0014 forward-pointers** (housekeeping, not new decisions). Annotate
   ADR-0013 §5 D12 as superseded by ADR-0017; annotate ADR-0014 §5 GAP-1 resolution as narrowed
   by ADR-0017 (library retains metadata types only).

3. **Glossary update for CONTEXT.md** (not an ADR, but blocking). Pin "Project Hook,"
   "LLM-Tool hook," "Programmatic hook," and "backing-service" in the glossary. Resolve whether
   "backing-service" replaces "os-eco" as the Tier-B preferred term.

---

## Already tracked in seeds

| Finding | Seed | Status |
|---------|------|--------|
| Wayfinder map (ADR-0017 entry) | `dagmar-80dd` | open — map updated with "Unified Project Hooks — ADR-0017 decided" summary (L in Decisions-so-far) |
| Spike `dagmar-e8f3` (os-eco tool-wrapping) | `dagmar-e8f3` | closed — answered by ADR-0017 |

## Newly surfaced (observations only — NOT filed as seeds)

| ID | Tag | Finding |
|----|-----|---------|
| A1 | `[FIX]` | ADR-0017 §6 L125–126: stale "os-eco hooks" — heading renamed to "LLM-Tool hooks" but body text not updated |
| A2 | `[FIX]` | ADR-0017 Consequences L150–151: redundant/contradictory double "mandatory" in ADR-0014 bullet |
| B1 | `[SPEC]` | §8 treats dagmar-prompt uniformly with issues/memory in the adapter bridge, but §4 defers its relationship — §8 should note the asymmetry |
| C1 | `[FIX]` | CONTEXT.md L225: ADR-list line says "os-eco hooks" instead of "LLM-Tool hooks" |
| C2 | `[FIX]` | CONTEXT.md L82: "os-eco hook implementations" — stale term |
| C3 | `[FIX]` | CONTEXT.md L129–130: ProjectManifest entry uses pre-refinement wording ("may persist… or be absorbed") vs §4's "slimmed, not removed" |
| C4 | `[HOUSE]` | CONTEXT.md L77: "os-eco binding" on Project CRD post-0017 ambiguous (hooks vs CR config) |
| D1 | `[GAP]` | "Project Hook," "LLM-Tool hook," "Programmatic hook," "backing-service" absent from glossary (carried from review-24) |
| D2 | `[GAP]` | §5: `ParseManifest` deprecation/retention status ambiguous by omission; coupled to deprecated `validateWorkdir` |
| D3 | `[GAP]` | "backing-service" introduced as synonym for "os-eco" without glossary definition or preferred-term decision |
| E1 | `[GAP]` | `dagmar-prompt` × ADR-0005 tension: §4 defers, §8 presents as settled — ADR-0018 must resolve (replace/wrap/supplement) |
| E2 | `[HOUSE]` | ADR-0013 §5 D12: no forward-pointer (mechanism silently dead in source ADR) |
| E3 | `[HOUSE]` | ADR-0014 §5: no forward-pointer (manifest library types deprecated without source ADR annotation) |
| F1 | `[HOUSE]` | `.dagmar/project.yaml` L1–6: no inline note pointing to ADR-0017 staleness policy |
| F2 | `[HOUSE]` | Mulch [mx-38d10e]: "dagmar-gate = wrapper over manifest checkables" contradicts ADR-0017 §3 |
