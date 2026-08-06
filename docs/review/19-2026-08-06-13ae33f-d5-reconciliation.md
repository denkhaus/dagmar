# Review 19 — ADR-0013 §2 reconciliation implementation (D5 conditions + D3 verify + D4 deferred), 2026-08-06

- **Date:** 2026-08-06
- **HEAD / artifact:** `13ae33f` — 2 commits (`3c76125` feat: controller D5 conditions + D3
  verify; `13ae33f` docs: ADR-0013 §2 D4 deferred + seed `dagmar-54c9`). Range reviewed:
  `f9e955b..HEAD`.
- **Scope:** the IMPLEMENTATION of ADR-0013 §2 (D3–D5), not the design — the design was
  review-15 (`docs/review/15-2026-08-04-2b35fad-67bc.md`, on `2b35fad`, PROPOSED → revised →
  ACCEPTED). This review checks that the controller code cashes out the accepted decision: the
  D5 condition taxonomy is correctly realized and is the source of truth (Phase derived), the
  pod-phase→condition mapping semantics are right, the status write is genuinely hardened, D3 is
  honestly verified (not over-claimed), and the D4 deferral is honest + the seed scopes the real
  fix.
- **Reviewer posture:** advice-only. No code, ADR, or seed changed; this document only. Reviewed
  directly (no sub-delegation — 200k context, spawn disabled in config). Each claim cross-checked
  against the cited source line.
- **Verdict:** **SOUND** — D5 is faithfully realized (pinned set `{Accepted,Progressing,Succeeded,
  Failed}`; Phase is correctly derived, not the source of truth; pod-phase→condition mapping is
  right including the fresh-pod `""`/Pending vs Running distinction; the conflict-hardened
  `patchStatus` is textbook Get→DeepCopy→mutate→MergeFrom with status-subresource scoping). D3 is
  honestly verified, not padded. D4's deferral conclusion is correct. Two GAPs (one present
  cross-Run identity hazard that the D4 note under-discloses — pre-existing, latent for single-Run
  Phase 0; one untested terminal path — the pod-Failed half of the distinction this review was
  asked to verify) and four low-severity HOUSE items (doc drift, dead code, two invariants).
  None blocks push for Phase-0 single-Run; GAP-1 belongs in `dagmar-54c9`'s scope.

> Advice-only. Independent review; each claim below was checked against the cited code, ADR, seed,
> and prior review 18 (format). Standards axis: ADR-0010 (Go layout), gofmt, comment density,
> test style.

---

## 1. Verification performed

Read in full: `api/v1alpha1/run_types.go`; `internal/controller/run_controller.go`;
`internal/controller/run_controller_test.go`; ADR-0013 §2 (`docs/adr/0013-kubernetes-control-
plane-design.md` L56–88, alternatives L221–224, deferred L255); the D4 deferral note (commit
`13ae33f` diff); the D5 code diff (commit `3c76125`); seed `dagmar-54c9` entry. `gofmt -l
internal/ api/` → clean. `go test ./internal/controller/` not re-run (commit claims 8 green;
static trace of each path confirms the assertions).

Confirmed against the cited sources:

- The pinned set written by the controller is exactly `{Accepted, Progressing, Succeeded, Failed}`
  — matches ADR-0013 §2 D5 (L86–88). No fifth type, no stray `Available` (removed: `conditionAvailable`
  → `statusPatchAttempts`, diff confirmed).
- `Status.Phase` is no longer authored directly from pod phase except as a derived value:
  `reconcileStatus` (L383) and `failRun` (L396) both set `s.Phase = phaseFromConditions(s.Conditions)`.
  The only hand-authored Phase is the transient engine-not-ready path (L141, `Pending`), which
  equals what `phaseFromConditions` would return (default branch) — consistent.
- `patchStatus` (L344–361) is genuinely status-only: `base := fresh.DeepCopy()` before `mutate`,
  so `client.MergeFrom(base)` diffs only the status mutation; AND it routes through
  `r.Status().Patch` (status subresource), which would ignore spec fields regardless. Double-safe. ✓
