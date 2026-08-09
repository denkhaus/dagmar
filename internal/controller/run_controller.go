package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/denkhaus/dagmar/api/v1alpha1"
	"github.com/denkhaus/dagmar/internal/prompt"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// engineLabelKey/Val select the singleton Dagger engine pod (cbb8 spike recipe, helm chart).
	engineLabelKey = "name"
	engineLabelVal = "dagger-dagger-helm-engine"
	// engineNamespace is where the singleton engine DaemonSet runs.
	engineNamespace = "dagmar"
	// defaultAgentPodImage is the Phase-0 default agent image (runtime-installs kubectl + dagger,
	// matching the cbb8 Probe recipe).
	defaultAgentPodImage = "alpine:3.20"
	// daggerVersion is the CLI version installed into the agent pod.
	daggerVersion = "0.21.8"
	// agentSA is the per-namespace ServiceAccount the agent pods run as (shared across Runs in a
	// namespace). It is granted pods/exec on the engine via agentRole/agentRoleBinding.
	agentSA = "dagmar-agent"
	// agentRole is the Role (in the engine namespace) granting the agent SA pods/exec on the
	// engine pod. Shared; created once.
	agentRole = "dagmar-agent-exec"

	// gitCredsEnvVar is the env var carrying the projected PAT into the agent pod; the headless
	// git credential helper (set in agentPodFor) reads it at credential-fill time (ADR-0013 §4 D10).
	gitCredsEnvVar = "DAGMAR_GIT_PAT"
	// gitCredsDefaultKey is the Secret key assumed when Project.Spec.GitCredentialsRef.Key is empty.
	gitCredsDefaultKey = "token"

	// engineRequeueAfter is the requeue delay when the engine pod is not yet Ready (transient —
	// the engine may still be rolling out at kind bringup).
	engineRequeueAfter = 10 * time.Second

	// statusPatchAttempts bounds conflict-retry on the status subresource patch (concurrent
	// reconcile — the pod watch + an engine requeue can race for one Run; ADR-0013 D5).
	statusPatchAttempts = 3
)

// RunReconciler reconciles a Run CR into an agent pod that calls the dagmar module, and writes
// the pod's outcome back as Run status. Phase 0: status conditions + phase only
// (ADR-0012 §2 SPEC-1).
type RunReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=dagmar.denkhaus.io,resources=runs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=dagmar.denkhaus.io,resources=runs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=dagmar.denkhaus.io,resources=runs/finalizers,verbs=update
// +kubebuilder:rbac:groups=dagmar.denkhaus.io,resources=agents,verbs=get;list;watch
// +kubebuilder:rbac:groups=dagmar.denkhaus.io,resources=workflows,verbs=get;list;watch
// +kubebuilder:rbac:groups=dagmar.denkhaus.io,resources=projects,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods/exec,verbs=create
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch

