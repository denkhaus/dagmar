# ADR-0010: Go module layout & hexagonal architecture

Date: 2026-08-02
Seed: dagmar-3684 (part of dagmar-80dd) · Status: ACCEPTED

## Context

`dagmar-3684` asks: *What is dagmar's Go module/package layout, and how are SoC / DRY /
ports-and-adapters realized?* This is the skeleton all implementation lands in — the
domain core, application services, adapters (Dagger, Kubernetes, os-eco, LLM), config,
logging, observability, and the testing strategy.

Decided via grilling on 2026-08-02. Constraints inherited (do not re-litigate): the tiered
language (ADR-0001), the CRD set + non-CR entities (CONTEXT.md), Hybrid-C topology
(ADR-0004), engine tenancy (ADR-0008), credentials (ADR-0007), autonomy model (ADR-0006).

Two Go modules already existed: root `github.com/denkhaus/dagmar` (empty) and the Dagger
module `dagger/dagmar` under `.dagger/` (held the cbb8 engine-tenancy spike:
`Up`/`DeployEngine`/`Probe`). Dagger CLI v0.21.8.

## Decision

### 1. Module topology — logic in `.dagger/`, root deferred

All execution logic lives in the `.dagger/` module (`dagger/dagmar`) as packages under
`.dagger/internal/`. The root module `github.com/denkhaus/dagmar` stays **empty for now**;
it will host the future Kubernetes control plane (CRD types + controller) under its own
seed. dagmar is fundamentally a Dagger module — its domain is Dagger-native (a Sandbox *is*
a Container, a Workspace *is* a `CodeWorkspace`, a Run drives a `dag.LLM().Loop()`) — so a
separate "Dagger-free domain library" in root would be artificial over-engineering.

Verified fact: Go's `internal` rule is bound to the **module path**; `.dagger/` (path
`dagger/dagmar`) cannot import `github.com/denkhaus/dagmar/internal/...`. Within
`.dagger/internal/`, however, `internal` works normally (same module). The `.dagger` module
builds with `GOWORK=off` (a parent `go.work` excludes the `dagger/dagmar` sub-module).

### 2. k8s integration model — agent pods are Dagger clients; the constructor is the seam

Per ADR-0008, the controller (root, future) **orchestrates only**: it watches CRDs and
provisions, per Project, an agent pod + ServiceAccount + `pods/exec` RoleBinding + a distinct
cache-volume name. The **agent pod is a `kube-pod://` Dagger client** that invokes the dagmar
module against the singleton engine. There is **no Go-import coupling** between the controller
and the module. The `New` constructor (§5) is the per-Project binding seam: the agent pod
passes the Project source + os-eco paths at construction, and every method reuses them.

### 3. Tier-A policy — functional core, Tier A used directly

Dagger primitives (`dag.LLM`, `Container`, `CodeWorkspace`, `Loop`) are used **directly** in
`app/` — they are NOT wrapped behind ports (ADR-0001 "reuse by reference, never re-coin"; the
`Loop` API is too rich to abstract cleanly). Only **Tier B** (os-eco: seeds/mulch/canopy) gets
ports. The pure decision logic (gate/merge/prompt rules) lives in `domain/` with **zero Dagger
import** → maximally unit-testable. The reuse principle targets **composed** workflows/tools,
not individual primitives.

Sub-packages reach the Dagger client via `dagger.Connect()` (returns the `init()` singleton;
`package main` uses the local `dag` — same client).

### 4. Composed reusable units — `tools/` + `workflows/` in-module

Reusable composed tool units (`tools/`) and workflow units (`workflows/`: gate, bootstrap,
review) live as packages within `.dagger/internal/. They are extracted into separate Dagger
modules only when reuse crosses the dagmar boundary.

### 5. Module surface — `Dagmar` main object + `New` constructor

The module's main object is `Dagmar` (auto-named from the module). `New(project *dagger.Directory,
seeds, mulch, canopy string) *Dagmar` is the constructor and the per-Project binding seam (all
args optional, so infra/spike methods remain callable without a binding). Methods
(`Sandbox`, future `Run`/`Gate`/`Workspace`) delegate to `internal/app/`; custom return objects
(`*Sandbox`) form a chainable object graph.

### 6. v0 vertical — `Sandbox`

`Sandbox(image, workingDir)` constructs a pure `domain.SandboxSpec`, and `app.BuildSandbox`
validates it at the top of the binding (the functional-core contract, §3 — so no caller can
skip it; a malformed spec surfaces as `domain: SandboxSpec.Image is required`, not an opaque
engine error) before applying the Tier-A-direct `Container` binding and returning a chainable
`*Sandbox`. Proven end-to-end against the engine (exit 0). No LLM, no network beyond the
base-image pull.

### 7. Testing — three-tier + GoMock

- `domain/` — pure table-tests, no Dagger, fastest.
- `app/` port-logic — unit-tested against `mockgen`-generated mocks of the os-eco ports
  (`go.uber.org/mock`, per KB `guide.golang.testing`; in-package mocks via `ports/generate.go`).
  The tool is a tracked module dependency (`tools.go`, `//go:build tools`) and `mock_oseco.go`
  is committed, so a fresh checkout resolves it from the module cache — no live network at
  `go generate` time.
