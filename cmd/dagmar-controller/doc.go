// Package main is the dagmar-controller binary entrypoint. It wires the CRD types into a
// controller-runtime manager and registers the RunReconciler. Phase 0 runs it locally against a
// kind cluster (`just run`); in-cluster deployment (Increment 2) builds the Dockerfile image.
package main