// Reconcile drives a Run: it resolves the Project + the singleton engine pod, ensures the agent
// identity + an agent pod that calls the module, and mirrors the pod's phase into Run status.
func (r *RunReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// 1. Fetch the Run; deleted Runs are nothing to do.
	run := &v1alpha1.Run{}
	if err := r.Get(ctx, req.NamespacedName, run); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// 2. Fetch the referenced Project. A missing Project is a config error (terminal).
	project := &v1alpha1.Project{}
	if err := r.Get(ctx, types.NamespacedName{Name: run.Spec.ProjectRef, Namespace: run.Namespace}, project); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, r.failRun(ctx, run, "ProjectNotFound",
				fmt.Sprintf("referenced Project %q not found", run.Spec.ProjectRef))
		}
		return ctrl.Result{}, err
	}

	// 3. ModuleRef is a load-bearing Phase-0 read (ADR-0012 §2 GAP-1): the agent pod must know
	// which module version to invoke. Empty → terminal config error.
	if project.Spec.ModuleRef == "" {
		return ctrl.Result{}, r.failRun(ctx, run, "ModuleRefRequired",
			"Project.Spec.ModuleRef is empty; it must name the dagmar module ref for `dagger call -m`")
	}

	// 3a. If the Project declares a GitCredentialsRef (private module ref, ADR-0013 §4 D10), the
	// named Secret must exist in the Run's namespace — the controller projects it into the agent
	// pod, and a missing Secret would leave the pod stuck Pending with no terminal signal.
	// Existence check only: the controller never reads or logs the PAT (ADR-0007 — it flows
	// pod→engine via the credential helper). The Get transiently deserializes the Secret object
	// but the code never inspects .Data.
	if project.Spec.GitCredentialsRef != nil {
		secretName := project.Spec.GitCredentialsRef.Name
		if err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: run.Namespace}, &corev1.Secret{}); err != nil {
			if errors.IsNotFound(err) {
				return ctrl.Result{}, r.failRun(ctx, run, "GitCredentialsSecretNotFound",
					fmt.Sprintf("Project.Spec.GitCredentialsRef names Secret %q not found in namespace %q", secretName, run.Namespace))
			}
			return ctrl.Result{}, err
		}
	}

	// 3b. Dual-mode validation (ADR-0016 §2): a Run must have exactly one of
	// ModuleFunction (atomic) or WorkflowRef (orchestration). Neither or both is a
	// terminal config error. For atomic Runs with an AgentRef, the controller reads
	// the Agent spec to configure the code() function call (model, maxAPICalls).
	hasFn := run.Spec.ModuleFunction != ""
	hasWf := run.Spec.WorkflowRef != ""
	if !hasFn && !hasWf {
		return ctrl.Result{}, r.failRun(ctx, run, "ModuleFunctionOrWorkflowRefRequired",
			"Run must set either ModuleFunction (atomic) or WorkflowRef (orchestration)")
	}
	if hasFn && hasWf {
		return ctrl.Result{}, r.failRun(ctx, run, "ModuleFunctionAndWorkflowRefMutuallyExclusive",
			"Run.Spec.ModuleFunction and WorkflowRef are mutually exclusive (ADR-0016 §2)")
	}

	// 3c. Orchestration Runs (WorkflowRef): drive the pipeline (ADR-0016 §4).
	// The controller creates and supervises atomic Sub-Runs. Atomic Runs (ModuleFunction)
	// fall through to the engine-pod + agent-pod path below.
	if hasWf {
		// Mark accepted, then orchestrate.
		_ = r.patchStatus(ctx, run, func(s *v1alpha1.RunStatus) {
			gen := run.Generation
			meta.SetStatusCondition(&s.Conditions, runCondition(v1alpha1.RunConditionAccepted, metav1.ConditionTrue, "OrchestrationMode", "orchestration Run accepted", gen))
		})
		return r.reconcileOrchestration(ctx, run, project)
	}

	// 3d. For atomic Runs with an AgentRef, read the Agent spec (model, maxAPICalls).
	// The controller passes these as module args to the code() function. A missing
	// Agent is a terminal config error (like a missing Project).
	var agentModel string
	var agentMaxAPICalls int
	var agentPrompt string
	if run.Spec.AgentRef != "" {
		agent := &v1alpha1.Agent{}
		if err := r.Get(ctx, types.NamespacedName{Name: run.Spec.AgentRef, Namespace: run.Namespace}, agent); err != nil {
			if errors.IsNotFound(err) {
				return ctrl.Result{}, r.failRun(ctx, run, "AgentNotFound",
					fmt.Sprintf("referenced Agent %q not found", run.Spec.AgentRef))
			}
			return ctrl.Result{}, err
		}
		agentModel = agent.Spec.Model
		agentMaxAPICalls = agent.Spec.MaxAPICalls
		agentPrompt = agent.Spec.Prompt.ProjectPrompt
	}

	// 4. Resolve the singleton engine pod (a Ready one). Transiently absent (engine still rolling
	// out) → requeue, not terminal.
	enginePod, err := r.enginePodName(ctx)
	if err != nil {
		logger.Info("engine pod not ready; requeueing", "err", err)
		// Transient: validation passed (Accepted=True) but the engine isn't Ready, so the Run is
		// not yet progressing. No pod exists yet → AgentPodName is not set. Requeue, not terminal.
		_ = r.patchStatus(ctx, run, func(s *v1alpha1.RunStatus) {
			gen := run.Generation
			meta.SetStatusCondition(&s.Conditions, runCondition(v1alpha1.RunConditionAccepted, metav1.ConditionTrue, "EngineNotReady", err.Error(), gen))
			meta.SetStatusCondition(&s.Conditions, runCondition(v1alpha1.RunConditionProgressing, metav1.ConditionFalse, "EngineNotReady", err.Error(), gen))
			s.Phase = v1alpha1.RunPhasePending
		})
		return ctrl.Result{RequeueAfter: engineRequeueAfter}, nil
	}

	// 5. Ensure the agent identity (SA + pods/exec Role/RoleBinding) + the owned agent pod.
	if err := r.ensureAgentIdentity(ctx, run); err != nil {
		logger.Error(err, "ensure agent identity", "run", req.NamespacedName)
		return ctrl.Result{}, err
	}
	pod, err := r.ensureAgentPod(ctx, run, project, enginePod, agentModel, agentMaxAPICalls, agentPrompt)
	if err != nil {
		logger.Error(err, "ensure agent pod", "run", req.NamespacedName)
		return ctrl.Result{}, err
	}

	// 6. Mirror the observed pod phase into the pinned condition set + derived Phase (ADR-0013 D5).
	// A distinct reason per path (review-13 HOUSE-3 — "Reconciling" was misleading on terminal).
	reason, message := "Dispatched", "agent pod observed"
	switch pod.Status.Phase {
	case corev1.PodFailed:
		reason, message = "AgentPodFailed", "agent pod exited non-zero"
	case corev1.PodSucceeded:
		reason, message = "AgentPodSucceeded", "agent pod exited zero"
	case corev1.PodRunning:
		reason, message = "Dispatched", "agent pod running"
	}
	return ctrl.Result{}, r.reconcileStatus(ctx, run, pod.Status.Phase, reason, message)
}