- `app/` Dagger-direct logic — `//go:build integration` via a real engine.
- Plus Dagger `// check` functions for module invariants.

DI = **plain constructor injection** (no `samber/do`); `New()` is the composition root.

### 8. Config / logging / observability — minimal-dependency path

- Logging: a `Logger` interface (KB `guide.golang.logging`) over stdlib `log/slog` (the KB's
  documented Go-1.26 minimal-dependency alternative), injected via constructors.
- Config: `envconfig`-style struct tags for runtime/env (the env resolver is not yet wired —
  tags are prospective until it lands); the **ProjectManifest** (`.dagmar/project.yaml`,
  ADR-0003) for declarative per-Project conformance. The two same-shape os-eco triples
  reconcile at runtime: the constructor-seam binding (`main.OsEcoBinding`, from the
  ProjectManifest) is authoritative for a Run's store paths; `config.OsEcoConfig` is the
  env-override view of the same triple.
- Observability: behind a `Tracer` port; default = Dagger otel + `TokenUsage` (already deps);
  Langfuse is a deferred opt-in adapter (KB pattern, `Enabled` toggle).

## Empirical v0.21.8 constraints (recorded — revisit on engine upgrade)

These surfaced while scaffolding and shaped the seam:

- **`*dagger.Workspace` as a constructor INPUT is unsupported** by v0.21.8 codegen (skipped).
  Use `*dagger.Directory` — the SDK's own Workspace representation (`Env.Workspace()` returns
  `*Directory`). `dag.CurrentWorkspace()` exists, but Workspace-as-input is newer.
- **Custom struct input types from a non-main package are rejected** ("cannot code-generate
  for foreign type SandboxSpec"). The main seam takes **primitives** and constructs the domain
  struct internally; `domain/` stays Dagger-free.
- **A custom struct as a constructor ARG is skipped.** os-eco paths are passed as primitive
  strings and grouped into the `OsEcoBinding` field internally.
- **`workdir` arg name collides** with `*dagger.Container`'s own `workdir` field in the CLI →
  renamed to `workingDir` (the domain field stays `Workdir`).

## Alternatives considered

- **Root hosts domain, `.dagger` delegates (original thin-delegate).** Rejected — the domain is
  inherently Dagger-native; a Dagger-free domain library is artificial over-engineering.
- **Strict hexagonal — port Tier A too.** Rejected — wraps Tier A (re-coining risk); the `Loop`
  API is too rich to abstract. Functional core achieves testability where it matters (decisions).
- **`samber/do` DI container (KB-primary).** Rejected for the Dagger module — small
  constructor-injected graph; `New()` is the composition root; a service locator adds no value.
- **`zap` + Langfuse now (KB-primary).** Rejected for now — the minimal-dependency path
  (`slog` + Dagger otel) fits a dep-light Dagger module; Langfuse needs host infra and is
  deferred behind the `Tracer` port.

## Consequences

- **Glossary:** `.dagger/internal/{domain,ports,adapters,app,tools,workflows,config,log}` = the
  skeleton; `Dagmar` main object + `New` constructor = the per-Project binding seam; functional
  core = `domain/` (pure) + `app/` (Tier-A-direct).
- **Landed:** 13 hand-written files + `mock_oseco.go` (generated, committed) + regenerated
  bindings; `go build`/`test`/`vet` green; `dagger develop` clean; the `Sandbox` vertical
  proven end-to-end (exit 0), and the functional-core validation gate proven on the live
  path (empty image → clean domain error).
- **Spike:** the cbb8 methods (`Up`/`DeployEngine`/`Probe`) remain on the `Dagmar` object; to be
  refactored into `workflows/bootstrap` later.
- **ADR links:** ADR-0001 (Tier A direct, Tier B ports) confirmed; ADR-0003 (ProjectManifest =
  declarative config); ADR-0005 (os-eco ports); ADR-0008 (constructor binds per-Project for the
  `kube-pod://` client flow).
- **Deferred:** `Run`/`Gate`/`Workspace` methods; os-eco adapter implementations (sd/ml/cn);
  Langfuse adapter; refactoring the spike into `workflows/bootstrap`; the root control plane
  (CRD types + controller) — its own seed.
