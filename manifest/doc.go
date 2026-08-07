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
// Resolution (CONFIRMED by the dagmar-a1e0 prototype gate, 2026-08-07): a versioned `require`
// (pseudo-version from this module's published commit) DOES resolve at Dagger's source-isolated
// module load — but only because the dagmar repo is PUBLIC, so Go fetches
// github.com/denkhaus/dagmar/manifest over HTTPS without auth. A PRIVATE repo FAILS: the load
// container cannot authenticate to github.com over HTTPS (no git insteadOf / token in the
// container, and the SDK's load-time `go mod tidy` ignores a committed vendor/). GOPRIVATE=
// github.com/denkhaus/dagmar skips the public proxy + sumdb (needed for a newly-published module
// until sum.golang.org records it). For clean versioning, tag the subdir module manifest/vX.Y.Z
// (the prototype uses a pseudo-version from the extract commit; tag at merge to main). A local
// repo-root go.work (gitignored) gives instant cross-module dev without re-publishing.
package manifest