// enginePodName lists a Ready singleton engine pod and returns its name.
func (r *RunReconciler) enginePodName(ctx context.Context) (string, error) {
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, client.InNamespace(engineNamespace),
		client.MatchingLabels{engineLabelKey: engineLabelVal}); err != nil {
		return "", fmt.Errorf("list engine pods: %w", err)
	}
	for _, p := range pods.Items {
		if p.Status.Phase == corev1.PodRunning {
			return p.Name, nil
		}
	}
	return "", fmt.Errorf("no Running engine pod in namespace %q with label %s=%s (engine still rolling out?)",
		engineNamespace, engineLabelKey, engineLabelVal)
}

// ensureAgentIdentity ensures the per-namespace agent ServiceAccount and the pods/exec Role +
// RoleBinding that let the agent pod exec into the engine (cross-namespace: the SA lives in the
// Run's namespace; the Role/RoleBinding live in the engine namespace and bind that SA). Shared
// (created once per namespace), not per-Run. Phase 0: cleanup of the engine-namespace Role/
// RoleBinding on Run deletion is deferred (no cross-namespace ownerRef) — tracked in dagmar-67bc.
func (r *RunReconciler) ensureAgentIdentity(ctx context.Context, run *v1alpha1.Run) error {
	// ServiceAccount in the Run's namespace (where the agent pod runs).
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: agentSA, Namespace: run.Namespace}}
	if err := r.Get(ctx, client.ObjectKeyFromObject(sa), sa); errors.IsNotFound(err) {
		if err := ctrl.SetControllerReference(run, sa, r.Scheme); err != nil {
			return fmt.Errorf("set SA owner ref: %w", err)
		}
		if err := r.Create(ctx, sa); err != nil && !errors.IsAlreadyExists(err) {
			return fmt.Errorf("create agent SA: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("get agent SA: %w", err)
	}

	// Role in the engine namespace granting the agent SA the minimum kube-pod:// needs: exec into
	// the engine pod (pods/exec: create) + resolve it (pods: get). Two separate rules — NOT the
	// cartesian product `pods,pods/exec: create,get`, which would also grant `create` on bare `pods`
	// (letting the SA create arbitrary pods in the engine ns). Review-13 SPEC-2 least-privilege fix.
	role := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: agentRole, Namespace: engineNamespace}}
	if err := r.Get(ctx, client.ObjectKeyFromObject(role), role); errors.IsNotFound(err) {
		role.Rules = []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"pods/exec"}, Verbs: []string{"create"}},
			{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get"}},
		}
		if err := r.Create(ctx, role); err != nil && !errors.IsAlreadyExists(err) {
			return fmt.Errorf("create agent Role: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("get agent Role: %w", err)
	}

	// RoleBinding in the engine namespace, binding the SA (Run's namespace) to the Role.
	rb := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: agentRole, Namespace: engineNamespace}}
	if err := r.Get(ctx, client.ObjectKeyFromObject(rb), rb); errors.IsNotFound(err) {
		rb.Subjects = []rbacv1.Subject{{Kind: "ServiceAccount", Name: agentSA, Namespace: run.Namespace}}
		rb.RoleRef = rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "Role", Name: agentRole}
		if err := r.Create(ctx, rb); err != nil && !errors.IsAlreadyExists(err) {
			return fmt.Errorf("create agent RoleBinding: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("get agent RoleBinding: %w", err)
	}
	return nil
}

