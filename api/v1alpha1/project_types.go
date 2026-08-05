package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ProjectSpec defines a registered, repo-backed repository dagmar operates on. Phase 0 carries
// only the fields the dispatch vertical needs (ADR-0012 §2 GAP-1: agent-pod image + module ref
// are load-bearing Phase-0 reads). The full Project spec (os-eco binding, the three typed
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
}

// ProjectStatus holds the observed state of a Project. Minimal in Phase 0.
type ProjectStatus struct {
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
