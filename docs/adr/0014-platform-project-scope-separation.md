# ADR-0014: Platform/project scope separation — gate-family module boundary

Date: 2026-08-05
Seed: dagmar-b8c2 (part of dagmar-80dd) · Status: **ACCEPTED**
Decided via grilling 2026-08-05; revised after dagmar-review 17
(`docs/review/17-2026-08-05-cb32310-b8c2.md`, NEEDS-WORK-light → revised: GAP-1 mixed-scope
`config` package split, SPEC-1 ADR-0010 §4 reconciliation, HOUSE-1/2 wording; then accepted).
Resolves the gate-family abstraction-layer item: dagmar
conflates platform-scope and project-scope code in one `.dagger` module, and the platform
shortcircuits the abstraction a normal project uses (it calls dagmar-the-project's gate-family
directly instead of by module-ref). Builds on ADR-0010 (Go module layout) and ADR-0009 §2 /
ADR-0012 §4 (gate-family is the project's conformance contract).

## Context

dagmar's `.dagger` module (ADR-0010) holds two scopes in one `Dagmar` struct + one module:

- **PLATFORM** — `Up`, `DeployEngine`, `Probe`, `ProbeNet`, `ProbeCache` (engine/cluster
  infrastructure) + `Sandbox` (the agent execution context). Plus the root module's K8s
  controller/CRDs — the runtime that EXECUTES projects.
- **PROJECT** — `DagmarBootstrap`, `DagmarGate` (+ `workflows.{Bootstrap,Gate}`) —
  dagmar-as-a-project's OWN gate-family conformance contract.

ADR-0009 §2 + ADR-0012 §4 establish `dagmar-bootstrap`/`dagmar-gate` as **PROJECT-scope**:
"a Project must be a Dagger module exposing `dagmar-bootstrap` + `dagmar-gate`," written in the
project's own Dagger-SDK language. The conflation means dagmar-the-platform calls
dagmar-the-project's gate-family **directly** (a shortcuit) instead of through the abstraction a
normal project uses — checkout the project repo → call the PROJECT's module's
`dagmar-bootstrap`/`dagmar-gate` by ref. **The abstraction layer does not exist yet.**

This surfaced during the gate-centralization work, where the mise toolchain rollout was hardcoded
into `.dagger/internal/workflows/bootstrap.go`: that code is correct as PROJECT-scope, but the
conflation made it read as platform-level. D1 is the foundational scope question that blocks the
invocation abstraction (D2), the dogfood path (D3), and the toolchain-rollout home (D4).

## Decision

### 1. Module boundary, not code boundary (Q1)

The `.dagger` execution module SPLITS into two Dagger modules on a **module boundary**: a
**project module** (dagmar-as-a-project's conformance contract — `dagmar-bootstrap`,
`dagmar-gate`, the `.dagmar/project.yaml` manifest) and a **platform module** (`Up`/
`DeployEngine`/`Probe`/… + `Sandbox`). The platform invokes the project module by ref; the
dogfood (dagmar-as-a-project) IS the real path, not a shortcuit.

A **code-only boundary** (separate packages within one module) is rejected: the platform would
still call its own functions, no addressable project module exists, and the abstraction (swap in
a different project's module) cannot be expressed — the shortcuit persists structurally. The
module boundary is the price of the abstraction being real.

**Coherence with ADR-0010 §4 (review-17 SPEC-1).** ADR-0010 §4 says reusable workflow units are
"extracted into separate Dagger modules **only when reuse crosses the dagmar boundary**." This ADR
extracts gate + bootstrap **now**, before any cross-boundary reuse. The triggers are orthogonal and
both hold: §4's trigger is **reuse** (don't fork a module until a second consumer exists); ADR-0014's
trigger is **scope** (the platform/project conflation must be severed to make the abstraction real).
Scope-separation is not a violation of §4's reuse-default — it is a different axis. (ADR-0010 §4
also names `review` as a third workflow; it stays prospective — a third home is decided when review
is real, not preempted here.)

### 2. Assignment (Q2)

| Function | Scope | Module |
|---|---|---|
| `Up`, `DeployEngine`, `Probe`, `ProbeNet`, `ProbeCache` | engine/cluster infra | PLATFORM |
| `Sandbox` | agent execution context (`1 Run : 1 Sandbox`) | PLATFORM |
| `DagmarBootstrap`, `DagmarGate` + `workflows.{Bootstrap,Gate}` | dagmar-as-a-project's gate-family conformance contract | PROJECT |
| `.dagmar/project.yaml` (manifest INSTANCE) | project conformance contract (data) | PROJECT — the schema/parser is PLATFORM authority, published as `manifest/` (see Consequences GAP-1) |

`Sandbox` is **platform**: it is the agent execution context the platform provisions per Run,
coupled to Run/Engine — distinct from the project-side container-exec the gate uses for
checkables (which runs via the Dagger primitive `dag.Container()` inside the project module).

