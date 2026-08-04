# Review 14 — Phase 1: always-Dagger gate-family

- **Date:** 2026-08-04
- **HEAD:** `54c3eaa` (scope: phase1-gate-family)
- **Baseline:** review 13 / `d1d8237` (Phase-0 Increment 2)
- **Reviewer:** dagmar-review (advice-only)
- **Verdict:** **Phase 1 is SOUND** — the gate correctly gates, CI will go green, ADR coherence is strong, the generated-files fix is correct. Two `[FIX]` items degrade *failure diagnostics* (not detection) and should land before Phase 2 reuses the gate in-loop; the rest are `[GAP]`/`[HOUSE]`/`[SPEC]`.

> Advice-only. No seeds filed, no code/doc changes beyond this report.

---

## 1. Scope reviewed (full Phase-1 diff vs `d1d8237`)

| File | Role | LoC |
|---|---|---|
| `.dagger/main.go` | `DagmarBootstrap` + `DagmarGate` thin delegators to `workflows` (+26) | 389 |
| `.dagger/internal/workflows/gate.go` | `Gate` — reads manifest, runs checkables in `golang:1.26`, `ReturnTypeAny` + `DAGMAR_EXIT` marker, abort-on-failure | 88 |
| `.dagger/internal/workflows/bootstrap.go` | `Bootstrap` — `go mod download` per deduped workdir | 49 |
| `.dagger/internal/config/manifest.go` | `ProjectManifest`/`Checkable` types + `ParseManifest` (yaml.v3, pure) | 52 |
| `.dagmar/project.yaml` | dagmar-own manifest: `controller` (build/vet/test/gofmt) + `dagger-module` (build/vet/test) | 23 |
| `.github/workflows/ci.yml` | `dagger-for-github@v8.4.1`, `dagger call dagmar-gate --source .`, v0.21.8 | 26 |
| `.dagger/.gitignore` | generated bindings now COMMITTED (was ignored) | diff |
| `.dagger/dagger.gen.go`, `internal/dagger/*.gen.go` | committed generated SDK (+~18k lines, linguist-generated) | — |

Plus coherence reads: ADR-0003, ADR-0009 §2, ADR-0010, ADR-0011, ADR-0012 §4/§5, CONTEXT.md.

## 2. Verification performed

- `go build ./...` (root) → **exit 0**
- `gofmt -l .` (root, = controller checkable) → **empty (clean)** — confirms committed `dagger.gen.go` is gofmt-clean
- `GOWORK=off go -C .dagger build ./...` → **exit 0**
- `GOWORK=off go -C .dagger vet ./...` → **exit 0**
- `func Connect() *Client` (generated SDK) returns **no error** → `dagger.Connect().Container()` is valid (compiles)
- `ReturnTypeAny` / `ContainerWithExecOpts.Expect` / `Expect: dagger.ReturnTypeAny` API confirmed in generated SDK
- `dagger.json` (root): `engineVersion: v0.21.8`, `sdk.source: go`, `source: .dagger`, dep `k3s@…v0.11.1`
- `dagger-for-github@v8.4.1/action.yml` fetched: `version`, `verb` (default `call`), `args`, `workdir` (default `.`) inputs all present and match ci.yml usage

Both modules build/vet clean and are gofmt-clean locally (toolchain `go1.26.1`, `.dagger/go.mod` `go 1.26.5` resolved via GOTOOLCHAIN). The gate's own checkables pass locally → **CI is expected to go green** on push.

## 3. Findings

### [FIX-1] Gate captures **stdout only** — go toolchain diagnostics go to stderr → abort message is typically empty on failure
**Severity:** medium (diagnostics, not detection). **File:** `gate.go:66-67, 44-45`.

`runCheckable` reads `.Stdout(ctx)`. Go tooling (`go build`, `go vet`, `go test` compile errors, `gofmt` parse errors) writes its diagnostics to **stderr**, not stdout. With `Expect: ReturnTypeAny` the exit code is recovered correctly from the `DAGMAR_EXIT` marker (the `echo` always runs via `;`, so detection is unaffected), but the surfaced *output* block in the abort message (`gate.go:44`) will usually be **empty** — the compile error the coder needs went to stderr and was dropped.

