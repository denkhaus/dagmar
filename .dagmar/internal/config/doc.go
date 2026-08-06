// Package config holds the in-repo ProjectManifest types + YAML parser (ADR-0003). This is the
// PROJECT-scope half of dagmar's config (ADR-0014): it travels with the gate-family in the
// project module (.dagmar/). The package is PURE — no Dagger import — so it is unit-testable
// without an engine. Reading a manifest out of a *dagger.Directory is a Dagger I/O concern that
// lives in the workflows/ layer (workflows.Gate); this package only turns raw bytes into a typed
// manifest.
//
// The runtime/env OsEcoConfig half is PLATFORM-scope and stays in .dagger/internal/config/
// (ADR-0014 GAP-1: config was mixed-scope and splits along with this module boundary). See
// manifest.go for why the manifest schema — though platform authority — lives here rather than in
// a sibling module (Dagger's source-isolated module loading).
package config
