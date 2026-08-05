# Review 16 — dagmar private-module dispatch + tooling (mise/lefthook/CI), 2026-08-05

- **Date:** 2026-08-05
- **HEAD / artifact:** `8a4257e` — 4 code commits (`80a0bba` feat: private-module dispatch, `dbaa85a` fix: git-creds recipe, `4209da4` chore: mise + lefthook, `8a4257e` ci: mise + betterleaks secrets job) + 2 mulch syncs
- **Scope:** three areas only — (1) controller credential path vs ADR-0007 / ADR-0013 §4 D10, (2) secret-hygiene story, (3) standards drift in the controller. Explicitly out of scope: mise/lefthook/CI YAML beyond secret-hygiene, re-reviewing ADR-0013 itself, re-litigating the #8805 mechanism decision.
- **Reviewer posture:** advice-only. No code or ADR changed; no seeds filed/closed/updated.
- **Verdict:** **SOUND** — the controller faithfully realizes the resolved D10 mechanism, the secret-hygiene claim holds across every path I checked, and the new code follows established repo conventions. Three low-severity HOUSE items are precision / defense-in-depth nits; none blocks push.

> Advice-only. This is an independent review; each claim below was cross-checked against the cited source code, ADR, and prior reviews.

---

## 1. Verification performed

Read in full: `internal/controller/run_controller.go` (validation step 3a + `agentPodFor` credential
path + RBAC markers + SHELL-INJECTION NOTE); `api/v1alpha1/project_types.go` (`GitCredentialsRef`
struct + `ProjectSpec` field); `docs/adr/0007-credentials-secret-management.md` (the two-layer
defense, injection model); `docs/adr/0013-kubernetes-control-plane-design.md` §4 D10 (resolved —
source, projection, delivery, invariant); `internal/controller/run_controller_test.go` (two new
tests); `Justfile` (`git-creds` recipe); `lefthook.yml`; `mise.toml`; `.github/workflows/ci.yml`;
`config/samples/dagmar_v1alpha1_project.yaml`; `config/rbac/role.yaml`. Skimmed:
`docs/review/15-2026-08-04-2b35fad-67bc.md` (prior-review baseline, the GAP-2/GAP-3 thread this
batch cashes out).

Confirmed against the cited sources:

- The controller's existence check at step 3a (`r.Get` on `corev1.Secret{}`) fetches the full
  Secret object but the code never accesses `.Data` — the PAT value is not read, logged, or stored
  in Run status.
- The PAT is projected into the agent pod via `secretKeyRef` (not a literal in the pod spec), and
  the credential helper is a fixed controller string (not an author-injectable field).
- The credential helper's `$DAGMAR_GIT_PAT` is single-quoted for the outer `sh -c`, so the variable
  reference is stored literally in gitconfig and expanded only when git invokes the helper at
  credential-fill time (inheriting the pod env).
- The Justfile `git-creds` recipe pipes `gopass show -o → tr -d '\n' → kubectl create …
  --from-file=token=/dev/stdin` — the PAT never appears on argv or in shell history.
- Betterleaks runs at two layers: pre-commit (`dir .` working-tree scan) and CI (`git .`
  full-history scan with `fetch-depth: 0`).
- The RBAC addition is `secrets:get` only — no `list`, `watch`, `create`, `update`, `patch`, or
  `delete`.

---

## 2. Findings

### [SPEC-1] Coherence with ADR-0013 §4 D10 (resolved) — CONFIRMED

**Severity:** n/a (positive coherence check). **Files:** `run_controller.go` (validation 3a +
`agentPodFor`); `project_types.go` (`GitCredentialsRef`).

ADR-0013 §4 D10 (resolved via spike `dagmar-2c68`, 2026-08-05) specifies four aspects. Each is
verified against the code:

| Aspect | ADR claim | Code |
|---|---|---|
| **Source** | `Project.spec.gitCredentialsRef` → `Secret` in the Project's namespace | `GitCredentialsRef{Name, Key}` on `ProjectSpec`; validation checks `run.Namespace` (the Project's namespace in Phase 0) ✓ |
| **Projection** | controller projects the Secret into the agent pod as an env var + configures a headless git credential helper | `agentPodFor`: `secretKeyRef` env `DAGMAR_GIT_PAT` + `git config --global credential.helper '!f() { … }'` ✓ |
| **Delivery** | engine queries the pod's `git credential fill`, receives the PAT, injects as session-scoped secret | the credential helper emits `username=dagmar` + `password=$DAGMAR_GIT_PAT` at fill time — this is the `#8805` client-side resolution path ✓ |
| **Invariant** | engine holds no standing cred; PAT resolved client-side, per-session, never persisted | the PAT lives only in the pod env + is emitted only when git invokes the helper; the engine receives it transiently via `git credential fill` and uses it as a Dagger secret for that fetch ✓ |

Review-15 GAP-2/GAP-3 flagged this path as sitting outside ADR-0007's two layers and spike-gated.
The spike resolved favorably, and this implementation cashes out the resolved mechanism exactly.
The ADR-0007 consistency tension (the path is a fourth credential consumer, not one of the three
Sandbox-projected classes) is a property of the *design*, already debated in review-15; the
*implementation* is faithful to the resolved design.

