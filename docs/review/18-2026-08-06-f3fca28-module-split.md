# Review 18 — dagmar platform/project module split (ADR-0014, implementation), 2026-08-06

- **Date:** 2026-08-06
- **HEAD / artifact:** `f3fca28` — 2 commits (`816ab51` feat: implement platform/project module split;
  `f3fca28` docs: ADR-0014 GAP-1 interim note + seed `dagmar-a1e0`). Range reviewed: `9158f1b..HEAD`.
- **Scope:** the IMPLEMENTATION of ADR-0014 (Q1–Q6), not the design — the design was review-17
  (`docs/review/17-2026-08-05-cb32310-b8c2.md`, on `cb32310`, PROPOSED → revised → ACCEPTED). This
  review checks that the code cashes out the accepted decision: module boundary real, assignment
  correct, Q5 structural guarantee holds, dependency safety of the move, GAP-1 interim honest,
  generated bindings committed, ADR coherence, standards.
- **Reviewer posture:** advice-only. No code, ADR, or seed changed; this document only. Reviewed
  directly (no sub-delegation). Each claim cross-checked against the cited source.
- **Verdict:** **SOUND** — the split faithfully realizes ADR-0014 Q1–Q6; the module boundary is a
  real Dagger + Go module boundary (not a package split); the Q5 shortcuit-closure is enforced by
  structure (verified by reading the full platform `main.go`); GAP-1 is an honestly-documented
  interim with a precisely-scoped seed for the real fix. Three low-severity HOUSE items (one concrete
  `go mod tidy` drift, two comment/clarity nits); none blocks push.

> Advice-only. Independent review; each claim below was checked against the cited code, ADR, seed,
> and prior reviews 14 (self-protection precedent) + 17 (design baseline).

---

## 1. Verification performed

Read in full: `docs/adr/0014-platform-project-scope-separation.md`; root `dagger.json`;
`.dagger/main.go` (every method — confirmed no gate-family remains); `.dagmar/main.go`;
`.dagmar/dagger.json`; `.dagmar/go.mod`; `.dagmar/project.yaml`; `.dagmar/.gitignore`;
`.dagmar/.gitattributes`; `.dagmar/internal/workflows/{gate,bootstrap}.go`;
`.dagmar/internal/config/{manifest,doc}.go`; `.dagger/internal/config/doc.go`; `.dagger/.gitattributes`;
`.dagger/go.mod`; `.github/workflows/ci.yml`; `lefthook.yml`; `mise.toml`. Queries against the seed
store (`dagmar-a1e0`) and prior reviews 16 (format) + 17 (design / GAP-1 origin).

Confirmed against the cited sources:

- `.dagger/main.go` exposes **only** `Up`, `DeployEngine`, `Probe`, `ProbeNet`, `ProbeCache`, `Sandbox`
  — no `DagmarGate`/`DagmarBootstrap`. The gate-family is gone from the platform module.
- `.dagmar/` is a real Dagger module: own `dagger.json` (name `dagmar`, engineVersion `v0.21.8`,
  sdk go) + own `go.mod` (`module dagger/dagmar-project`). Not a same-module package split.
- `.dagmar/main.go` exposes `DagmarBootstrap` + `DagmarGate`, delegating to
  `workflows.{Bootstrap,Gate}`; imports only `dagger/dagmar-project/internal/{dagger,workflows}`.
- Root `dagger.json` is platform-only (`source: .dagger`, k3s dep, no `.dagmar` reference) → the
  default module (no `-m`) is the platform, which lacks `dagmar-gate`.
- CI (`ci.yml:32`) and lefthook (`lefthook.yml:23`) both invoke `-m .dagmar dagmar-gate --source .`;
  `mise.toml:22` pins `"ubi:dagger/dagger" = "v0.21.8"` so the lefthook `mise exec -- dagger` shim resolves.
- `.dagmar/go.mod` carries **no** relative `replace` toward `.dagger` (the GAP-1 interim is clean —
  no broken directive left in). Its only non-SDK direct require is `gopkg.in/yaml.v3`.
- `.dagmar/dagger.gen.go` + `.dagmar/internal/dagger/dagger.gen.go` are committed; `git check-ignore`
  reports them NOT ignored; `.dagmar/.gitattributes` mirrors `.dagger/.gitattributes` exactly.
- No `go.work` — all three Go modules build in module mode (consistent with the `project.yaml` claim).
- grep over `.dagger/**/*.go`: zero code-level references to `workflows`/`ParseManifest`/
  `dagmar-project` (only comment mentions) → the platform has no dangling dependency on the moved code.

---

## 2. Findings

### [SPEC-1] Q1–Q6 realized; platform/project assignment matches the ADR §2 table — CONFIRMED

**Severity:** n/a (positive coherence check). **Files:** `.dagmar/{main.go,dagger.json,go.mod}`;
`.dagger/main.go`; `.dagmar/project.yaml`.

Each Q maps to concrete code:

| Q | ADR claim | Implementation |
|---|---|---|
| **Q1** module boundary, not code boundary | two Dagger modules on a module boundary | `.dagmar/` has its own `dagger.json` + `go.mod` (`dagger/dagmar-project`) — a real module, not a package ✓ |
| **Q2** assignment | platform = Up/DeployEngine/Probe*/Sandbox; project = DagmarBootstrap/DagmarGate + workflows + manifest | `.dagger/main.go` retains exactly the platform set (read in full); `.dagmar/main.go` exposes the gate-family + delegates to `workflows` ✓ |
| **Q3** path symmetry | `.dagger`=platform, `.dagmar`=project; ref `-m .dagmar` | as claimed ✓ |
| **Q4** invocation = convention | every caller does `-m <ref> call dagmar-bootstrap`/`dagmar-gate` | CI + lefthook identical; no central wrapper ✓ |
| **Q6** toolchain home | `dagmar-bootstrap` (mise rollout) in the PROJECT module; no platform default | `bootstrap.go` lives in `.dagmar/`; platform never references it ✓ |

**Verdict:** CONFIRMED — the code realizes the decision.

### [SPEC-2] Q5 structural guarantee — the shortcuit is excluded by structure — CONFIRMED

**Severity:** n/a (positive verification). **Files:** `.dagger/main.go`; root `dagger.json`;
`.github/workflows/ci.yml:32`; `lefthook.yml:23`; `.dagger/dagger.gen.go`.

Because `dagmar-gate`/`dagmar-bootstrap` live ONLY in `.dagmar/`, and the root `dagger.json`
(`source: .dagger`, no `.dagmar` dep) makes the platform the default module, a bare
`dagger call dagmar-gate` cannot resolve — it fails structurally, not by convention. The ref call is
the only path: `ci.yml:32` `args: -m .dagmar dagmar-gate --source .`; `lefthook.yml:23` identical.
The `.dagger/dagger.gen.go` diff is −28 lines — the removed functions' generated bindings, regenerated
(not left stale). The platform calling its own gate function is now structurally impossible; the
abstraction's *ref* is secured by structure (its *conformance* remains by convention, per Q4 — out of
scope here, deferred as the runtime probe).

**Verdict:** CONFIRMED — Q5 holds by construction.

### [SPEC-3] GAP-1 interim is honest; seed `dagmar-a1e0` scopes the real fix — CONFIRMED

**Severity:** n/a (positive verification). **Files:** `.dagmar/go.mod`; `.dagmar/internal/config/manifest.go:5-11`;
`.dagmar/internal/config/doc.go`; `.seeds/issues.jsonl` (`dagmar-a1e0`); ADR-0014 Consequences/Deferred.

The interim rationale — Dagger loads each module in source isolation, so a relative
`replace dagger/dagmar => ../.dagger` cannot resolve at module-load time — is correct, and the seed
records the empirical failure (`/src/.dagger/go.mod: no such file or directory`). Critically,
`.dagmar/go.mod` carries **no** relative replace: the interim is clean (no broken/legacy directive
left behind for a future contributor to trip on). `manifest.go:5-11` + `doc.go` cross-reference the
interim, the convention+conformance-probe authority backstop, and the published-library end-state.
Seed `dagmar-a1e0` (priority 1, open) scopes the fix precisely: extract the contract types + parser
into a versioned module both `.dagger` and `.dagmar` depend on by `require` (not relative replace),
with open layout/resolve questions and an explicit "prototype the resolve end-to-end in the gate
container before committing" gate. The authority inversion (a project module owns the platform-authority
schema) is explicitly acknowledged as interim, not hidden — this is the honest posture review-17 GAP-1
asked the *ADR* to take; the *implementation* + seed take it too.

**Verdict:** CONFIRMED — GAP-1 is a faithful, well-scoped interim.

### [HOUSE-1] `.dagger/go.mod` retains an orphaned `gopkg.in/yaml.v3` direct require after the manifest moved — `go mod tidy` drift

**Severity:** low (tidiness; not caught by the gate). **File:** `.dagger/go.mod` (direct `require` block).

`manifest.go` was the only `yaml.v3` consumer in `.dagger/`; it moved to `.dagmar/` at this split.
grep confirms no `.dagger/**/*.go` imports `gopkg.in/yaml` anymore (only a comment in `config.go:4`
mentions it). Yet `.dagger/go.mod` still lists `gopkg.in/yaml.v3 v3.0.1` as a **direct** require.
`go mod tidy` would drop or recategorize it. The gate does not catch this: the `dagger-module`
checkable runs `go build ./... && go vet ./... && go test ./...` — none of which flags an unused
go.mod entry (it builds fine). Harmless, but it is a concrete artifact of the move worth tidying so
the platform module's dependency list reflects reality. (`.dagmar/go.mod` is clean — `yaml.v3` is
genuinely used there by `manifest.go`.)

**Recommendation:** run `go mod tidy` in `.dagger/` and commit the result. Not blocking.

### [HOUSE-2] Stale "refactored into workflows/" comments in `.dagger/main.go`

**Severity:** low (comment rot). **Files:** `.dagger/main.go:268,306`.

