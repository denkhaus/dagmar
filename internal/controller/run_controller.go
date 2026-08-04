package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/denkhaus/dagmar/api/v1alpha1"
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

	// engineRequeueAfter is the requeue delay when the engine pod is not yet Ready (transient —
	// the engine may still be rolling out at kind bringup).
	engineRequeueAfter = 10 * time.Second

	// conditionAvailable is the Run "Available" condition type.
	conditionAvailable = "Available"
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
// +kubebuilder:rbac:groups=dagmar.denkhaus.io,resources=projects,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods/exec,verbs=create
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

	// 3b. ModuleFunction is the other load-bearing Phase-0 read: the function the agent pod calls.
	// Empty → terminal config error (symmetric with ModuleRef; review-13 HOUSE-4 — otherwise the
	// pod runs `dagger call -m <ref>` with no function and fails at runtime, mirrored opaquely).
	if run.Spec.ModuleFunction == "" {
		return ctrl.Result{}, r.failRun(ctx, run, "ModuleFunctionRequired",
			"Run.Spec.ModuleFunction is empty; it must name the module function for `dagger call`")
	}

	// 4. Resolve the singleton engine pod (a Ready one). Transiently absent (engine still rolling
	// out) → requeue, not terminal.
	enginePod, err := r.enginePodName(ctx)
	if err != nil {
		logger.Info("engine pod not ready; requeueing", "err", err)
		_ = r.setStatus(ctx, run, v1alpha1.RunPhasePending, "EngineNotReady",
			err.Error(), metav1.ConditionUnknown)
		return ctrl.Result{RequeueAfter: engineRequeueAfter}, nil
	}

	// 5. Ensure the agent identity (SA + pods/exec Role/RoleBinding) + the owned agent pod.
	if err := r.ensureAgentIdentity(ctx, run); err != nil {
		logger.Error(err, "ensure agent identity", "run", req.NamespacedName)
		return ctrl.Result{}, err
	}
	pod, err := r.ensureAgentPod(ctx, run, project, enginePod)
	if err != nil {
		logger.Error(err, "ensure agent pod", "run", req.NamespacedName)
		return ctrl.Result{}, err
	}

	// 6. Mirror the pod phase into the Run status + record the pod name. A distinct reason on the
	// terminal failure path (review-13 HOUSE-3) — "Reconciling" was misleading for a Failed pod.
	reason, message := "Reconciling", "agent pod observed"
	if pod.Status.Phase == corev1.PodFailed {
		reason, message = "AgentPodFailed", "agent pod exited non-zero"
	}
	return ctrl.Result{}, r.setStatus(ctx, run, podPhaseToRunPhase(pod.Status.Phase), reason, message,
		runConditionStatus(pod.Status.Phase))
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
func (r *RunReconciler) ensureAgentPod(ctx context.Context, run *v1alpha1.Run, project *v1alpha1.Project, enginePod string) (*corev1.Pod, error) {
	podName := agentPodName(run.Name)
	pod := &corev1.Pod{}
	err := r.Get(ctx, types.NamespacedName{Name: podName, Namespace: run.Namespace}, pod)
	if err == nil {
		return pod, nil // already exists
	}
	if !errors.IsNotFound(err) {
		return nil, err
	}
	newPod := agentPodFor(run, project, enginePod, podName)
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
func agentPodFor(run *v1alpha1.Run, project *v1alpha1.Project, enginePod, podName string) *corev1.Pod {
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
	cmd := fmt.Sprintf(
		`apk add --no-cache kubectl curl && `+
			`curl -fsSL https://github.com/dagger/dagger/releases/download/v%s/dagger_v%s_linux_amd64.tar.gz | tar xz -C /usr/local/bin dagger && `+
			`dagger call -m %s %s %s`,
		daggerVersion, daggerVersion, project.Spec.ModuleRef, run.Spec.ModuleFunction, shellJoin(run.Spec.ModuleArgs),
	)
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
				Env: []corev1.EnvVar{
					{Name: "_EXPERIMENTAL_DAGGER_RUNNER_HOST", Value: runnerHost},
				},
			}},
		},
	}
}

// setStatus writes Phase + AgentPodName + an Available condition to the Run's status subresource.
func (r *RunReconciler) setStatus(ctx context.Context, run *v1alpha1.Run, phase, reason, message string, available metav1.ConditionStatus) error {
	run.Status.Phase = phase
	run.Status.AgentPodName = agentPodName(run.Name)
	meta.SetStatusCondition(&run.Status.Conditions, metav1.Condition{
		Type: conditionAvailable, Status: available, Reason: reason, Message: message,
		ObservedGeneration: run.Generation,
	})
	return r.Status().Update(ctx, run)
}

// failRun is setStatus with Phase=Failed + Available=False (terminal config errors).
func (r *RunReconciler) failRun(ctx context.Context, run *v1alpha1.Run, reason, message string) error {
	return r.setStatus(ctx, run, v1alpha1.RunPhaseFailed, reason, message, metav1.ConditionFalse)
}

func agentPodName(runName string) string { return runName + "-agent" }
func podPhaseToRunPhase(p corev1.PodPhase) string {
	switch p {
	case corev1.PodPending:
		return v1alpha1.RunPhasePending
	case corev1.PodRunning:
		return v1alpha1.RunPhaseRunning
	case corev1.PodSucceeded:
		return v1alpha1.RunPhaseSucceeded
	case corev1.PodFailed:
		return v1alpha1.RunPhaseFailed
	default:
		return v1alpha1.RunPhasePending
	}
}
func runConditionStatus(p corev1.PodPhase) metav1.ConditionStatus {
	switch p {
	case corev1.PodSucceeded:
		return metav1.ConditionTrue
	case corev1.PodFailed:
		return metav1.ConditionFalse
	default:
		return metav1.ConditionUnknown
	}
}

// shellJoin joins args into a shell argument string (Phase 0 spike; no escaping — ModuleArgs are
// shellJoin joins ModuleArgs into a shell argument string. See the SHELL-INJECTION NOTE on
// agentPodFor: this is one of THREE unescaped interpolation sites (ModuleRef, ModuleFunction,
// ModuleArgs) — all author-controlled in Phase 0; revisit for Phase 2 agent-generated Runs.
func shellJoin(args []string) string {
	return strings.Join(args, " ")
}

// SetupWithManager registers the reconciler for Run CRs, also watching owned Pods.
func (r *RunReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Run{}).
		Owns(&corev1.Pod{}).
		Named("run").
		Complete(r)
}
