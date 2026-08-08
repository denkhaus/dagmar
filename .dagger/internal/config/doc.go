// Package config holds dagmar's PLATFORM runtime/environment config (ADR-0010 §8).
// Today this is a placeholder; concrete config types land as the control plane develops.
//
// The manifest / PROJECT-scope half (ProjectManifest types + YAML parser, ADR-0003)
// traveled with the gate-family to the project module at the ADR-0014 scope split — see
// .dagmar/internal/config/. This package is the platform runtime side only.
package config
