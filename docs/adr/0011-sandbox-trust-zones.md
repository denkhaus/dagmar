# ADR-0011: Sandbox trust-zones — hermetic LLM via tailored tool-sets (calculated risk)

Date: 2026-08-03
Seed: dagmar-911b (part of dagmar-80dd) · Status: **PROPOSED (draft — pending review)**

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
That draft was reviewed (`docs/review/07-2026-08-02-a1441a5-911b.md`) and **rejected** (see
Alternatives): the bell's harder isolation came at operational cost that did not pay for
itself.

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

A hermetic agent's `Env` carries **no network-capable tool**. The Tool glossary (CONTEXT.md)
lists `dag.git` / `container` / `http`; for a hermetic agent the tool-set withholds the
network-capable ones (`http`, `git` remote operations, `container` egress) and exposes only
what the use case needs. **Tools are tailored per use case** — the agent gets the minimum
tool-set its role requires, nothing more. (The `merge` tool is in **no** Agent's tool-set,
ADR-0006.)

### 3. The ProbeNet residual risk is a calculated, accepted risk

Tool-set exclusion is **not** a hard network guarantee: a container exec has outbound network
by default, and an injected instruction could in principle reach the network via a raw exec
path (the in-loop checkable, a build step) — independent of the `Env` tool-set. We **accept**
this residual risk, because:

- **The primary threat is controlled.** The threat model is *the LLM primitive itself
  exfiltrating via its own tools* (prompt-injection → the agent calls `http`/`git` to leak).
  The tailored tool-set removes exactly those tools from the hermetic agent's `Env`, so the
  LLM cannot exfiltrate through the tool-surface it is given.
- **The blast radius of any escape is bounded** by ADR-0007's defense-in-depth: per-Project
  namespace + a per-Run projected-secret subset + the `llm` key never in any tool-set. A
  Sandbox escape bounds to one Project's credentials.
- **The residual path is deterministic / project-controlled.** The in-loop checkable
  (build/test/lint) is declared in the ProjectManifest (ADR-0003) and reviewed at the gate;
  it is not arbitrary agent-authored shell.

This is a deliberate **calculated risk**, not an oversight: harder isolation (the bell) was
considered and rejected (§Alternatives).

### 4. Per-tool boundary (resolves ADR-0009 §3's open question)

ADR-0009 §3 deferred the per-tool boundary to this ADR. It is now defined: **hermetic agents
exclude the network-capable tools** (`http`, `git` remote ops, `container` egress) from their
`Env`; the exact minimal set per Agent role is the Agent CRD's `tool-set` field (CONTEXT.md).
The mechanism class is the tool-set (as ADR-0009 §3 asserted); the surface is now pinned.

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
  tool-sets + ADR-0007 already bound. Not worth it.
- **Per-exec no-network flag in Dagger.** Not available in v0.21.8 (`ContainerWithExecOpts`
  has no network field); not a lever today. Revisit if Dagger adds one.
- **Singleton engine + image-baked deps (deny all egress).** Rejected — forces every Project
  to pre-bake all dependencies (heavy conformance burden); loses runtime dep resolution.
- **Egress proxy / provider-call-in-agent-pod.** Variants of the bell; same complexity class,
  rejected for the same reason.

## Consequences

- **ADR-0008: unchanged** — one singleton engine; no trust-zone split, no re-open.
- **ADR-0009 §3: resolved** — the tool-set IS the hermeticity mechanism (as asserted); the
  per-tool boundary is now pinned (network-capable tools withheld from hermetic agents). The
  §3 forward-pointer to "the forthcoming trust-zone ADR" lands here.
- **CONTEXT.md: unchanged** — `Sandbox` gains no trust-zone field; `Tool` glossary unchanged
  (tool-set = capability, per ADR-0009 §3). No "two engines" wording to go stale (the H1
  housekeeping note from review 07 does not apply).
- **ProbeNet:** retained as the **residual-risk evidence** — the fact that grounds the
  calculated-risk acceptance, not a call for a bell.
- **The calculated-risk acceptance is explicit** — documented here so it is a conscious
  decision, not an oversight, and is not re-litigated without new threat information.
- **Deferred:** the exact per-Agent minimal tool-sets (Agent CRD `tool-set` values); a future
  hard-isolation mechanism if Dagger adds per-exec network control or the threat model changes.

## Open during review (this draft's derivations — please confirm or correct)

- **§3(b):** reliance on ADR-0007's blast-radius bound as the compensating control for the
  residual risk — is that the right framing of "calculated"?
- **§3(c):** "the checkable is deterministic / project-controlled" — does this adequately
  bound the raw-exec exfiltration path? A malicious Project could declare a checkable that
  curls out; is that accepted as that Project's own compromise, or does the gate need to
  inspect checkable changes?
- **§4 / §2:** the withheld-tools granularity — is "container egress" the right cut, or is
  the whole `container` tool withheld from hermetic agents (losing legitimate local-container
  use)?
