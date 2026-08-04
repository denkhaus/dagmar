// Package controller contains dagmar's Kubernetes controllers — the reconcilers that turn
// declarative CRDs into observed cluster state. Phase 0 (ADR-0012 §2) ships the RunReconciler,
// which reconciles a Run CR into an agent pod that invokes the dagmar module against the
// singleton Dagger engine, and writes the result back as Run status (status conditions + phase).
//
// The controller is intentionally lean (no webhooks / leader-election / conversion — ADR-0012
// §3); those grow as the control plane does, tracked in the control-plane-design seed.
package controller