### 3. Paths & addressing — symmetry of the hidden dirs (Q3)

- **Platform module** = `.dagger/` (keeps name + root `dagger.json` `source: .dagger`). Keeps
  `Up`/`DeployEngine`/`Probe`/`ProbeNet`/`ProbeCache` + `Sandbox`. Minimal churn.
- **Project module** = `.dagmar/` (becomes a **new Dagger module** with its own `dagger.json`;
  holds `dagmar-bootstrap`/`dagmar-gate` — moved from `.dagger/internal/workflows/` — PLUS the
  existing `project.yaml` manifest already there). One home for the project contract's code AND
  data.
- **Ref** (local): `-m .dagmar`. The project module is addressable, so the platform (and every
  caller) addresses it uniformly by ref.

The `.dagger/` = platform-Dagger, `.dagmar/` = project-Dagger symmetry is minimal-churn AND
co-locates the project contract's code+data. `.dagmar/` is already the manifest's home.

### 4. Invocation abstraction — direct + convention (Q4 / D2)

Direct invocation, no central wrapper this increment. Every caller (CI, in-loop) does
`-m <project-module-ref> call dagmar-bootstrap` then `dagmar-gate --source <source>`, uniformly
for every project. The "abstraction" is the **convention** (the ref + the `dagmar-bootstrap`/
`dagmar-gate` function names). Conformance (= the project exposes them) is by ADR + naming
convention now; a runtime conformance probe (the platform verifying the project module exposes
the functions before dispatch) is later hardening.

A central wrapper function is rejected: there are two callers today (CI, in-loop) with an
identical `-m <ref> call` pattern — no duplicated orchestration code to justify a wrapper. Build
the wrapper only when callers diverge or a conformance probe becomes economical.

### 5. Dogfood path — local ref now, published later (Q5 / D3)

Because `dagmar-gate`/`dagmar-bootstrap` live ONLY in `.dagmar/` (project module) per Q2, the
platform module (`.dagger/`) no longer has them — a `dagger call dagmar-gate` (no `-m`, hits the
platform module) **structurally fails**. The module boundary ENFORCES the **ref-based call** (not
conformance — Q4 leaves "does the project expose the functions at all?" to ADR + naming now, a
runtime probe later); the shortcuit is structurally impossible. The abstraction's *ref* is secured
by structure; its *conformance* is secured by convention (Q4).

The dogfood uses a **local ref now**: CI/in-loop call `-m .dagmar call dagmar-gate --source .`
(path in the checked-out repo). This exercises the layer fully (platform calls the project module
by ref, not its own function). The "deepest dogfood" — a published ref
`-m github.com/denkhaus/dagmar/.dagmar@<ref>` (subpath-ref), treating dagmar-as-a-project exactly
like an external project — is a later deepening, once dagmar is published and the
exactly-external experience is to be validated (then also the subpath-ref prototype). "Local now"
proves the **abstraction** (platform calls the project module by ref, shortcuit excluded); it does
**not** exercise the published subpath-ref mechanism, which is a different Dagger ref-resolver code
path and stays wholly unvalidated until that prototype (review-17 HOUSE-2). Published is later.

### 6. Toolchain-rollout home — project-specific, no platform default (Q6 / D4)

`dagmar-bootstrap` (with its mise toolchain rollout) lives in the PROJECT module (`.dagmar/`);
each project writes its own `dagmar-bootstrap` (its own toolchain setup — dagmar chooses
debian+mise; a Node project would write an npm setup). The platform calls only
`dagmar-bootstrap` (whatever the project defines) — **no platform default.** A shared "mise-based
bootstrap" helper is extracted only when multiple projects converge (ADR-0010 §4: "extracted only
when reuse crosses the boundary"). A platform-provided default would undermine project autonomy
over its bootstrap and is premature for a single project.

## Alternatives considered

- **Code boundary (Q1):** rejected — separation in name only; the platform still calls its own
  functions, the shortcuit persists structurally, the abstraction cannot be expressed.
- **`Sandbox` → project (Q2):** rejected — `Sandbox` is the agent execution context
  (platform-side, coupled to Run/Engine), not the project-side checkable container-exec.
- **Explicit dir names (`platform-dagger/` + `project-dagger/`) / project module elsewhere (Q3):**
  rejected — `.dagger/` + `.dagmar/` symmetry is minimal-churn AND co-locates the project
  contract's code+data; a project module elsewhere splits the manifest from its code.
- **Invocation wrapper (Q4):** rejected for now — two callers with an identical pattern; no
  duplicated orchestration to justify a central wrapper. Build when callers diverge or conformance
  probing is economical.
- **Published ref dogfood now (Q5):** deferred — the local ref already makes the abstraction real
  (shortcuit excluded); the published/subpath-ref deepening waits for dagmar publication + the
  exactly-external validation.
- **Platform-default toolchain rollout (Q6):** rejected — undermines project autonomy over its
  bootstrap; premature for one project; extract a shared helper only at real multi-project reuse.