- Conflict retry re-Gets and re-runs the (deterministic) mutator each attempt — correct optimistic
  concurrency. 3 attempts occur. ✓
- D3: `For(&Run{}) + Owns(&corev1.Pod{})` (L437–439) + `ctrl.SetControllerReference` on the pod
  (L249) → pod phase transitions requeue the owning Run. `engineRequeueAfter` (10s, L48) is the
  only explicit `RequeueAfter`. No poll loop. Honestly verified. ✓
- D5 alternative "pod writes Run status back" (L222) honored: pod has no Run write RBAC; the
  controller mirrors pod→status. Least privilege intact.

---

## 2. Findings

### [GAP-1] The shared-named SA is controller-owner-ref'd to its *creating* Run — k8s GC breaks siblings TODAY, and the D4 note frames this as only a future-finalizer hazard

**Severity:** medium (latent for Phase-0 single-Run-per-namespace; real as soon as two Runs share
a namespace). **Files:** `internal/controller/run_controller.go:194–204`; ADR-0013 §2 D4 note
L73–84; seed `dagmar-54c9`.

In `ensureAgentIdentity`, the per-namespace `dagmar-agent` ServiceAccount is created with a
controller owner reference to the Run that first creates it:

```go
sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: agentSA, Namespace: run.Namespace}}
if err := r.Get(ctx, ..., sa); errors.IsNotFound(err) {
    if err := ctrl.SetControllerReference(run, sa, r.Scheme); err != nil { ... }   // L196
    if err := r.Create(ctx, sa); err != nil && !errors.IsAlreadyExists(err) { ... }
}
```

`SetControllerReference` sets `Controller: true`. So the shared-named SA is owned by **Run A**
(the first Run in the namespace). When Run A is deleted, k8s garbage collection cascades to the SA
(foreground/background GC per the controller ownerRef). The SA vanishes — and every **sibling
Run B** in that namespace that ran a pod as `ServiceAccountName: dagmar-agent` (L328) now references
a missing SA: new pods fail admission, and exec-into-engine (which uses the SA token bound by the
RoleBinding) breaks. This happens **today, with no finalizer**, purely via the owner-ref GC the
controller itself sets.

The D4 deferral note (commit `13ae33f`, ADR L73–84) frames the sibling-break as a *hypothetical
future* hazard: "A Run- or Project-level finalizer that deleted these shared-named resources
**would** break sibling Runs." That conclusion is correct — but it locates the harm in a finalizer
that does not yet exist, while the owner-ref GC realizes the same break right now. The note's
"provisioned per-Run but shared-named" framing omits that the SA is *also* lifecycle-bound to one
specific Run. The in-code comment at L190–191 acknowledges the engine-ns Role/RoleBinding as a
cleanup leak ("tracked in dagmar-67bc") but treats the SA owner-ref as unremarkable — i.e. the SA
*is* cleaned up on Run deletion, which is precisely the sibling-break.