// ensureAgentPod creates the agent pod if it does not yet exist and returns it.
func (r *RunReconciler) ensureAgentPod(ctx context.Context, run *v1alpha1.Run, project *v1alpha1.Project, enginePod string, agentModel string, agentMaxAPICalls int, agentPrompt string) (*corev1.Pod, error) {
	podName := agentPodName(run.Name)
	pod := &corev1.Pod{}
	err := r.Get(ctx, types.NamespacedName{Name: podName, Namespace: run.Namespace}, pod)
	if err == nil {
		return pod, nil // already exists
	}
	if !errors.IsNotFound(err) {
		return nil, err
	}
	newPod := agentPodFor(run, project, enginePod, podName, agentModel, agentMaxAPICalls, agentPrompt)
	if err := ctrl.SetControllerReference(run, newPod, r.Scheme); err != nil {
		return nil, fmt.Errorf("set controller reference: %w", err)
	}
	if err := r.Create(ctx, newPod); err != nil {
		return nil, fmt.Errorf("create agent pod: %w", err)
	}
	return newPod, nil
}

// agentPodFor builds the agent pod. It installs kubectl + the dagger CLI and runs
// `dagger call -m <ModuleRef> <fn> <args>` against the singleton engine via kube-pod://. The pod
// runs as the per-namespace dagmar-agent SA (granted pods/exec on the engine) and uses in-cluster
// auth for the kubectl exec the runner host performs — no kubeconfig mount (unlike the cbb8 Probe
// client, which ran outside the cluster).
func agentPodFor(run *v1alpha1.Run, project *v1alpha1.Project, enginePod, podName string, agentModel string, agentMaxAPICalls int, agentPrompt string) *corev1.Pod {
	image := defaultAgentPodImage
	if project.Spec.AgentPodImage != "" {
		image = project.Spec.AgentPodImage
	}
	runnerHost := fmt.Sprintf("kube-pod://%s?namespace=%s", enginePod, engineNamespace)
	// Install kubectl + the dagger CLI (downloaded directly from the GitHub release tarball — the
	// `dl.dagger.io | sh` install script is unreliable from inside a pod: it 403s under some
	// networks). Then run `dagger call -m <moduleRef> <fn> <args>` against the singleton engine
	// via kube-pod://. The engine fetches the module server-side from the git ref.
	//
	// SHELL-INJECTION NOTE (review-13 GAP-1): this `sh -c` string interpolates THREE unescaped,
	// unquoted author fields — ModuleRef (Project CR), ModuleFunction (Run CR), and ModuleArgs
	// (Run CR, via shellJoin). All three are privileged-author fields (creating a Run/Project needs
	// RBAC), so a shell-metacharacter (;, $(), backtick) or even a space would be interpolated raw
	// but is author-self-inflicted in Phase 0. This becomes a LIVE injection vector for
	// agent-generated Runs (Phase 2) — the call shape must be built without a shell then.
	// (The git-credential additions below are fixed controller strings, not author fields.)
	apkPkgs, preCall := "kubectl curl", ""
	if ref := project.Spec.GitCredentialsRef; ref != nil {
		// Private module ref (ADR-0013 §4 D10, resolved): install git and configure a headless
		// credential helper that emits the projected PAT. When the engine fetches the private
		// module it queries this pod's `git credential fill`; the helper runs, emits the PAT, and
		// the engine injects it as a session-scoped secret (it holds no standing credential).
		apkPkgs = "kubectl curl git"
		// $DAGMAR_GIT_PAT is single-quoted for the outer sh, so git stores it literally and it
		// expands only when the helper runs at credential-fill time (inheriting the pod env).
		preCall = `git config --global credential.helper '!f() { echo username=dagmar; echo password="$DAGMAR_GIT_PAT"; }; f' && `
	}
	// Build the module function + args.
	fnArgs := run.Spec.ModuleArgs
	if agentModel != "" {
		// Cognition Run (AgentRef set): clone workspace + write prompt + inject code() args.
		// The workspace clone (ADR-0020 D1) is an ephemeral git clone into /workspace.
		// The prompt file is a minimal stub for now — full ADR-0005 cross-store merge is
		// deferred. The project prompt name from the AgentSpec is written as the prompt
		// content placeholder until canopy composition is wired.
		composeCmd := prompt.ShellComposeCommand(agentPrompt, "/workspace", "/tmp/prompt.md")
		preCall += fmt.Sprintf(
			`git clone %s /workspace && `+
				`%s && `,
			project.Spec.Repo, composeCmd,
		)
		fnArgs = append(fnArgs,
			"--source", "/workspace",
			"--prompt-file", "/tmp/prompt.md",
			"--module-ref", project.Spec.ModuleRef,
			"--model", agentModel,
		)
		if agentMaxAPICalls > 0 {
			fnArgs = append(fnArgs, "--max-apicalls", fmt.Sprintf("%d", agentMaxAPICalls))
		}
	}
	cmd := fmt.Sprintf(
		`apk add --no-cache %s && `+
			preCall+
			`curl -fsSL https://github.com/dagger/dagger/releases/download/v%s/dagger_v%s_linux_amd64.tar.gz | tar xz -C /usr/local/bin dagger && `+
			`dagger call -m %s %s %s`,
		apkPkgs, daggerVersion, daggerVersion, project.Spec.ModuleRef, run.Spec.ModuleFunction, shellJoin(fnArgs),
	)
	env := []corev1.EnvVar{
		{Name: "_EXPERIMENTAL_DAGGER_RUNNER_HOST", Value: runnerHost},
	}
	if ref := project.Spec.GitCredentialsRef; ref != nil {
		key := ref.Key
		if key == "" {
			key = gitCredsDefaultKey
		}
		env = append(env, corev1.EnvVar{
			Name: gitCredsEnvVar,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: ref.Name},
					Key:                  key,
				},
			},
		})
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: run.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "dagmar-controller",
				"dagmar.denkhaus.io/run":       run.Name,
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy:      corev1.RestartPolicyNever,
			ServiceAccountName: agentSA,
			Containers: []corev1.Container{{
				Name:    "agent",
				Image:   image,
				Command: []string{"sh", "-c", cmd},
				Env:     env,
			}},
		},
	}
}

