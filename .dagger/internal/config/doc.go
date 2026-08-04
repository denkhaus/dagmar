// Package config holds dagmar's configuration: runtime/environment config (envconfig-style
// tags — the resolver is TBD) and the in-repo ProjectManifest types + YAML parser (ADR-0003,
// ADR-0010 §8). The package is PURE — no Dagger import — so it is unit-testable without an
// engine. Reading a manifest out of a *dagger.Directory is a Dagger I/O concern that lives in
// the workflows/ layer (workflows.Gate); this package only turns raw bytes into a typed manifest.
package config
