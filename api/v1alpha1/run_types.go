package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RunPhase is the high-level phase of a Run's lifecycle (Status.Phase), DERIVED from the
// condition set (phaseFromConditions) — a read-optimized summary for k9s/back-compat. The
// conditions (RunCondition*) are the source of truth (ADR-0013 D5).
const (
	RunPhasePending   = "Pending"
	RunPhaseRunning   = "Running"
	RunPhaseSucceeded = "Succeeded"
	RunPhaseFailed    = "Failed"
)

// RunCondition* are the pinned Run status condition types (metav1.Condition.Type), the minimal set
// grown deliberately (ADR-0013 D5): the controller writes exactly these four. Condition TYPES are
// open-ended in the CRD schema, so adding/growing this set needs no manifest regen.
const (
	// RunConditionAccepted — the Run passed validation (Project + ModuleRef/Function present, git-creds
	// Secret exists) and was admitted for dispatch. False on a terminal validation rejection.
	RunConditionAccepted = "Accepted"
	// RunConditionProgressing — the agent pod is actively executing (Running). False while dispatched-
	// but-waiting (Pending) or terminal (Succeeded/Failed/rejected).
	RunConditionProgressing = "Progressing"
	// RunConditionSucceeded — the Run reached a successful terminal state (pod Succeeded).
	RunConditionSucceeded = "Succeeded"
	// RunConditionFailed — the Run reached a failed terminal state (pod Failed OR a validation rejection).
	RunConditionFailed = "Failed"
)

// RunSpec defines one execution of an Agent in one Sandbox on one Workspace — the observable,
// reconciled execution unit (CONTEXT.md). Phase 0 carries the module-call the agent pod runs;
// the Agent reference (model/prompt/tool-set) is added in Phase 2 (cognition).
type RunSpec struct {
	// ProjectRef names the Project (in the Run's namespace) this Run executes under.
	// +kubebuilder:validation:Required
	ProjectRef string `json:"projectRef"`

	// ModuleFunction is the dagmar module function the agent pod calls via `dagger call`
	// (e.g. "sandbox", "probe-net"). Phase 0 uses the existing module surface; the coder-Loop
	// function is Phase 2.
	ModuleFunction string `json:"moduleFunction"`

	// ModuleArgs are the CLI args passed to the function (e.g. ["--image","alpine:3.20"]).
	// +optional
	ModuleArgs []string `json:"moduleArgs,omitempty"`
}

// RunStatus holds the observed state of a Run. Phase 0 = status conditions + phase only
// (ADR-0012 §2 SPEC-1: requeue/error-backoff/finalizers deferred to the control-plane-design
// seed).
type RunStatus struct {
	// Phase is the high-level lifecycle phase (Pending|Running|Succeeded|Failed), derived from
	// the owned agent pod's phase.
	// +optional
	Phase string `json:"phase,omitempty"`

	// AgentPodName is the name of the owned pod executing the module call.
	// +optional
	AgentPodName string `json:"agentPodName,omitempty"`

	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=run

// Run is one execution of an Agent, in one Sandbox, on one Workspace — the observable,
// reconciled execution unit (the resource the dagmar controller reconciles).
type Run struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RunSpec   `json:"spec,omitempty"`
	Status RunStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RunList is a list of Runs.
type RunList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Run `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Run{}, &RunList{})
}
