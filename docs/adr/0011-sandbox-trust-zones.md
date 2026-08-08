# ADR-0011: Sandbox trust-zones — hermetic LLM via tailored tool-sets (calculated risk)

Date: 2026-08-03
Seed: dagmar-911b (part of dagmar-80dd) · Status: **ACCEPTED**
Reviewed: `docs/review/07-2026-08-02-a1441a5-911b.md` (bell draft → rejected),
`docs/review/08-2026-08-03-4206601-911b.md` (this decision → accepted with sharpening N1–N6).

## Context

Spun out of ADR-0009 §3, which asserted the LLM loop runs hermetically *"enforced via the
tool-set"* but left two things open: (1) the resolved **per-tool boundary** (which
network-capable tools a hermetic agent withholds), and (2) whether hermeticity needs a
**harder** mechanism than the tool-set (e.g. network segmentation). This ADR resolves both.

**Empirical input (`ProbeNet` spike, `.dagger/main.go`):** a Dagger container exec has
**outbound network by default**, and Dagger v0.21.8 exposes **no per-exec no-network flag**
(`ContainerWithExecOpts` has no network/egress field). `dagger call probe-net` fetched
`https://example.com` from inside a container exec (`HTTP_FETCH_EXIT=0`). So tool-set
exclusion is **not a hard network guarantee** — there is a residual path (a raw exec: the
in-loop checkable, a build step) that an injected instruction could in principle use to reach
the network regardless of which `dag.*` tools are on the agent's `Env`.

An earlier draft of this ADR proposed a **"bell"** — two cluster-level Dagger engines by
trust-zone (`engine-hermetic` with a deny-all-except-provider NetworkPolicy vs.
`engine-networked`), enforcing hermeticity at the network layer independent of the tool-set.
That draft was reviewed (`docs/review/07-…`) and **rejected** (see Alternatives): the bell's
harder isolation came at operational cost that did not pay for itself.

## Decision

Hermeticity of the LLM loop is enforced via **per-use-case tailored tool-sets (least
privilege)**, on **one singleton engine** (ADR-0008, unchanged — no bell, no re-open). The
LLM primitive's safety IS the tailored tool-set; the residual network risk (ProbeNet) is
consciously accepted as a **calculated risk**.

### 1. One engine (ADR-0008 unchanged)

dagmar runs **one** cluster-level singleton Dagger engine shared across all Projects, exactly
as ADR-0008 decided. There is **no** trust-zone-based engine split, no second engine, no
per-Run engine routing. ADR-0008 is not re-opened.

### 2. Hermeticity = tailored tool-sets (least privilege)

> **Terminology (review N2):** in dagmar, a "hermetic" agent means its tool-set carries **no
> network-capable tool** — a *tool-surface* constraint. It is **not** a network-level air-gap
> (there is no per-exec no-network mechanism; see §3). "Hermetic" = the LLM is not *handed* a
> tool it can call to reach the network, not that no network path exists anywhere in the
> Sandbox. Readers must not over-trust "hermetic" as "cannot reach the network under any path."

**Criterion — which agents are hermetic (review N3):** the **cognitive LLM-loop agents**
(coder, ReviewAgent — the agents that process untrusted content and drive a
`dag.LLM().Loop()`) are hermetic. The **deterministic infrastructure steps**
(`dagmar-bootstrap`, `dagmar-gate`) are **not** hermetic — they need network (dep install,
registry pulls). This mirrors ADR-0009 §3's two profiles and is the rule applied when
materializing an Agent CRD `tool-set`.

A hermetic agent's `Env` withholds the **network-capable tools wholesale**:

- **`http`** — always network; withheld entirely.
- **`git`** — Dagger's `dag.git` is *remote*-git (clone/fetch/push to a URL); withheld. (A
  local `git` inside a workspace would run via `container` exec — moot, see next.)
- **the entire `container` tool** — because `container.WithExec()` always has outbound network
  (ProbeNet); there is **no "container minus egress" lever** (review N6), so the tool is
  withheld as a whole, not just "its egress." A hermetic agent therefore cannot run local
  containers; that is acceptable — hermetic agents are LLM-loop agents, not container-runners.

