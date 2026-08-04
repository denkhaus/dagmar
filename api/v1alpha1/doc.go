// Package v1alpha1 contains dagmar's Kubernetes CRD types (API group dagmar.denkhaus.io).
//
// These are the declarative k8s surface of dagmar's domain model (CONTEXT.md). They are
// deliberately NOT shared with the execution-side domain types in .dagger/internal/domain/
// (different Go modules; ADR-0010 forbids the cross-module import). The CRD types are the
// declarative surface the controller reconciles; the .dagger domain types are the execution
// surface the Dagger module consumes; they map at the controller/module boundary
// (ADR-0012 HOUSE-2: deliberate duplication, not a DRY violation).
//
// Phase 0 (ADR-0012 §2) defines only Project + Run; the full CRD set
// {Project, Agent, Prompt, QualityGate, Trigger, Run} is added as the control plane grows.
//
// +kubebuilder:object:generate=true
// +groupName=dagmar.denkhaus.io
package v1alpha1
