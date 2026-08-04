package controller

import (
	"context"
	"fmt"
	"strings"

	"github.com/denkhaus/dagmar/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
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
	// engineLabel selects the singleton Dagger engine pod (cbb8 spike recipe, helm chart name).
	engineLabel = "name=dagger-dagger-helm-engine"
	// engineNamespace is where the singleton engine DaemonSet runs.
	engineNamespace = "dagmar"
	// defaultAgentPodImage is the Phase-0 default agent image (runtime-installs kubectl + dagger,
	// matching the cbb8 Probe recipe).
	defaultAgentPodImage = "alpine:3.20"
	// daggerVersion is the CLI version installed into the agent pod.
	daggerVersion = "0.21.8"

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

// Reconcile drives a Run: it resolves the Project + the singleton engine pod, ensures an agent
// pod exists that calls the module, and mirrors the pod's phase into the Run's status.
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

	// 2. Fetch the referenced Project.
	project := &v1alpha1.Project{}
	if err := r.Get(ctx, types.NamespacedName{Name: run.Spec.ProjectRef, Namespace: run.Namespace}, project); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, r.setStatus(ctx, run, v1alpha1.RunPhaseFailed, "ProjectNotFound",
				fmt.Sprintf("referenced Project %q not found", run.Spec.ProjectRef), metav1.ConditionFalse)
		}
		return ctrl.Result{}, err
	}

	// 3. Resolve the singleton engine pod name (kube-pod:// target).
	enginePod, err := r.enginePodName(ctx)
	if err != nil {
		return ctrl.Result{}, r.setStatus(ctx, run, v1alpha1.RunPhaseFailed, "EngineNotFound",
			err.Error(), metav1.ConditionFalse)
	}

	// 4. Ensure the agent pod exists (owned by the Run).
	pod, err := r.ensureAgentPod(ctx, run, project, enginePod)
	if err != nil {
		logger.Error(err, "ensure agent pod", "run", req.NamespacedName)
		return ctrl.Result{}, err
	}

	// 5. Mirror the pod phase into the Run status + record the pod name.
	return ctrl.Result{}, r.setStatus(ctx, run, podPhaseToRunPhase(pod.Status.Phase), "Reconciling",
		"agent pod observed", runConditionStatus(pod.Status.Phase))
}

// enginePodName lists the singleton engine pod (by helm chart label) and returns its name.
func (r *RunReconciler) enginePodName(ctx context.Context) (string, error) {
	labelKey, labelVal := splitLabel(engineLabel)
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, client.InNamespace(engineNamespace), client.MatchingLabels{labelKey: labelVal}); err != nil {
		return "", fmt.Errorf("list engine pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no engine pod found in namespace %q with label %s", engineNamespace, engineLabel)
	}
	return pods.Items[0].Name, nil
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

// agentPodFor builds the agent pod that installs kubectl + the dagger CLI and runs
// `dagger call <fn> <args>` against the singleton engine via kube-pod://. The pod uses in-cluster
// auth (its ServiceAccount) for the kubectl exec the runner host performs — so no kubeconfig
// mount is needed (unlike the cbb8 Probe client, which ran outside the cluster).
func agentPodFor(run *v1alpha1.Run, project *v1alpha1.Project, enginePod, podName string) *corev1.Pod {
	image := defaultAgentPodImage
	if project.Spec.AgentPodImage != "" {
		image = project.Spec.AgentPodImage
	}
	runnerHost := fmt.Sprintf("kube-pod://%s?namespace=%s", enginePod, engineNamespace)
	cmd := fmt.Sprintf(
		`apk add --no-cache kubectl curl && DAGGER_VERSION=%s curl -fsSL https://dl.dagger.io | sh && dagger call %s %s`,
		daggerVersion, run.Spec.ModuleFunction, shellJoin(run.Spec.ModuleArgs),
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
			RestartPolicy: corev1.RestartPolicyNever,
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

// splitLabel splits a "key=value" label selector into its parts.
func splitLabel(kv string) (string, string) {
	k, v, _ := strings.Cut(kv, "=")
	return k, v
}

// shellJoin joins args into a shell-safe argument string (one space, no escaping for Phase 0
// spike; the Run CR is a privileged resource and ModuleArgs are author-controlled).
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