// patchStatus applies mutate to a fresh copy of the Run's status and patches the status subresource
// with a MergeFrom (status-only) patch, retrying on Conflict (concurrent reconcile — the pod watch
// and an engine requeue can race for one Run). Conditions are the source of truth; Phase is derived
// (ADR-0013 D5). The mutator receives the freshly-fetched status; run.Generation is captured for
// ObservedGeneration.
func (r *RunReconciler) patchStatus(ctx context.Context, run *v1alpha1.Run, mutate func(*v1alpha1.RunStatus)) error {
	var lastErr error
	for range statusPatchAttempts {
		fresh := &v1alpha1.Run{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(run), fresh); err != nil {
			return fmt.Errorf("get run for status patch: %w", err)
		}
		base := fresh.DeepCopy()
		mutate(&fresh.Status)
		if err := r.Status().Patch(ctx, fresh, client.MergeFrom(base)); err != nil {
			// Retry only on Conflict (optimistic concurrency — a concurrent reconcile changed the
			// Run between our Get and Patch). A non-conflict error is surfaced immediately.
			if !errors.IsConflict(err) {
				return fmt.Errorf("patch run status: %w", err)
			}
			lastErr = err
			continue // re-Get + repatch
		}
		return nil
	}
	return fmt.Errorf("patch run status: %d conflicts in a row (last: %w)", statusPatchAttempts, lastErr)
}

