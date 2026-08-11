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

	// AgentRef names the Agent (in the Run's namespace) whose spec configures the
	// code() function call (model, prompt, maxAPICalls, toolSetPolicy).
	// Required for cognition Runs; empty for raw module-call Runs (Phase 0 compat).
	// +optional
	AgentRef string `json:"agentRef,omitempty"`

	// ModuleFunction is the dagmar module function the agent pod calls via `dagger call`
	// (e.g. "code", "sandbox", "probe-net"). Atomic mode (ADR-0016 §2): one function,
	// one agent pod. Mutually exclusive with WorkflowRef.
	// +optional
	ModuleFunction string `json:"moduleFunction,omitempty"`

	// ModuleArgs are the CLI args passed to the function (e.g. ["--image","alpine:3.20"]).
	// +optional
	ModuleArgs []string `json:"moduleArgs,omitempty"`

	// WorkflowRef names the Workflow template for orchestration mode (ADR-0016 §2).
	// When set, the controller orchestrates 1..N atomic Sub-Runs in the pipeline
	// sequence. Mutually exclusive with ModuleFunction.
	// +optional
	WorkflowRef string `json:"workflowRef,omitempty"`

	// ParentRun is set on Sub-Runs created by an orchestration Run (ADR-0016 §2).
	// Names the orchestration Run that created this atomic Run. Empty on atomic
	// Runs created directly and on orchestration Runs.
	// +optional
	ParentRun string `json:"parentRun,omitempty"`

	// TaskContext is the concrete task description passed to the prompter and coder
	// (dagmar-d8dc). For orchestration Runs it is the issue text / objective the pipeline
	// works on. The controller injects it as --task-context into the prompter call and
	// flows it to coder/reviewer Sub-Runs via annotation. Without it the prompter has no
	// concrete objective and explores the entire codebase aimlessly.
	// +optional
	TaskContext string `json:"taskContext,omitempty"`
}

// RunStatus holds the observed state of a Run. Phase 0 = status conditions + phase only
// (ADR-0012 §2 SPEC-1: requeue/error-backoff/finalizers deferred to the control-plane-design
// seed).
type RunStatus struct {
	// Phase is the high-level lifecycle phase (Pending|Running|Succeeded|Failed), DERIVED from the
	// condition set via phaseFromConditions (a read-optimized summary for k9s/back-compat; the
	// RunCondition* types are the source of truth, ADR-0013 D5).
	// +optional
	Phase string `json:"phase,omitempty"`

	// AgentPodName is the name of the owned pod executing the module call.
	// +optional
	AgentPodName string `json:"agentPodName,omitempty"`

	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// PipelinePhase tracks orchestration Run progress (ADR-0027).
	// Values: "dispatching" | "running" | "done" | "escalated".
	// Zero-valued on atomic Runs.
	// +optional
	PipelinePhase string `json:"pipelinePhase,omitempty"`

	// StepResults holds step-by-step results pushed by the CognitionRun pipeline's
	// Collector (ADR-0027 D3). Each entry is a step result from the pipeline execution:
	// prompt, code, gate, review, adjudicate. Provides fine-grained visibility for the
	// controller's policy decisions (retry/escalate/done) and k9s observability.
	// +optional
	// +listType=map
	// +kubebuilder:validation:MaxItems=50
	StepResults []StepResult `json:"stepResults,omitempty"`
}

// StepResult is a single pipeline step's result, pushed by the Collector (ADR-0027 D3).
type StepResult struct {
	// Step is the pipeline step name: "prompt", "code", "gate", "review", "adjudicate".
	Step string `json:"step"`

	// Round is the revise-loop round (1-based). 0 for non-loop steps (prompt).
	// +optional
	Round int `json:"round,omitempty"`

	// Result is the step's result as a JSON string (gate JSON, review verdict, etc.).
	// +optional
	Result string `json:"result,omitempty"`
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
