// Package manifest is dagmar's PUBLISHED platform manifest contract — the schema + parser for the
// in-repo conformance contract each Project exposes at .dagmar/project.yaml (ADR-0003).
//
// Authority: platform, by construction. This package IS the contract (analogous to the Project
// CRD in the K8s control plane): projects author INSTANCES, the platform owns the schema +
// validation. It was extracted out of the project module (.dagmar/internal/config) by dagmar-a1e0
// to close ADR-0014 GAP-1, where the manifest's project-module home was an interim forced by
// Dagger's source-isolated module loading — a relative sibling `replace` could not resolve at
// module-load time (the sibling dir is absent from the load context).
//
// Purity: the package has NO Dagger import — it is unit-testable without an engine. Turning a
// *dagger.Directory into manifest bytes is a Dagger I/O concern that lives in the consuming gate
// (workflows.Gate), not here.
//
// Resolution: both consumer modules (the platform .dagger and the project .dagmar) depend on this
// by a versioned `require` (a published remote dep), which resolves under Dagger's
// source-isolated module loading.
//
// TODO(dagmar-a1e0): record the concrete resolution mechanism here once the prototype gate
// confirms it — pseudo-version from commit vs git tag (manifest/vX.Y.Z), and whether a committed
// vendor/ offers a hermetic network-free fallback for the local dev loop.
package manifest
