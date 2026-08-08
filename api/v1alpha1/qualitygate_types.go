package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// QualityGateSpec defines the deterministic gate that decides whether a candidate may
// advance (ADR-0009). The QualityGate is INVARIANT — it always secures quality. Merge
// requires QualityGate.green AND ReviewAgent.approve (two green lights, ADR-0006).
//
// The gate references the project's check functions. In-loop, the agent self-verifies via
// env.Checks() (ADR-0020 D6); in CI, dagmar-gate runs the check functions. The gate itself
// is a function reference + rules — the actual check logic lives in the project module.
type QualityGateSpec struct {
	// CheckFunction is the project module function that runs the gate checks
	// (default: "dagmar-gate"). Called by the controller via `dagger call -m <ref> dagmar-gate`.
	// +optional
	// +kubebuilder:default="dagmar-gate"
	CheckFunction string `json:"checkFunction,omitempty"`

	// MaxReviseRounds bounds the number of coder → gate → revise cycles before
	// escalation (ADR-0016 §4). When exceeded, the orchestration Run enters
	// "escalated" state (human handoff).
	// +optional
	// +kubebuilder:default=3
	MaxReviseRounds int `json:"maxReviseRounds,omitempty"`
}

// QualityGateStatus holds the observed state of a QualityGate. Minimal — QualityGate is a
// definition/template; the gate outcome is evaluated per-Run by the controller.
type QualityGateStatus struct {
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=qgate

// QualityGate is the deterministic layer deciding whether a candidate may advance
// (checkables + rules). Invariant — always secures quality (ADR-0009). Merge requires
// QualityGate.green AND ReviewAgent.approve (two green lights, ADR-0006).
type QualityGate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   QualityGateSpec   `json:"spec,omitempty"`
	Status QualityGateStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// QualityGateList is a list of QualityGates.
type QualityGateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []QualityGate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&QualityGate{}, &QualityGateList{})
}