## Consequences

- `.dagger/` gives up `DagmarBootstrap`/`DagmarGate` + `workflows.{Bootstrap,Gate}` (becomes
  platform-only: `Up`/`DeployEngine`/`Probe*`/`Sandbox`).
- `.dagmar/` becomes a Dagger module (`dagger.json` + the moved gate/bootstrap code + the existing
  `project.yaml`); addressable as `-m .dagmar`.
- CI, lefthook pre-push, and the in-loop agent self-verification all invoke
  `dagmar-bootstrap`/`dagmar-gate` via `-m .dagmar`, uniformly with any other project. The dogfood
  IS the real path.
- The held gate-centralization restructure (`.dagger/internal/workflows/{bootstrap,gate}.go` with
  the mise rollout + the `secrets` checkable) is **re-homed, not discarded**: the code moves to
  `.dagmar/`, the manifest's `secrets` checkable stays, the CI/lefthook invocation becomes
  `-m .dagmar call dagmar-gate`. The centralization (one PR-readiness check incl. secret scan) is
  preserved; its placement is corrected by this ADR.
- **The re-home is NOT a pure code move — `config` splits too (review-17 GAP-1).**
  `.dagger/internal/workflows/gate.go` imports `dagger/dagmar/internal/config`
  (`ParseManifest`/`ProjectManifest`/`Checkable`). Once `gate.go` moves to `.dagmar/` — a **new Go
  module** whose path is not `dagger/dagmar` — Go's `internal/` rule makes that import unresolved:
  `gate.go` will not compile against the platform's `internal/config`. The `config` package is itself
  **mixed-scope** — `manifest.go` (`ProjectManifest`/`Checkable`/`ParseManifest`, PROJECT conformance,
  ADR-0003) vs `config.go` (`OsEcoConfig`, PLATFORM runtime/env, ADR-0010 §8, prospective). So `config`
  does not travel wholesale: manifest parsing → `.dagmar/`, `OsEcoConfig` → stays in `.dagger/`.
  (`bootstrap.go` imports only the generated SDK + stdlib and travels clean.) The implementer of the
  deferred split must split `config` alongside the move, or hit the `internal/` wall mid-move.
  **RESOLVED (dagmar-a1e0, 2026-08-07):** the manifest's project-module home was a Dagger-forced
  interim — the manifest is PLATFORM authority and a user Project must not own its parsing. It is
  now a PUBLISHED shared library (`manifest/` → `github.com/denkhaus/dagmar/manifest`) that both
  `.dagger` (pins `ManifestContractVersion = manifest.Version`) and `.dagmar` (the gate parser)
  depend on by versioned `require`. Relocating it as a relative-replace cross-module dep FAILED
  (Dagger loads each module in source isolation; a relative sibling cannot resolve at load). The
  published `require` DOES resolve at load — proven end-to-end in the gate (dagmar-gate loads both
  modules) — because the dagmar repo is PUBLIC: proxy.golang.org fetches the contract over HTTPS
  (no auth) and the committed go.sum carries the hash, so no GOPRIVATE is needed (it is optional,
  only to bypass the sum.golang.org crawl of a freshly-published module). See
  `manifest/doc.go` for the full resolution finding. The project-local `.dagmar/internal/config`
  copy is deleted; the manifest is now genuinely platform-authority by construction (the shared
  library IS the contract).
- The platform's conformance contract becomes mechanical (does `.dagmar` expose
  `dagmar-bootstrap`/`dagmar-gate`?) rather than implicit.

## Deferred

> **ADR-0017 impact:** `Checkable`/`validateWorkdir` types are deprecated by ADR-0017 §5
> (checkables move into `dagmar-gate` code). The published `manifest/` library is retained but
> narrows to metadata types only. `ParseManifest` must be refactored to stop requiring checkables.

- **Published/subpath-ref dogfood** (`-m github.com/denkhaus/dagmar/.dagmar@<ref>`) — later, once
  dagmar is published; validates the exactly-external experience + the subpath-ref mechanism.
- **Manifest → published shared library (dagmar-a1e0)** — DONE (2026-08-07). Extracted into
  `manifest/` (`github.com/denkhaus/dagmar/manifest`); both `.dagger` and `.dagmar` depend on it by
  versioned `require`; the project-local `.dagmar/internal/config` copy is removed. The
  load-resolution question (relative `replace` failed; published `require` works once the repo is
  public) is resolved — see the Consequences GAP-1 update + `manifest/doc.go`.
- **Runtime conformance probe** — the platform verifying the project module exposes the
  gate-family functions before dispatch. Later hardening over the naming-convention contract.
- **Central invocation wrapper** — only if callers diverge or a probe becomes economical.
- **The actual module split (implementation)** — moving the code, creating `.dagmar/dagger.json`,
  wiring CI/lefthook to `-m .dagmar`. This ADR fixes the decision; the split is a follow-on
  implementation increment (the held restructure seeds it).