**Verdict:** CONFIRMED — the code realizes what the ADR claims.

### [SPEC-2] Secret-hygiene claim ("PAT never on argv/history, never committed, never in the control plane") — CONFIRMED

**Severity:** n/a (positive verification). **Files:** `Justfile`, `run_controller.go`, `ci.yml`,
`lefthook.yml`, `config/samples/dagmar_v1alpha1_project.yaml`.

Checked every path where the PAT value could surface:

1. **Argv / shell history:** the `git-creds` recipe pipes `gopass show -o {{key}} | tr -d '\n' |
   kubectl create secret generic … --from-file=token=/dev/stdin`. The token flows through stdin
   pipes — it never appears as a command-line argument to any process, so it cannot appear in
   `ps`, `/proc/*/cmdline`, or shell history. The `git-creds-key` variable holds the *gopass path*
   (`dev/dagmar/github_token`), not the PAT itself; the comment correctly states "the PATH is not
   secret — the value lives encrypted in gopass." ✓

2. **Pod spec / pod logs / pod events:** the PAT is injected via `secretKeyRef`
   (`ValueFrom.SecretKeyRef`), so the pod spec shows only the Secret name + key, not the value. The
   `sh -c` command string contains `$DAGMAR_GIT_PAT` as a literal variable reference (single-quoted
   for the outer shell), not the PAT value. `kubectl describe pod` shows the command with the
   variable name; the env var section shows `secretKeyRef`, not the resolved value. The credential
   helper writes the PAT to stdout, which git captures (not to pod logs). ✓

3. **Run status / controller logs:** `failRun` for `GitCredentialsSecretNotFound` includes
   `secretName` and `run.Namespace` — neither is the PAT value. The controller never accesses
   `.Data` on the Secret object. ✓

4. **Committed to git:** the sample YAML references `just git-creds` and names the Secret
   (`dagmar-git-creds`), never the PAT. Betterleaks scans both pre-commit (`dir .` working tree)
   and CI (`git .` full history with `fetch-depth: 0`). ✓

5. **Agent pod SA isolation:** the agent pod's ServiceAccount (`agentSA`) does not have
   `secrets:get` — only the controller's role does. So the pod cannot independently `kubectl get
   secret` to read the PAT; it receives the value only via kubelet-resolved env-var injection. ✓

**Verdict:** CONFIRMED — the claim holds across all paths.

### [HOUSE-1] "The PAT value never enters the controller" comment is literally imprecise — `r.Get` deserializes the full Secret including `.Data`

**Severity:** low (precision / defense-in-depth). **File:** `run_controller.go:107`.

The comment at step 3a reads:

> Existence check only: the PAT value never enters the controller (ADR-0007 — it flows pod→engine
> via the credential helper, not through the control plane).

The `r.Get(ctx, typesNamespacedName{…}, &corev1.Secret{})` call fetches the complete Secret object
from the API server. Controller-runtime's client deserializes the JSON response into the
`corev1.Secret{}` struct, including the `.Data` field (`map[string][]byte`), which contains the
base64-decoded PAT bytes. The controller's business logic never reads `.Data` — so the PAT is not
*processed*, *logged*, or *stored in status* — but the bytes do transit the network and live in the
controller process memory as part of the deserialized struct.

This is standard Kubernetes controller practice (the GET-and-check-NotFound pattern is ubiquitous)
and is not a real vulnerability. But "never enters the controller" is the kind of absolute claim a
security reviewer flags: it is true at the *business logic* level and false at the *process memory*
level.

**Recommendation:** soften to "the controller never reads or logs the PAT value" (precise and
accurate) or leave as-is (the intent is clear). Not actionable for push.

### [HOUSE-2] Credential helper `$DAGMAR_GIT_PAT` is unquoted in the echo + host-agnostic — two minor defense-in-depth nits

**Severity:** low (defense-in-depth, not a practical risk). **File:** `run_controller.go:278`.

The credential helper string:

```
git config --global credential.helper '!f() { echo username=dagmar; echo password=$DAGMAR_GIT_PAT; }; f'
```

Two observations:

1. **Unquoted expansion.** `$DAGMAR_GIT_PAT` is correctly single-quoted for the outer `sh -c`
   (so git stores the variable reference literally, not the value). But when git later invokes the
   helper via its own shell, `$DAGMAR_GIT_PAT` expands *unquoted*: `echo password=$DAGMAR_GIT_PAT`.
   If the PAT contained shell metacharacters (`$(…)`, backticks, spaces), they would be
   interpreted by that inner shell. GitHub fine-grained PATs are alphanumeric + underscore
   (`github_pat_…`), so this is not exploitable in practice. But `echo "password=$DAGMAR_GIT_PAT"`
   (double-quoted) would be strictly safer at zero cost — if the PAT were ever a different token
   type (e.g., a deploy key with special chars), the quoting would prevent a surprise.

2. **Host-agnostic.** The helper unconditionally emits credentials for any host git asks about —
   it does not inspect the `host=` input line from stdin. In Phase 0 the only git operation is
   fetching `github.com/denkhaus/dagmar` (the module ref), so the helper fires only for github.com
   in practice. And the PAT is a fine-grained token scoped to `contents:read` on one repo, so even
   if it were presented to a different host, it would be useless there. A host-checking helper
   (`[ "$host" = "github.com" ] && …`) would be marginally more disciplined, but adds shell
   complexity for no Phase-0 benefit.

**Recommendation:** wrap the expansion in double quotes (`echo "password=$DAGMAR_GIT_PAT"`) — a
one-character hardening. The host-agnosticism is fine for Phase 0; note it if the module-ref
surface broadens.

### [HOUSE-3] `GitCredentialsRef.Name` lacks a CRD validation marker — empty-name edge case causes indefinite requeue

**Severity:** low (privileged-author field, Phase 0). **File:** `project_types.go:21`;
`run_controller.go:109-117`.

The `Name` field on `GitCredentialsRef` has no `+kubebuilder:validation:Required` or
`+kubebuilder:validation:MinLength=1` marker:

```go
type GitCredentialsRef struct {
    Name string `json:"name"`       // no validation marker
    Key string `json:"key,omitempty"`
}
```

Compare: `ProjectSpec.Repo` has `+kubebuilder:validation:Required` (line 31). The asymmetry means
a Project with `gitCredentialsRef: { name: "" }` (or `{}`) passes CRD admission, then hits the
controller's nil-check (`project.Spec.GitCredentialsRef != nil` — true, the pointer is non-nil),
and attempts `r.Get(ctx, typesNamespacedName{Name: "", …}, &corev1.Secret{})`. An empty-name GET
returns a non-NotFound error from the API server (HTTP 400 / "resource name may not be empty"),
which falls through to `return ctrl.Result{}, err` — an indefinite requeue with no terminal signal.

The other validated fields (ModuleRef step 3, ModuleFunction step 3b) handle emptiness as a
terminal `failRun`. This new field does not, which is the asymmetry.

**Recommendation:** either add `// +kubebuilder:validation:Required` on `Name` (rejects empty at
admission) or add a controller-side `if secretName == ""` guard before the Get (terminal failRun,
symmetric with steps 3/3b). Not blocking — creating a Project is a privileged-author action — but
the inconsistency is worth closing.

