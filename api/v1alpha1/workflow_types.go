package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorkflowSpec defines a pipeline template referencing Dagger functions + controller-
// interpreted orchestration metadata (ADR-0016). A Workflow is NOT a step DSL — the
// pipeline form (gate → review → merge with revise loop) is hardcoded in the controller.
// The Workflow carries the metadata the controller needs to orchestrate Sub-Runs.
type WorkflowSpec struct {
	// CoderAgentRef names the Agent (in the Workflow's namespace) used for the coder role.
	// The controller reads this Agent's spec to configure the code() function call.
	// +kubebuilder:validation:Required
	CoderAgentRef string `json:"coderAgentRef"`

	// PrompterAgentRef names the Agent used for the prompter role (ADR-0023 D7). The
	// controller reads this Agent's spec to configure the prompt() call (model,
	// maxAPICalls) that synthesizes tailored prompts at runtime.
	// +kubebuilder:validation:Required
	PrompterAgentRef string `json:"prompterAgentRef"`

	// ReviewerAgentRef names the Agent used for the review role. Optional: if unset,
	// the workflow runs without review (gate-only, for non-merge workflows).
	// +optional
	ReviewerAgentRef string `json:"reviewerAgentRef,omitempty"`

	// AdjudicatorAgentRef names the Agent used for the adjudicator role (ADR-0023 D7).
	// Optional: when absent, gate↔reviewer disagreement escalates directly to a human.
	// +optional
	AdjudicatorAgentRef string `json:"adjudicatorAgentRef,omitempty"`

	// QualityGateRef names the QualityGate that gates advancement. The controller
	// evaluates gate-green between pipeline stages.
	// +kubebuilder:validation:Required
	QualityGateRef string `json:"qualityGateRef"`

	// RequiresTwoGreen enforces the two-green model (ADR-0006): merge requires
	// QualityGate.green AND ReviewAgent.approve. If false, only gate-green is needed
	// (used for non-merge workflows like research).
	// +optional
	// +kubebuilder:default=true
	RequiresTwoGreen bool `json:"requiresTwoGreen,omitempty"`
}

// Workflow has NO Status subresource — it is a pure definition/template (like Agent,
// Prompt, QualityGate). Orchestration state lives on the orchestration Run (ADR-0016 §1).

// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=wf

// Workflow is a pipeline template: agent refs + quality gate + orchestration metadata.
// The controller reads a Workflow to orchestrate Sub-Runs in the pipeline sequence
// (gate → coder → review → merge). NOT a step DSL (ADR-0016 §1).
type Workflow struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec WorkflowSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// WorkflowList is a list of Workflows.
type WorkflowList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Workflow `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Workflow{}, &WorkflowList{})
}
