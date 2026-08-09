package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ToolSetPolicy defines the network/tool surface an Agent's Env gets (ADR-0011).
type ToolSetPolicy string

const (
	// ToolSetPolicyHermetic excludes all network-capable tools (http, git remote,
	// container exec). The Env has only the project module's LLM-Tool hooks (ADR-0019).
	// Default for coder and reviewer roles.
	ToolSetPolicyHermetic ToolSetPolicy = "hermetic"
	// ToolSetPolicyNetworked allows network-capable tools on the Env (http, git remote).
	// For roles that need external access (e.g. researcher). Use with caution — the agent
	// can reach the network (ADR-0011 §3 residual risk).
	ToolSetPolicyNetworked ToolSetPolicy = "networked"
)

// AgentSpec defines a durable role/persona (coder, reviewer, researcher). An Agent is
// materialized as Runs — it carries the LLM configuration the controller passes to the
// code() function (ADR-0021 D1). Agents have no merge authority (ADR-0006); the merge tool
// is in no Agent's tool-set. The prompt is no longer part of the Agent spec — it is
// synthesized at runtime by the Prompter-LLM (ADR-0023 D1/D8).
type AgentSpec struct {
	// Model is the LLM model identifier passed to dag.LLM(LLMOpts{Model}).
	// Examples: "anthropic/claude-sonnet-4", "openai/gpt-4o".
	// +kubebuilder:validation:Required
	Model string `json:"model"`

	// MaxAPICalls bounds the LLM API calls per Run (ADR-0021 D4). Engine-enforced
	// hard stop: when exhausted, the Loop terminates and the Run fails (gate RED).
	// Default 100 when unset.
	// +optional
	// +kubebuilder:default=100
	MaxAPICalls int `json:"maxAPICalls,omitempty"`

	// ToolSetPolicy controls the network/tool surface on the Agent's Env (ADR-0011).
	// "hermetic" (default) excludes network-capable tools; "networked" allows them.
	// Phase 2 incremental: not yet read by the controller; code() always uses
	// WithMainModule (hermetic). Wired when networked agent roles are needed.
	// +optional
	// +kubebuilder:default=hermetic
	ToolSetPolicy ToolSetPolicy `json:"toolSetPolicy,omitempty"`
}

// AgentStatus holds the observed state of an Agent. Minimal — Agent is a definition/template
// (like Prompt, QualityGate); it has no reconciliation of its own. Runs reference it.
type AgentStatus struct {
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=agent

// Agent is a durable role/persona (coder, reviewer, researcher): model + budget +
// tool-set policy. Materialized as Runs; the controller reads an Agent's spec to configure
// the code() function (ADR-0021 D1). Agents have no merge authority (ADR-0006).
// Prompt synthesis is handled by the Prompter-LLM (ADR-0023 D1/D8), not the Agent spec.
type Agent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentSpec   `json:"spec,omitempty"`
	Status AgentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AgentList is a list of Agents.
type AgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Agent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Agent{}, &AgentList{})
}