// reconcileStatus mirrors the observed agent-pod phase into the pinned condition set (ADR-0013 D5)
// + the derived Phase + AgentPodName. Called once the pod is observed (validation has passed, so
// Accepted=True). podPhase "" (pod just created, not yet Pending/Running) is treated as dispatched-
// but-waiting → Progressing=False, Phase=Pending (distinct from Running, which sets Progressing=True).
//
// Succeeded/Failed are only ever set True, never reset False here. That is correct because agent
// pods run RestartPolicy: Never (agentPodFor) — a terminal phase is sticky, so a pod cannot move
// Succeeded↔Failed and the two can never both be True. Phase-2 pod retry/recreation would need to
// actively clear the stale terminal condition first, or phaseFromConditions would silently prefer
// Succeeded.
func (r *RunReconciler) reconcileStatus(ctx context.Context, run *v1alpha1.Run, podPhase corev1.PodPhase, reason, message string) error {
	return r.patchStatus(ctx, run, func(s *v1alpha1.RunStatus) {
		gen := run.Generation
		s.AgentPodName = agentPodName(run.Name)
		meta.SetStatusCondition(&s.Conditions, runCondition(v1alpha1.RunConditionAccepted, metav1.ConditionTrue, reason, message, gen))
		progressing := metav1.ConditionFalse // dispatched-but-waiting (Pending/"") or terminal
		if podPhase == corev1.PodRunning {
			progressing = metav1.ConditionTrue
		}
		if podPhase == corev1.PodSucceeded {
			meta.SetStatusCondition(&s.Conditions, runCondition(v1alpha1.RunConditionSucceeded, metav1.ConditionTrue, reason, message, gen))
		}
		if podPhase == corev1.PodFailed {
			meta.SetStatusCondition(&s.Conditions, runCondition(v1alpha1.RunConditionFailed, metav1.ConditionTrue, reason, message, gen))
		}
		meta.SetStatusCondition(&s.Conditions, runCondition(v1alpha1.RunConditionProgressing, progressing, reason, message, gen))
		s.Phase = phaseFromConditions(s.Conditions)
	})
}

// failRun is the terminal-rejection status transition (ADR-0013 D5): the Run failed validation
// (missing Project/ModuleRef/ModuleFunction/git-creds Secret), so Accepted=False + Failed=True +
// Progressing=False. No pod is created on this path → AgentPodName is not set.
func (r *RunReconciler) failRun(ctx context.Context, run *v1alpha1.Run, reason, message string) error {
	return r.patchStatus(ctx, run, func(s *v1alpha1.RunStatus) {
		gen := run.Generation
		meta.SetStatusCondition(&s.Conditions, runCondition(v1alpha1.RunConditionAccepted, metav1.ConditionFalse, reason, message, gen))
		meta.SetStatusCondition(&s.Conditions, runCondition(v1alpha1.RunConditionProgressing, metav1.ConditionFalse, reason, message, gen))
		meta.SetStatusCondition(&s.Conditions, runCondition(v1alpha1.RunConditionFailed, metav1.ConditionTrue, reason, message, gen))
		s.Phase = phaseFromConditions(s.Conditions)
	})
}

// runCondition builds a metav1.Condition for a Run status type.
func runCondition(t string, st metav1.ConditionStatus, reason, message string, gen int64) metav1.Condition {
	return metav1.Condition{Type: t, Status: st, Reason: reason, Message: message, ObservedGeneration: gen}
}

// phaseFromConditions derives the high-level Phase from the condition set (ADR-0013 D5): the
// conditions are authoritative; Phase is a read-optimized summary for k9s/back-compat.
func phaseFromConditions(conds []metav1.Condition) string {
	switch {
	case meta.IsStatusConditionTrue(conds, v1alpha1.RunConditionSucceeded):
		return v1alpha1.RunPhaseSucceeded
	case meta.IsStatusConditionTrue(conds, v1alpha1.RunConditionFailed):
		return v1alpha1.RunPhaseFailed
	case meta.IsStatusConditionTrue(conds, v1alpha1.RunConditionProgressing):
		return v1alpha1.RunPhaseRunning
	default:
		return v1alpha1.RunPhasePending
	}
}

func agentPodName(runName string) string { return runName + "-agent" }

// shellJoin joins ModuleArgs into a shell argument string. See the SHELL-INJECTION NOTE on
// agentPodFor: this is one of THREE unescaped interpolation sites (ModuleRef, ModuleFunction,
// ModuleArgs) — all author-controlled in Phase 0; revisit for Phase 2 agent-generated Runs.
func shellJoin(args []string) string {
	return strings.Join(args, " ")
}

// SetupWithManager registers the reconciler for Run CRs and watches owned Pods. D3 (ADR-0013 §2)
// is realized here: For(Run)+Owns(Pod) + the controller-owner ref set in ensureAgentPod means a pod
// phase transition (Running/Succeeded/Failed) requeues the owning Run automatically — no polling.
// (Owns(&ServiceAccount{}) is intentionally NOT added: the agent SA is shared-named per namespace,
// not per-Run, so an owner-ref watch is semantically wrong until identity becomes per-Run-named —
// the D4-blocker, tracked in the identity-refactor seed.)
func (r *RunReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Run{}).
		Owns(&corev1.Pod{}).
		Owns(&v1alpha1.Run{}). // Sub-Runs created by orchestration Runs (ADR-0016 §4)
		Named("run").
		Complete(r)
}
