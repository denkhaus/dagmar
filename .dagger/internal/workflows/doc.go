// Package workflows holds dagmar's reusable COMPOSED workflow units — the named pipelines
// (gate, bootstrap, review) assembled from tools + ports + domain (ADR-0009, ADR-0010 §4).
// The cbb8 spike's Up/DeployEngine/Probe logic in main.go is the seed of a future
// bootstrap workflow to be refactored in here.
package workflows