Tools are **tailored per use case** — the agent gets the minimum its role requires, nothing
more. (The `merge` tool is in **no** Agent's tool-set, ADR-0006.)

> **Carve-out — named Dagger functions ≠ the raw `container` tool (ADR-0012 §4).** The withheld
> tool is the **raw `container`** tool. A **named Dagger function** the agent calls in-loop —
> e.g. the checkable wrapper `dagmar-gate`, which internally container-execs build/test/lint —
> is NOT the raw `container` tool and is therefore NOT withheld. Its container-exec network
> residual is the §3-accepted residual; the gate body is project-declared (ADR-0017: in-code
> `dagmar-gate` Go logic, formerly manifest-declared per ADR-0003)
> and gate-reviewed, and the in-loop run uses a pinned ref, so an injected agent cannot redirect
> it to arbitrary code. Hermetic coders therefore DO run `dagmar-gate` in-loop (self-verification
> preserved, CONTEXT.md).

### 3. The ProbeNet residual risk is a calculated, accepted risk

Tool-set exclusion is **not** a hard network guarantee: a container exec has outbound network
by default (ProbeNet), and an injected instruction could in principle reach the network via a
raw exec path (the in-loop checkable, a build step) — independent of the `Env` tool-set. We
**accept** this residual, deliberately.

**Threat model — explicit scope (review N1).** "Calculated" is honest only if the scope is
named:

- **In scope, and controlled — the LLM primitive exfiltrating via its own tools.**
  Prompt-injection → the agent calls a tool to leak. The tailored tool-set removes exactly the
  network-capable tools, and the per-Project `llm` credential key (ADR-0007) is in **no**
  Agent's tool-set — so the LLM can neither exfiltrate through the surface it is given nor leak
  its own cognition key. *(Primary-threat control = tool-set + key isolation.)*
- **In scope, and accepted as residual — the raw-exec path reaching the network.** The in-loop
  checkable / a build step can reach the network despite the tool-set. Its blast radius is
  bounded by ADR-0007's defense-in-depth — a per-Project namespace + a per-Run projected-secret
  subset — so an escape is confined to one Project's credentials. *(Residual bound =
  namespace + projection; distinct from the primary control above — review N4.)* The checkable
  is deterministic and declared in the ProjectManifest (ADR-0003); a Project that declares a
  checkable which curls out compromises only **its own** namespace — the gate *runs* the
  checkable but does not *inspect* its network behavior; the security boundary is the
  per-Project namespace, not the gate (review N5, closes the draft's Open §3c).
- **Out of scope (named, not addressed here) — a Dagger Sandbox isolation break.** A
  container/process escape that defeats Sandbox encapsulation itself on the shared engine. The
  rejected "bell" would have contained this at the network layer; under the tool-set model it
  is a lower-probability residual, still bounded by ADR-0007's namespace, accepted *as a
  separate residual* — it is not covered by the tool-set. Naming it keeps the acceptance from
  over-claiming completeness.

This is a deliberate **calculated risk**, not an oversight: harder isolation (the bell) was
considered and rejected (§Alternatives); the out-of-scope residual (Sandbox-isolation break)
is named rather than implied.

### 4. Per-tool boundary (resolves ADR-0009 §3's open question)

ADR-0009 §3 deferred the per-tool boundary to this ADR. It is now defined: **hermetic agents
withhold the network-capable tools wholesale** — `http`, `git` (remote operations), and the
**entire `container` tool** (review N6 — `container.WithExec()` always has network; there is
no egress-free sub-lever) — from their `Env`. The exact minimal set per Agent role is the
Agent CRD's `tool-set` field (CONTEXT.md). The mechanism class is the tool-set (as ADR-0009 §3
asserted); the surface is now pinned.

## Alternatives considered

- **The "bell" — two engines by trust-zone (earlier draft of this ADR).** A hermetic engine
  with a deny-all-except-provider NetworkPolicy vs a networked engine, enforcing hermeticity
  at the network layer. **Rejected** after `docs/review/07-…`: it provides harder isolation
  but at operational cost that does not justify the marginal gain over tailored tool-sets +
  ADR-0007 defense-in-depth:
  - **G1** — hermetic base-image resolution: deny-all-except-provider blocks registry pulls
    the hermetic sandbox/checkable needs (registries live only in the networked bell) → the
    hermetic bell is not operationally runnable without extra plumbing.
  - **G2** — cross-engine Workspace handoff: bootstrap (networked) → coder (hermetic) are
    different engine pods; the `Directory` does not transfer automatically, and the bell
    blocks the S3/GCS cache path (ADR-0004).
  - **G3** — checkable split: §8 of the bell draft forced in-loop (hermetic) and gate
    (networked) to use *different* checkables, contradicting CONTEXT.md's "reused checkable".
  - **G4** — DNS-exfiltration: NetworkPolicy is L3/L4; DNS tunneling stays open — the very
    exfiltration threat the bell meant to neutralize.

  The bell trades four real operational gaps for one residual risk (ProbeNet) that tailored
  tool-sets + ADR-0007 already bound. Not worth it. (Review 08 confirmed G1–G4 evaporate under
  the single-engine tool-set decision.)
- **Per-exec no-network flag in Dagger.** Not available in v0.21.8 (`ContainerWithExecOpts`
  has no network field); not a lever today. Revisit if Dagger adds one.
- **Singleton engine + image-baked deps (deny all egress).** Rejected — forces every Project
  to pre-bake all dependencies (heavy conformance burden); loses runtime dep resolution.
- **Egress proxy / provider-call-in-agent-pod.** Variants of the bell; same complexity class,
  rejected for the same reason.

## Consequences

- **ADR-0008: unchanged** — one singleton engine; no trust-zone split, no re-open.
- **ADR-0009 §3: resolved.** The tool-set IS the hermeticity mechanism (as asserted); the
  per-tool boundary is pinned (§4). Note (review N8): ADR-0009 §3's phrase "no network" is, in
  light of this ADR, a *tool-surface* statement (no network-capable tool on the `Env`), not a
  literal network air-gap — the precise term is "hermetic" as defined in §2.
- **CONTEXT.md:** `Sandbox` gains no trust-zone field; the **"hermetic" term is sharpened**
  (review N2 — see the §2 Terminology note, mirrored in the Tool glossary). `Tool` glossary
  otherwise unchanged (tool-set = capability).
- **ProbeNet:** retained as the **residual-risk evidence** — the fact that grounds the
  calculated-risk acceptance, not a call for a bell.
- **The calculated-risk acceptance is explicit** — scope named in §3 (incl. the out-of-scope
  Sandbox-isolation-break residual), so it is a conscious decision, not an oversight, and is
  not re-litigated without new threat information.
- **Deferred:** the exact per-Agent minimal tool-sets (Agent CRD `tool-set` values); a future
  hard-isolation mechanism if Dagger adds per-exec network control or the threat model changes.