The `ProbeNet` and `ProbeCache` spike doc-comments end with "to be refactored into workflows/ later —
ADR-0010 Consequences." The `workflows/` package no longer exists in `.dagger/` — it moved to `.dagmar/`
at this split. The pointer now names a location absent from this module. (These are platform-scope
spikes; their eventual refactor home would be a *platform* workflows package that does not exist yet,
not the project module's.)

**Recommendation:** reword to "to be refactored into a platform workflows/ package if/when one is
introduced" (or drop the pointer). Cosmetic.

### [HOUSE-3] `dagger.json` `name: "dagmar"` ≠ go.mod path `dagger/dagmar-project` — benign but the reason is undocumented

**Severity:** low (clarity for the next reader). **Files:** `.dagmar/dagger.json:2`; `.dagmar/go.mod:1`;
`.dagmar/main.go:22`.

The project module's Dagger `name` is `dagmar` while its Go module path, its checkable name
(`project.yaml` → `dagmar-project`), and the ADR's label are all `dagmar-project`. This is **forced
and correct**: Dagger derives the main object type from `name` (kebab→CamelCase), so `name: "dagmar"`
matches `type Dagmar` in `main.go`; `dagmar-project` would make the SDK expect `DagmarProject` and
break codegen. `main.go:22` documents the *inter-module* name collision (`.dagger` and `.dagmar` both
`dagmar`, benign because path-addressed) but not this *intra-module* name≠path. There is no collision
risk either way: `.dagger/dagger.json` declares no `.dagmar` dependency, and the platform invokes by
`-m` ref, never as a Dagger dependency.

**Recommendation:** a half-line in `main.go` or `dagger.json` noting the name stays `dagmar` to match
the `Dagmar` type would spare the next reader the deduction. Optional.

---

## 3. Standards assessment

Checked against the conventions established in reviews 12–17 and ADR-0010:

| Convention | Status |
|---|---|
| Comment density / ADR cross-referencing | **Consistent** — every new/edited block cites its ADR (ADR-0014 Q1–Q6, ADR-0009 §2, ADR-0012 §4, ADR-0003); `manifest.go`/`doc.go` carry the GAP-1 scope note; `bootstrap.go:11-16` documents the `debian:12-slim` choice + the chainguard DNS rationale honestly |
| Generated bindings committed (review-14 self-protection) | **Consistent** — `.dagmar/dagger.gen.go` + `internal/dagger/dagger.gen.go` committed; `git check-ignore` confirms NOT ignored; `.gitattributes` mirrors `.dagger` exactly (3 lines) |
| Module-mode build / no go.work | **Consistent** — no `go.work`; three checkables build each module in isolation; `./...` respects both module boundaries and dot-dir exclusion, so the root build does not recurse into `.dagger`/`.dagmar` |
| ADR-0014 coherence (Consequences/Deferred) | **Consistent** — Consequences describes `.dagger` giving up the gate-family, `.dagmar` becoming a module, CI/lefthook → `-m .dagmar`, the re-home (not discarded), AND the `config` split (review-17 GAP-1 incorporated); Deferred lists the published shared lib (`dagmar-a1e0`), published/subpath-ref dogfood, conformance probe, central wrapper. `f3fca28`'s +11 lines add the GAP-1 interim paragraph that matches the implementation |
| Dependency safety of the move | **Consistent** — `.dagger` has zero code-level dependency on the moved workflows/manifest (grep-confirmed); `.dagmar` imports only its own SDK path + stdlib + `yaml.v3` |

Drift: `yaml.v3` tidy (HOUSE-1); stale `workflows/` comment (HOUSE-2); name≠path undocumented (HOUSE-3). Everything else is clean.

---

## 4. Conclusion

The module split is **SOUND**. The boundary is a genuine Dagger + Go module boundary
(`.dagmar/dagger.json` + `go.mod dagger/dagmar-project`), not a package relabel — so the platform
addressing the project by `-m .dagmar` ref is the real abstraction path, and the Q5 shortcuit is
closed by structure (the platform `main.go` no longer contains the gate-family; the default module
cannot resolve `dagmar-gate`). Q1–Q6 are each realized and match the ADR §2 assignment (SPEC-1); the
Q5 guarantee holds by construction (SPEC-2). GAP-1 is handled with the right posture: the interim is
clean (no broken relative `replace` left in `.dagmar/go.mod`), the rationale is correct, and seed
`dagmar-a1e0` scopes the published-shared-library fix precisely with the empirical failure mode on
record (SPEC-3). Generated bindings are committed and `.gitattributes`-marked, reproducing the
review-14 self-protection; CI and lefthook are both wired to `-m .dagmar` with the dagger CLI pinned
in `mise.toml`. The three HOUSE items are a `go mod tidy` drift in `.dagger/go.mod` (HOUSE-1, the one
concrete cleanup), a stale comment (HOUSE-2), and an optional clarity half-line (HOUSE-3) — none
blocks push.

**Verdict: SOUND.** Push it. Tidy `.dagger/go.mod` (HOUSE-1) when convenient; the rest is optional polish.

**Summary of findings:** 3 SPEC (all CONFIRMED), 3 HOUSE.