---

## 3. Standards-drift assessment

The new controller code was checked against the conventions established in reviews 12–14 and the
existing `run_controller.go`:

| Convention | Status |
|---|---|
| Comment density / ADR cross-referencing | **Consistent** — every new block references its ADR/decision (ADR-0013 §4 D10, ADR-0007); the doc comment on `GitCredentialsRef` mirrors the established struct-doc style |
| SHELL-INJECTION NOTE pattern | **Consistent** — the existing note is extended with "(The git-credential additions below are fixed controller strings, not author fields.)", accurately distinguishing controller-fixed strings from author-injectable fields |
| Validation step pattern (3 / 3a / 3b) | **Consistent** — step 3a mirrors the `failRun` shape of steps 3 and 3b; same comment style, same terminal-vs-requeue split |
| Error handling (NotFound → terminal, other → requeue) | **Consistent** — `errors.IsNotFound` → `GitCredentialsSecretNotFound`; other → bare `return ctrl.Result{}, err` |
| RBAC markers | **Consistent** — `+kubebuilder:rbac:groups="",resources=secrets,verbs=get` follows the existing marker idiom; minimal (only `get`, not `list`/`watch`) |
| Test style | **Consistent** — two new tests use the existing explicit-setup + fake-client-builder pattern; one happy path (PAT projected, credential helper present, default-key exercised) + one failure path (missing Secret → terminal). Tests assert both the command-string content and the env-var projection mechanism. |
| Const naming | **Consistent** — `gitCredsEnvVar`, `gitCredsDefaultKey` follow the existing camelCase const convention |

One drift: `GitCredentialsRef.Name` validation asymmetry (HOUSE-3 above). Everything else is clean.

---

## 4. Conclusion

The private-module dispatch batch is **SOUND**. The controller code faithfully implements all four
aspects of the resolved ADR-0013 §4 D10 mechanism (SPEC-1): the Secret source, the env-var +
credential-helper projection, the `#8805` client-side delivery path, and the "no standing cred"
invariant. The secret-hygiene claim holds across every path I could find (SPEC-2): the PAT never
touches argv, shell history, pod specs, pod logs, Run status, controller logs, or git. The Justfile
gopass→kubectl pipe is clean; betterleaks is positioned at both pre-commit and CI layers. The new
controller code follows established repo conventions — comment density, SHELL-INJECTION NOTE
discipline, validation/error-handling patterns, test style, RBAC minimality. The three HOUSE items
are precision and defense-in-depth nits, none of which blocks push.

**Verdict: SOUND.** Push it. Close HOUSE-3 (validation marker) when convenient; the rest are
optional polish.

**Summary of findings:** 2 SPEC (both CONFIRMED), 3 HOUSE.