This defeats the code's own stated intent ("Include the failing output so CI / the coder sees why"). It is benign for Phase-1 CI (the step still fails red) but **material for Phase 2**, where the in-loop coder reads this output to self-repair.

> Note: the brief states the negative test "showed output." That output most likely came from Dagger's *progress stream* (stderr is shown in the exec progress), **not** from the gate's returned string. Worth re-checking that the *returned* `runCheckable` output (not the streamed log) actually contains diagnostics.

**Recommendation:** merge streams in the shell (`cmd := c.Command + ` 2>&1; echo "DAGMAR_EXIT=$?"``) or capture `.Stdout` + `.Stderr` separately and join for the abort message.

### [FIX-2] `parseDagmarExit` returns the **first** `DAGMAR_EXIT=` line, not the trailing real marker
**Severity:** low-medium (latent correctness). **File:** `gate.go:77-88`.

The marker is appended **last**, but `parseDagmarExit` scans top-to-bottom and returns the **first** line starting with `DAGMAR_EXIT=`. If any checkable's own output ever contains a literal `DAGMAR_EXIT=0` line (a verbose test, a build log, a future checkable echoing the string), that spurious line is returned and a **genuinely failing** checkable is reported as **pass**. Probability is low for today's `go build/vet/test/gofmt`, but the failure mode is a silent false-green — exactly what a gate must never produce.

