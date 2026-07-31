# dagmar — domain model & ubiquitous language

dagmar is a (target: fully-autonomous) Dagger/Kubernetes-hybrid multi-agent coding
system, written in Go, that works on the owner's own repositories and their forks.
This document is the single source of truth for dagmar's domain vocabulary: every
issue, design, and code symbol uses these terms as defined here.

> Authoritative for the domain model resolved in seeds **dagmar-4271**. Spoken
> language is German; all persisted writing (this file, issues, code, comments) is
> English.

## The layered language model

dagmar's ubiquitous language has three tiers. **Terms are coined only in Tier C.**
Tier A is reused by reference; Tier B is consumed behind adapter ports.

| Tier | Origin | Treatment |
|------|--------|-----------|
| **A — Dagger** | Dagger primitives | reused by reference; names never re-coined |
| **B — os-eco** | seeds / mulch / canopy | adapter ports; os-eco tool names appear only in adapter implementations |
| **C — dagmar core** | dagmar's own | where we coin terms |

**Rule:** never coin a Tier-C term for something Dagger already names (Tier A). If a
concept is Dagger's, reference it; if it is an os-eco service, adapt it behind a port.
See ADR-0001.

## Glossary

### Tier A — Dagger (reused, not re-coined)

- **Engine** — the hermetic Dagger execution engine. The resource that provisions and
  contains Sandboxes (`Engine ⊃ Sandbox`). dagmar runs an in-cluster instance but
  treats the Engine as a Tier-A resource. _(Engine cardinality/tenancy — singleton vs
  per-Project, cross-Project isolation — is an open execution decision; see ADR-0004.)_
- **LLM** — Dagger's LLM primitive (`dag.LLM()`); the cognition provider. dagmar does
  not reimplement cognition.
- **Env** — Dagger environment bundling inputs/outputs/tools for an LLM (`dag.Env()`).
- **CodeWorkspace** — `CodeWorkspace(source, checkable)`; the Tier-A projection of a
  dagmar Workspace.
- **checkable** — Dagger's mechanical self-verification (build/test/lint) running
  in-loop. Defined per-project (in the ProjectManifest) and reused both in-loop (agent
  self-verifies while working) and as the mechanical layer of the QualityGate.
- **Loop** — `dag.LLM().WithEnv(env).WithPromptFile(prompt).Loop()`; the agent
  cognition loop. A Run drives exactly one Loop.
- **TokenUsage** — Dagger's cost observability (`agent.TokenUsage()`).
- **Tool** — Dagger configuration: what an agent may call (`dag.git` / `container` /
  `http` plus os-eco adapter exposures). dagmar coins no Tool type; an Agent's
  permitted tools are its `tool-set` field.

### Tier B — os-eco backing services (adapter ports, bound per-Project)

All three are bound **per-Project** (N+1 contexts: dagmar-own + each target Project —
dogfooding).

- **IssueTracker** (→ seeds) — create/read/update/close issues (= Tasks); manage
  dependencies and plans. Canonical work handle.
- **Memory** (→ mulch) — read/write project expertise (conventions, patterns,
  failures, decisions); per-Project recall.
- **Prompts** (→ canopy) — compose a prompt from a Prompt spec (base sources +
  project-JSON enrichment).

### Tier C — dagmar core

**CRDs (declarative: definitions / policy / registration / observable execution):**

- **Project** (CRD) — a registered, repo-backed repository dagmar operates on (own
  repo or fork). Carries dagmar-side config (checkable source, os-eco binding,
  credentials, autonomy level) and references the repo's ProjectManifest.
- **Agent** (CRD) — a durable role/persona (coder, reviewer, researcher, …): model +
  Prompt ref + tool-set + checkable + autonomy scope. Materialized as Runs.
- **Prompt** (CRD) — a composition **spec** (not text): base canopy sources +
  project-JSON enrichment binding. canopy resolves it per-Project at run time into the
  final prompt passed to `dag.LLM().WithPromptFile(...)`.
- **QualityGate** (CRD) — the policy deciding whether a candidate change may advance.
  Composes: checkables (mechanical) + ReviewAgent review (cognitive) + autonomy/merge
  rules. Per-Project / autonomy-level; codebase-evolving.
- **Trigger** (CRD) — declarative event source that creates Tasks. Reactive (GitHub
  webhooks) or proactive (cron housekeeping). Bound to a Project + Agent /
  event-mapping.
- **Run** (CRD) — one execution of an Agent, in one Sandbox, on one Workspace. The
  observable, reconciled execution unit; carries status, token usage, outcome.

**Non-CR entities (canonical elsewhere or runtime artifacts):**

- **Task** — a unit of work on exactly one Project; ≡ one seeds issue (1:1, canonical).
  Lifecycle: created → Runs → resolved. Spawns 1..N Runs.
