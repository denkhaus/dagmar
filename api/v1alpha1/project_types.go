package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ProjectSpec defines a registered, repo-backed repository dagmar operates on. Phase 0 carries
// only the fields the dispatch vertical needs (ADR-0012 §2 GAP-1: agent-pod image + module ref
// are load-bearing Phase-0 reads). The full Project spec (Project Hook binding, the three typed
// credential classes, autonomy setting, ProjectManifest reference — CONTEXT.md) is added as the
// control plane grows; field-home details are deferred to the control-plane-design seed.
// GitCredentialsRef names the Secret holding the fine-grained PAT / deploy token the engine uses
// to fetch this Project's private module ref (ADR-0013 §4 D10 — the #8805 git-credential path).
// When set, the controller projects the token into the agent pod as an env var and configures a
// headless git credential helper; the engine queries the pod's `git credential fill`, receives
// the PAT, and injects it as a session-scoped secret for that fetch (it holds no standing cred).
// Optional: unset ⇒ the module ref is treated as public (current Phase-0 behavior).
// +kubebuilder:object:generate=true
type GitCredentialsRef struct {
	// Name is the Secret name, in the Project's (Run's) namespace. Required so an empty name is
	// rejected at CRD admission rather than requeuing indefinitely (review-16 HOUSE-3).
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Key is the key in the Secret holding the PAT/token. Defaults to "token" when empty.
	// +optional
	Key string `json:"key,omitempty"`
}

type ProjectSpec struct {
	// Repo is the git repository URL dagmar operates on. Informational in Phase 0 (full
	// clone/workspace model is Phase 2); recorded so dagmar-own can be registered as a Project.
	// +kubebuilder:validation:Required
	Repo string `json:"repo"`

	// AgentPodImage is the OCI image the agent pod runs (Phase-0 read, ADR-0012 GAP-1). The
	// field-home (Project spec vs. ConfigMap) is deferred to the control-plane-design seed.
	// Defaults to "alpine:3.20" when empty (runtime-installed kubectl + dagger CLI; cbb8 spike).
	// +optional
	AgentPodImage string `json:"agentPodImage,omitempty"`

	// ModuleRef is the dagmar module version the agent pod invokes (Phase-0 read, ADR-0012
	// GAP-1), and how it is pointed at the engine on kind vs. production. Deferred to the
	// control-plane-design seed for the exact resolution mechanism.
	// +optional
	ModuleRef string `json:"moduleRef,omitempty"`

	// GitCredentialsRef optionally names the Secret holding the PAT the engine uses to fetch
	// this Project's private module ref (ADR-0013 §4 D10 — the resolved #8805 mechanism). When
	// set, the controller projects the token into the agent pod + configures a headless git
	// credential helper; when unset, the module ref is treated as public. Dogfood: dagmar-own
	// carries dagmar-git-creds to read its own now-private module.
	// +optional
	GitCredentialsRef *GitCredentialsRef `json:"gitCredentialsRef,omitempty"`

	// CoveragePolicy controls test-coverage ratcheting in the quality gate (dagmar-4154).
	// When enabled, dagmar-gate measures total go test coverage and compares it against the
	// ratcheted floor (Project.Status.CoverageFloor). Coverage below the floor → gate RED.
	// The controller ratchets the floor upward after each green gate: newFloor = max(floor,
	// coverage - RatchetMargin), clamped to MinimumFloor. This creates continuously increasing
	// coverage pressure without manual threshold bumps.
	// +optional
	CoveragePolicy *CoveragePolicy `json:"coveragePolicy,omitempty"`
}

// CoveragePolicy defines the coverage-ratcheting policy for a Project (dagmar-4154).
// The policy is operator-set (CRD spec); the actual ratcheted floor is controller-managed
// (CRD status). The gate function receives the floor as a parameter and enforces it.
//
// All coverage values are expressed in basis points (0–10000), where 7850 = 78.50%.
// This avoids float64 in CRD schemas (which kubebuilder discourages for cross-language
// portability).
type CoveragePolicy struct {
	// Enabled activates coverage-ratcheting in the quality gate. When false (default),
	// coverage is not checked.
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// MinimumFloor is the absolute lower bound for the coverage floor (basis points, 0–10000).
	// The ratcheted floor (Status.CoverageFloor) never drops below this value, even if actual
	// coverage is lower. This prevents a bad commit from permanently lowering the bar.
	// Default 0 (= 0.00%) when unset.
	// +optional
	// +kubebuilder:default=0
	MinimumFloor int `json:"minimumFloor,omitempty"`

	// RatchetMargin is the margin below actual coverage that the floor tracks (basis points).
	// After a green gate, the controller sets newFloor = max(currentFloor, coverage - RatchetMargin).
	// A larger margin gives more slack; a smaller margin ratchets tighter.
	// Default 200 (= 2.00%) when unset.
	// +optional
	// +kubebuilder:default=200
	RatchetMargin int `json:"ratchetMargin,omitempty"`
}

// ProjectStatus holds the observed state of a Project. Minimal in Phase 0.
type ProjectStatus struct {
	// CoverageFloor is the current ratcheted coverage floor (basis points, 0–10000).
	// Controller-managed: after each green gate with coverage measurement, the controller
	// ratchets this value upward (dagmar-4154). Never drops below CoveragePolicy.MinimumFloor.
	// +optional
	CoverageFloor int `json:"coverageFloor,omitempty"`

	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=proj

// Project is a registered, repo-backed repository dagmar operates on.
type Project struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProjectSpec   `json:"spec,omitempty"`
	Status ProjectStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProjectList is a list of Projects.
type ProjectList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Project `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Project{}, &ProjectList{})
}