**Recommendation (advice-only):** the identity-refactor seed `dagmar-54c9` should name this
explicitly — its current scope ("make identity per-Run-named, OR provision per-Project namespaces +
a `ProjectReconciler`") resolves the naming dimension but should also call out *removing or
re-scoping the SA controller owner-ref* as part of "safely per-Run-deletable identity." The D4
note would be fully honest with one clause: the SA is already owner-ref'd to one Run, so GC
already cascades — the finalizer is not the only sibling-break path. Not a merge blocker for
Phase 0 (smoke runs are single-Run per namespace), but it is the most material finding and the
note under-discloses it.

### [GAP-2] The pod-Failed terminal path (Accepted=True / Failed=True) is untested — the failRun-vs-pod-Failed distinction is asserted one-sided

**Severity:** low–medium (test coverage). **Files:** `internal/controller/run_controller_test.go`
(negative space); `internal/controller/run_controller.go:367–385`.

The distinction this review was explicitly asked to verify — *failRun = rejection*
(`Accepted=False`, `Failed=True`) **vs** *pod-Failed = ran-but-failed* (`Accepted=True`,
`Failed=True`, `Phase=Failed`, `AgentPodName` set) — is **implemented** correctly:
`reconcileStatus` always sets `Accepted=True` (L371) and sets `Failed=True` only on `PodFailed`
(L379–381), keeping `AgentPodName` (L370); `failRun` sets `Accepted=False` (L393) and never sets
`AgentPodName`. `phaseFromConditions` returns `Failed` for both (both have `Failed=True`), so k9s
sees `Failed` in both cases and a condition reader distinguishes via `Accepted`. Sensible and
correct.

But the test coverage is one-sided:
- `TestReconcile_WritesPinnedConditionSet` (L340–382) walks Pending→Running→**Succeeded** only.
- `TestReconcile_MirrorsSucceededPodPhase` (L105–137) covers Succeeded.
- The rejection tests (`EmptyModuleRef…`, `MissingGitCredentialsSecret…`) cover `failRun`
  (`Accepted=False`), and `EmptyModuleRef` even asserts `Accepted=False`/`Failed=True`/no
  `AgentPodName` (L173–177).

**No test sets the pod to `corev1.PodFailed`** and asserts the pod-Failed half: `Accepted=True`,
`Failed=True`, `Phase=Failed`, `AgentPodName` present. The branch at `reconcileStatus` L379–381
and the reason/message switch at L161 (`case corev1.PodFailed`) execute in production but are
verified by nothing. A regression that, say, flipped pod-Failed to set `Accepted=False` (collapsing
the distinction) would pass the current suite.

**Recommendation:** add a `setPodPhase(t, cl, podKey, corev1.PodFailed)` case asserting
`Accepted=True` + `Failed=True` + `Phase=Failed` + `AgentPodName != ""` — mirrors the Succeeded
test and closes the distinction. Cheap, high-value.

### [HOUSE-1] `Status.Phase` field doc still says "derived from the owned agent pod's phase" — pre-D5 framing, drifted from the const block

**Severity:** trivial (doc drift). **Files:** `api/v1alpha1/run_types.go:55–56` vs `:7–9`.

This commit updated the `RunPhase` const-block comment to "DERIVED from the condition set
(phaseFromConditions) … conditions are the source of truth (ADR-0013 D5)" (L7–9), but the
`RunStatus.Phase` *field* comment two scopes down was left at the old framing:

```go
// Phase is the high-level lifecycle phase (Pending|Running|Succeeded|Failed), derived from
// the owned agent pod's phase.                       // L55-56 — stale
```

After D5, Phase derives from the *condition set*, not directly from the pod phase. A reader
hitting the field first (the natural read order in a `kubectl explain`/IDE) gets the wrong model.
One-line fix: mirror the const comment ("derived from the condition set via phaseFromConditions").

### [HOUSE-2] `patchStatus` trailing "exhausted N retries" return is unreachable (dead code)

**Severity:** trivial (the retry still runs 3×; only the intended diagnostic never fires).
**Files:** `internal/controller/run_controller.go:344–361`.

Trace of the loop (`attempt = 0,1,2`):
- On a successful patch → `return nil` (L358).
- On conflict when `attempt < statusPatchAttempts-1` (attempts 0,1) → `continue` (L354).
- On conflict at the **last** iteration (`attempt == 2`) → the guard `attempt < 2` is false →
  falls through to `return fmt.Errorf("patch run status: %w", err)` (L356), surfacing the raw
  conflict.
- On any non-conflict error (any attempt) → L356.

Every path through the final iteration returns inside the loop body. The loop-exit return
`return fmt.Errorf("patch run status: exhausted %d conflict retries", statusPatchAttempts)`
(L360–361) is therefore dead — the "exhausted" message can never reach a caller. Functionally
harmless (3 attempts do occur and a conflict still surfaces as an error), but the error semantics
a reader infers from the tail ("retries exhausted") differ from reality ("last attempt
conflicted"). Either drop L360–361 or restructure (e.g. bound on `<=` and let the tail fire).

### [HOUSE-3] ADR-0013 D5 frames `Status.Phase` as "rejected," but the code retains it as a derived field — ADR/code drift

**Severity:** trivial (doc coherence). **Files:** ADR-0013 §2 L86–88 + alternative L224 vs
`api/v1alpha1/run_types.go:7–9`, `internal/controller/run_controller.go:405–418`.

ADR §2 alternative: "`Status.Phase` string (D5): rejected — loses reason/message/lastTransition"
(L224), and the D5 body (L86–88) lists only the condition set. The implementation instead
**retains** Phase as a read-optimized derived summary (conditions = source of truth; Phase derived
via `phaseFromConditions`). The code documents this well (the const comment, the `phaseFromConditions`
doc). The ADR never says Phase is kept-but-subordinated — a reader correlating ADR→code sees
"rejected" vs "retained-derived." Recommend one line in D5 ("Phase is retained as a derived
back-compat summary; conditions are authoritative") so the ADR matches the (good) decision in code.

### [HOUSE-4] `Succeeded`/`Failed` are only ever set True, never reset False — safe only under an unstated `RestartPolicy: Never` invariant

**Severity:** trivial for Phase 0 (note for Phase 2). **Files:** `internal/controller/run_controller.go:376–381`.

`reconcileStatus` sets `Succeeded=True` (L377) or `Failed=True` (L380) but never sets the
counterpart back to `False`. This is correct only because pod terminal phases are sticky under
`RestartPolicy: Never` (L327) — a pod cannot move Succeeded→Failed or vice-versa, so the two can
never both be True. Worth a one-line invariant note on `reconcileStatus`: Phase-2 pod
retry/recreation would need to actively clear the stale terminal condition, or both could read True
(and `phaseFromConditions` would silently prefer Succeeded, L409). Documenting the dependency on
`RestartPolicyNever` prevents a future regression here.

---

## 3. Positive coherence checks

- **Mapping semantics (D5):** correct and internally consistent.
  - fresh pod `""`/Pending → `Progressing=False`, `Phase=Pending` (default branch) — distinct from
    Running. ✓ (`reconcileStatus` L372–375, `phaseFromConditions` default L416)
  - Running → `Progressing=True`, `Phase=Running`. ✓
  - Succeeded → `Succeeded=True`, `Progressing=False`, `Phase=Succeeded`. ✓
  - Failed → `Failed=True`, `Progressing=False`, `Phase=Failed`. ✓
  - rejection → `Accepted=False`, `Failed=True`, `Progressing=False`, `Phase=Failed`, no pod. ✓
  - engine-not-ready (transient) → `Accepted=True`, `Progressing=False`, `Phase=Pending`, requeue. ✓
  `phaseFromConditions` precedence (Succeeded > Failed > Progressing > Pending) matches the
  controller's call sites in every path.
- **`failRun` return contract:** returns the `patchStatus` error; on success `ctrl.Result{}, nil`
  (no requeue) → terminal. A 3×-conflict failure propagates as an error and requeues with backoff,
  re-running idempotent validation — acceptable, and rare for status patches.
- **Standards:** gofmt clean; comment density matches the file (high, every block explained);
  RBAC markers complete (L63–72); test style consistent with the existing suite (fake-client +
  `assertCondition` helper). No drift from ADR-0010 layout.

---

## 4. Recommendation

**Pushable as-is for Phase-0 single-Run.** Before two Runs share a namespace (or before the
identity-refactor seed lands), address:

1. **[GAP-1]** Widen `dagmar-54c9`'s scope to explicitly cover the SA controller owner-ref (the
   present GC sibling-break), and add one clause to the D4 note acknowledging that GC — not just a
   future finalizer — already cascades.
2. **[GAP-2]** Add a pod-`Failed` assertion (Accepted=True/Failed=True/Phase=Failed/AgentPodName
   set) to close the rejection-vs-pod-Failed distinction.

The four HOUSE items (field-comment drift, dead `exhausted` return, ADR Phase wording, the
`RestartPolicyNever` invariant) are one-line fixes, none blocking.
