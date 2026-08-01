# ADR-0007: Credentials & secret management

- **Status:** decided
- **Date:** 2026-08-01
- **Resolved in:** seeds dagmar-4c9f
- **Surfaced by:** foundations review C2

## Context

dagmar targets autonomy on forks, so secret handling is security-critical. The domain
model already leans on credentials — `CONTEXT.md` gives `Project` a "credentials" slot,
calls the `Sandbox` "credentialed", and binds the Tier B os-eco services (IssueTracker /
Memory / Prompts) per-Project — but **storage**, **per-Project scoping**, and
**injection** into the `Sandbox` / `dag.Env()` were undecided (foundations review **C2**).
This ADR closes that gap. It does not reopen any prior decision; it composes with:

- **ADR-0002** — CRD boundary; the controller reconciles, seeds is the work source of truth.
- **ADR-0004** — Hybrid-C: an in-cluster Dagger engine; agent pods reach it via
  `_EXPERIMENTAL_DAGGER_RUNNER_HOST=kube-pod://`; per-Project engine tenancy itself stays
  open (seed `dagmar-cbb8`), but the pod topology is fixed.
- **ADR-0005** — prompts are composed cross-store; credentials are never prompt content.
- **ADR-0006** — merge is a deterministic controller function; the merge tool is in no
  Agent's tool-set.

Hard requirement, restated from C2: **a Project must never read another Project's
credentials.**

## Decision

### 1. Scoping boundary = per-Project Kubernetes namespace

Each `Project` reconciles into a dedicated namespace. All of a Project's Secrets, its CRs
(`Run`, `Trigger`, …), and its `Sandbox` pods live there. Enforcement is two-layered:

- **Kubernetes RBAC** — dagmar's per-Project service accounts (and the controller's
  secret-read) are namespace-scoped; there is no grant to read Secrets in another
  Project's namespace.
- **Controller discipline** — the controller never materializes another Project's secret
  into a `Sandbox`.

The namespace is the hard, auditable trust boundary. The dogfooding Project ("dagmar-own")
gets its own namespace like any target Project.

### 2. Three typed secret classes, stored as native `Secret`s in the Project namespace

- **`vcs`** — VCS/git credentials (clone + push) for the Project's repo or fork; consumed
  by `Workspace` clone and PR push.
- **`os-eco`** — per-Project Tier B tokens (IssueTracker → seeds, Memory → mulch,
  Prompts → canopy); one os-eco binding per Project (N+1, including dagmar-own).
- **`llm`** — LLM provider key(s) for `dag.LLM()`.

Typing (rather than one blob) makes least-privilege projection and audit trivial: each
class is named, projectable, and revocable independently.

### 3. Source of truth = External Secrets Operator (ESO); runtime contract = native `Secret`

ESO syncs from an external backend (Vault / cloud Secrets Manager) into the native
`Secret`s above. The native `Secret` is the **only** object the controller and `Sandbox`
consume — ESO is purely how those `Secret`s are populated and rotated. Baseline (before
ESO is deployed): the controller writes native `Secret`s from admin-supplied values; the
contract the system depends on is identical, so ESO is an **operational upgrade**, not an
architectural dependency.

### 4. Encryption-at-rest is required

Native `Secret` values are base64 in etcd — that is encoding, not encryption. A production
deployment must enable etcd encryption-at-rest (or a KMS encryption provider). This is a
deployment prerequisite, not deferred.

### 5. Injection = Project → `Sandbox` → `dag.Env()`, least-privilege, per-`Run`

The controller projects **only the class(es) a given `Run` needs** into the `Sandbox` pod
(env vars / projected `Secret`):

- a read-only review `Run` receives `vcs` read but **no push token**;
- `os-eco` tokens are projected scoped to the Project's binding;
- the `llm` key is projected only when the Agent's role invokes `dag.LLM()`.

The `Sandbox` pod sets `_EXPERIMENTAL_DAGGER_RUNNER_HOST=kube-pod://` (ADR-0004) and
forwards the allowed env into `dag.Env()` — the LLM key as the model secret, tool tokens
as tool env. The Dagger engine sandbox inherits **only what was projected**; nothing else.

Two invariants carry over from prior decisions:

- **Exclusions (ADR-0006):** the merge tool is in no Agent's tool-set. Merge is a
  deterministic controller function that exercises the `vcs` push credential at
  controller level — never inside a `Sandbox`. Agents therefore never hold push authority.
- **Prompt boundary (ADR-0005):** secrets are runtime environment, **never** prompt or
  canopy content. Cross-store prompt composition never touches credentials.

## Alternatives considered

- **Storage — Sealed Secrets.** Git-storable (fits dagmar's git-native ethos), but the
  decrypting controller key is cluster-scoped, which weakens per-Project rotation and
  isolation. Rejected for a multi-Project autonomous system.
- **Storage — native `Secret` without ESO.** In-cluster source of truth, manual rotation.
  Rejected as the production path; retained only as the pre-ESO baseline (§3).
- **Scoping — label/RBAC only, no namespace isolation.** Rejected; the namespace is the
  hard, auditable boundary Kubernetes provides, and ADR-0004's per-Project pod topology
  already pulls this way.
- **Scoping — a single shared namespace.** Rejected; it directly violates "a Project must
  not read another's credentials".
- **Injection — credentials baked into the agent image.** Rejected (non-hermetic; leaks
  across `Run`s).
- **Injection — mount all Project secrets into every `Sandbox`.** Rejected; violates
  least-privilege and the per-`Run` need-basis above.

## Consequences

- **Glossary:** `Project` "credentials" = the three typed classes (ADR-0007); `Sandbox`
  "credentialed" = the per-`Run` projected subset actually mounted into that `Sandbox`;
  Tier B os-eco bindings resolve their tokens from the Project's `os-eco` secret.
- **ADR-0004 inherits:** the per-Project namespace topology and the `Sandbox`
  env-projection model are now fixed inputs to engine-tenancy / concurrency work
  (seed `dagmar-cbb8`).
- **ADR-0006 preserved:** merge stays controller-level; the `llm` key never enters an
  Agent tool-set.
- **Operations:** ESO + an external backend and etcd encryption-at-rest become deployment
  prerequisites for production autonomy. Without them dagmar runs in the baseline
  (controller-managed native `Secret`s) but cannot meet the rotation/isolation bar for
  full autonomy.
- **Deferred sub-questions** (filed, not gating — mirrors the ADR-0004 → `cbb8` pattern):
  exact RBAC `RoleBinding`s / service-account-per-Project model; per-`Run` short-lived
  credentials (workload identity / projected ServiceAccount tokens / token-request API);
  rotation cadence and lease lifetime; concrete ESO backend selection. These are
  implementation/operational and do not reopen this decision.