- **Sandbox** — an isolated execution slot subordinate to the Engine
  (`Engine ⊃ Sandbox`); the credentialed, resource-bounded pod + engine-session an
  Agent process runs in. `1 Run : 1 Sandbox`.
- **Workspace** — a task-scoped, Run-isolated clone of a Project on a branch + its
  checkable; handed to Dagger as a CodeWorkspace. Strictly isolated per Run (no shared
  clone — avoids file-change collisions); Workspace lineage across a Task's Runs
  (Run-out → next Run-in). Final output → diff → PR.

**In-repo manifest (not a CRD):**

- **ProjectManifest** — the in-repo conformance contract each Project exposes:
  project-specific `checkables` + os-eco binding + prompt-enrichment JSON + repo/flow
  metadata. Git-native, versioned with the code; the Project CR references it. Grows
  via dogfooding. _(Concrete path/format and the `checkable-source` projection are not
  yet pinned — tracked as ProjectManifest spec v0.)_

**Roles (Agent specializations, not separate types):**

- **ReviewAgent** — an Agent role that cognitively reviews another Run's output; the
  cognitive layer of the QualityGate.
- **planner / architect** — an Agent role that decomposes work into seeds issues
  (future).

**Explicitly not dagmar types:**

- **Plan** — not a Tier-C type (current). A seeds / IssueTracker concept. Planning is
  done locally (Wayfinder), translated into seeds issues, then agents work the issues.
  (Future direction — human-facing Plan-Agent via a chat interface — recorded in mulch.)

**External concepts (referenced, not part of the runtime model):**

- **Wayfinder** — the human planning process used to chart dagmar's direction early on
  (e.g. the `dagmar-80dd` wayfinder map in seeds). Planning with Wayfinder produces
  seeds issues that dagmar agents then work. Not a dagmar Tier-C type.

## Key relationships

- **Execution quartet:** a Run is the product of `{Agent, Sandbox, Workspace}` — Run
  itself is the fourth member. `Agent 1:N Runs` (a role is materialized as many Runs);
  `Task 1:N Runs`, each `Run 1:1 Sandbox`, all under one Engine.
- **Work hierarchy:** `Project 1:N Tasks`; `Task ≡ 1 seeds issue`; `Task 1:N Runs`.
- **Workspace:** per-Task base ref; per-Run isolated clone with in/out lineage.
- **Gating flow:** coder-Run → candidate → QualityGate (checkables + ReviewAgent) →
  `{auto-merge | revise | escalate | reject}`.
- **Trigger flow:** Trigger → seeds issue → Task → Run(s).
- **os-eco:** per-Project ports; N+1 contexts.
- **Two paths to seeds:** the controller observes seeds directly (scheduling/state);
  agents use the IssueTracker adapter (CRUD issues). Complementary, not redundant.

## CRD boundary

CRDs: `{Project, Agent, Prompt, QualityGate, Trigger, Run}`. Non-CR:
`{Task, Sandbox, Workspace, ProjectManifest}`. The boundary follows each entity's state
property (declarative/reconciled → CR; canonical-elsewhere or runtime → not) — see
**ADR-0002** for the full table and rationale.

## CRD fields → Dagger construction (bridge)

How an `Agent`'s CRD fields project onto the Tier-A `Loop` construction (detailed under
seeds `dagmar-3684`, Go module layout & hex arch):

| Agent CRD field | Tier-A target |
|-----------------|---------------|
| `model` | `dag.LLM(LLMOpts{Model})` |
| `prompt` ref → canopy resolve | `.WithPromptFile(...)` |
| `tool-set` | tools on `dag.Env()` |
| `checkable` | `CodeWorkspace(source, checkable)` |

## Open questions (tracked, not yet decided)

- **Autonomy model** — discrete levels + which entity (Project / Agent / QualityGate) is
  authoritative and precedence on conflict. _(ADR pending; overlaps `dagmar-e95b`.)_
- **Credentials & secrets** — storage, per-Project scoping, injection into Sandbox /
  `dag.Env()`. _(ADR pending.)_
- **Engine tenancy & Run concurrency** — singleton vs per-Project Engine; Sandbox
  isolation/quotas; concurrent Runs on one Task; who sequences Workspace lineage.
  _(Open sub-questions of ADR-0004.)_
- **Prompt pipeline data shapes** — Prompt spec / enrichment JSON / resolved prompt.
  _(Deferred to prompt/quality-gate work.)_

## Architectural decisions

See `docs/adr/`:

- **ADR-0001** — Layered ubiquitous language (Tier A/B/C)
- **ADR-0002** — Kubernetes CRD boundary
- **ADR-0003** — Project conformance via in-repo ProjectManifest
- **ADR-0004** — Execution topology (Hybrid-C)