**Recommendation:** return the **last** match (iterate and keep, don't return early), e.g. capture into a var across the loop and return it after; or anchor the marker uniquely (`DAGMAR_EXIT_MARKER=$?`) and scan for the suffix.

### [GAP-1] No `config/manifest_test.go` — `ParseManifest` is pure and table-testable, repo pattern expects tests
**Severity:** medium (coverage gap; explicitly raised in scope). **File:** `config/manifest.go:38-52`.

`ParseManifest` is pure YAML → typed struct with validation (empty-checkables, name/command required). It is the ideal table-test target. The repo already has `domain/sandbox_test.go` and `app/sandbox_integration_test.go`, establishing the test convention — but `config/` has none. Edge cases worth pinning: empty checkables, checkable missing name/command, duplicate names, `Env` map round-trip, and the path-traversal input (see HOUSE-2).

### [GAP-2] Gate does not verify checkable *coverage*; dagmar-own has no true linter
**Severity:** medium (conformance completeness). **Files:** `gate.go` (trusts the set), `.dagmar/project.yaml`.

ADR-0003 requires checkables to cover **build/test/lint**. The gate runs *whatever* the manifest declares — it does not assert the set includes build+test+lint. dagmar-own's manifest supplies build/vet/test/gofmt (controller) and build/vet/test (dagger-module): `vet` + `gofmt` partially cover "lint/static-analysis", but there is **no dedicated linter** (`golangci-lint` / `staticcheck`). Two separable points:
1. No conformance floor-check that build/test/(lint) are all present (a manifest with only `go build` would pass the gate yet be under-conforming vs ADR-0003).
2. dagmar-own itself is mildly under-linted relative to ADR-0003's "lint".

**Recommendation (later phase):** a minimal manifest validator (pure, testable) asserting the presence of build/test/lint semantics; and consider adding `staticcheck`/`golangci-lint` to dagmar-own. Not a Phase-1 blocker — the gate's job this phase is to *run declared* checkables, which it does.

### [GAP-3] `gateImage = "golang:1.26"` is a floating minor tag — non-reproducible
**Severity:** low (reproducibility). **File:** `gate.go:16`.

A floating tag resolves to the latest `1.26.x` at run time; a future point release could change behavior (or a pulled-tag mutation). ADR-0011 accepts that the gate has network, so reproducibility is the concern, not hermeticity. **Recommendation:** pin to an exact tag or digest (e.g. `golang:1.26.5`) to match the declared module floor and make gate runs reproducible.

### [HOUSE-1] `dagmar-bootstrap` is defined and doc-marked "reused in CI" but ci.yml calls only `dagmar-gate`
**Severity:** low. **Files:** `bootstrap.go:17`, `.github/workflows/ci.yml`.

`Bootstrap`'s doc comment says it is "Reused alongside dagmar-gate in CI," and ADR-0012 §4 lists both as Phase-1 deliverables, but ci.yml invokes only `dagmar-gate`. Functionally harmless: the gate's `go build` downloads deps in the networked container, so CI is green without bootstrap. But bootstrap is currently **unexercised** in CI, so its "deps resolve / cache-warm" purpose (review-11 HOUSE-5) is neither proven nor warming any cache. **Recommendation:** either add a `dagmar-bootstrap` step before `dagmar-gate` in ci.yml, or soften the comment to "available for CI/in-loop reuse" until wired.

### [HOUSE-2] `Workdir` is not validated — `path.Join("/src", c.Workdir)` can escape `/src`
**Severity:** low (trusted input). **File:** `gate.go:60` (and `bootstrap.go:39`).

`path.Join` cleans the path, so a manifest `workdir: ../../etc` resolves outside `/src`. The manifest is committed by the project owner (trusted), so the practical risk is nil today — but a future external/PR-supplied manifest or a typo could mis-target. **Recommendation:** reject `..` / absolute workdirs in `ParseManifest` (pure, testable) — cheap defense, fits GAP-1's tests.

### [HOUSE-3] `config/` has no `doc.go` (package comment is inline on `manifest.go`)
**Severity:** trivial. The package doc *does* exist (comment block above `package config`), so `godoc` is satisfied; but sibling packages (`workflows/`, `tools/`, `adapters/*`) use a dedicated `doc.go`. Consistency-only.

### [SPEC-1] Schema implements only the `command` form; ADR-0003's "verify-script ref" is deferred
**Severity:** informational (intentional minimalism). **Files:** `manifest.go:26-35`, ADR-0003.

ADR-0003 specifies `checkables:` as "build/test/lint commands **or** a verify-script ref." Phase 1's `Checkable` carries only inline `Command` (no script-ref field). This is an appropriately minimal Phase-1 cut — the `Env` map is already reserved for later (e.g. `GOWORK`) — but it is a *partial* implementation of the ADR's schema. **Recommendation:** nothing now; track the verify-script-ref form as a future extension when a Project actually needs it (YAGNI until then).

## 4. Coherence with the ADRs (strong)

- **ADR-0003 (manifest = what):** `ParseManifest` + `.dagmar/project.yaml` implement exactly the in-repo conformance contract; checkables are required (empty → error). Dogfooding honored (dagmar declares its own first). Schema is command-only — see SPEC-1.
- **ADR-0009 §2 (gate = how; Justfile → always-Dagger):** `dagmar-gate` is the single deterministic entry point that *consumes* (not re-declares) manifest checkables — manifest=what/gate=how preserved verbatim. No `just`/`bun`; both wrappers are Dagger functions. §2's prepare/verify lifecycle stages (`dagmar-bootstrap`/`dagmar-gate`) retained as names. ✓
- **ADR-0010 (layout; main delegates):** `workflows/` is the home for gate/bootstrap; `config/` for the manifest; `main.go` `DagmarGate`/`DagmarBootstrap` are thin delegators. Matches the hexagonal/home convention. ✓
- **ADR-0011 (gate is networked, not hermetic):** `gate.go` runs container-exec with default outbound network — explicitly consistent with "dagmar-bootstrap and dagmar-gate (deterministic) have network." The `ProbeNet` residual (no per-exec no-network flag in `ContainerWithExecOpts`) is the very residual ADR-0011 §3 accepts. ✓
- **ADR-0012 §4 (first earned capability; carve-out):** implementation realizes §4 point-for-point — prepare/verify Dagger functions; gate=wrapper-not-checkable (review-11 GAP-3); always-Dagger, not flexible transport; conformance floor = "Project is a Dagger module"; reused in CI + (future) in-loop; hermeticity carve-out (gate is a *named* function, so not withheld as the raw `container` tool — its network residual is the accepted one). ✓
- **CONTEXT.md:** lines 44-45 and 120-122 ("run by the always-Dagger wrapper function `dagmar-gate`"; "manifest declares what … `dagmar-gate` = how"; "reused") match the code.

## 5. Generated-files-commit fix — correct

- Prior `.dagger/.gitignore` **ignored** `/dagger.gen.go`, `/internal/dagger`, `/internal/telemetry`; `.dagger/.gitattributes` **marked them `linguist-generated`** — a direct contradiction, and a fresh CI checkout lacked the bindings so the `.dagger` checkable's `go build` failed. The fix removes the ignore rules (replaced by an explanatory comment) and **commits** the generated files. ✓
- `.dagger/.gitattributes` (`/dagger.gen.go`, `/internal/dagger/**`, `/internal/telemetry/**` → `linguist-generated`) hides the ~17.7k generated lines from GitHub diffs/stats/blame. Acceptable and matches Dagger's "commit the generated files" guidance (mulch mx-e05f90 / mx-dbaed7). No real concern.
- **Self-protecting:** if a signature changes in `main.go` without `dagger develop` + re-commit, the committed bindings drift and the `.dagger` checkable (`go build ./...`) fails the gate — so the gate itself catches generated-file drift. Positive.
- Minor: both gen files at `.dagger/dagger.gen.go` (+384) and `.dagger/internal/dagger/dagger.gen.go` (+17433), plus `k-3-s.gen.go` (+234, k3s dep), are committed. All needed for build; verified clean.

## 6. ci.yml — SOUND

- `dagger/dagger-for-github@v8.4.1` is a real tag; its `action.yml` defines `version`, `verb` (default `call`), `args`, `workdir` (default `.`) — all used by ci.yml. Effective command: `dagger --progress plain call dagmar-gate --source .`.
- `version: "0.21.8"` → action prepends `v` → `v0.21.8` == `dagger.json.engineVersion` `v0.21.8`. Pinned, matches.
- Local-module auto-detect works: `workdir` defaults to repo root where `dagger.json` lives (`source: .dagger`).
- `--source .` binds the workspace to the `source *dagger.Directory` arg; the function reads `.dagmar/project.yaml` from it.
- **Expected: green on push** (modules build/vet/gofmt-clean locally; network available in CI for any module fetch; `go.sum` committed).

## 7. Standards

- `doc.go` per package: present for `workflows/` (HOUSE-3 for `config/`, which has an inline package doc instead).
- gofmt/vet/build: clean (see §2).
- 500-line cap (KB `programming.md`): respected — largest touched file `main.go` is 389 lines; all new files ≤ 88.

## 8. New gaps/contradictions introduced by Phase 1

- **bootstrap-vs-CI inconsistency** (HOUSE-1): doc says CI reuses bootstrap; ci.yml does not. The only real *contradiction* in the set.
- **stdout-only capture + first-match marker** (FIX-1, FIX-2): not contradictions with prior docs, but latent defects in new code.
- No *blocking* contradiction with any ADR; ADR-0009 §2 / ADR-0012 §4 are accurately reflected.

## 9. Conclusion

Phase 1 delivers what ADR-0012 §4 promised: dagmar's first earned capability as an always-Dagger gate-family, manifest=what/gate=how, reused in CI. The build is clean, CI is wired correctly and will go green, the generated-files fix is right and self-protecting, and ADR/CONTEXT coherence is strong. **Verdict: SOUND.** Before Phase 2 reuses `dagmar-gate` in-loop, address the two `[FIX]` items (stderr capture, last-match marker) — they do not affect Phase-1 fail/red behavior but directly govern the diagnostic quality the coder will depend on — and close the `[GAP]` test coverage for `ParseManifest`.
