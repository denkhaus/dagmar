package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PromptSpec defines a reference to canopy prompt templates for cross-store composition
// (ADR-0005). A Prompt is NOT a dagmar-invented spec — it is a reference pair: a
// project-content prompt (in the project's .canopy/) plus dagmar operational mixins
// (in dagmar's .canopy/). The controller resolves these at dispatch time into the final
// .md file passed to the code() function's WithPromptFile.
type PromptSpec struct {
	// ProjectPrompt is the canopy prompt name in the project's .canopy/ store.
	// Contains the project-content prompt (role/task/domain specifics, mulch deps).
	// +kubebuilder:validation:Required
	ProjectPrompt string `json:"projectPrompt"`

	// DagmarMixins are canopy mixin names from dagmar's own .canopy/ store.
	// Operational mixins: output-format, review-gating, safety/autonomy-bounds, tool-rules.
	// dagmar controls the final composed output (ADR-0005 constraint).
	// +optional
	DagmarMixins []string `json:"dagmarMixins,omitempty"`
}

// PromptStatus holds the observed state of a Prompt. Minimal — Prompt is a definition/
// template (like Agent, QualityGate); it has no reconciliation of its own.
type PromptStatus struct {
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=prompt

// Prompt is a reference to canopy prompt templates: project-content prompt + dagmar
// operational mixins. dagmar composes them at run time (ADR-0005, Variant A) into the
// final prompt passed to dag.LLM().WithPromptFile(). Agents reference a Prompt (directly
// or via PromptRef inline).
type Prompt struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PromptSpec   `json:"spec,omitempty"`
	Status PromptStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PromptList is a list of Prompts.
type PromptList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Prompt `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Prompt{}, &PromptList{})
}
