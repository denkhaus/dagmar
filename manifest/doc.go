// Package manifest is dagmar's PUBLISHED shared contract module — types shared across all Go
// modules (root, .dagger, .dagmar, manifest). Contains: the manifest schema + parser (ADR-0003),
// the GateResult JSON contract (dagmar-481f), and the meta-prompt constants (ADR-0023 D9).
//
// This package IS the single source of truth for cross-module types. Every module that needs
// GateResult, CheckResult, or meta-prompts imports from here — no duplication.
//
// Originally the schema + parser for the
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
// module load — but only because the dagmar repo is PUBLIC. proxy.golang.org fetches
// github.com/denkhaus/dagmar/manifest over HTTPS (no auth) and the committed go.sum carries the
// hash, so NO GOPRIVATE is required on the proven path. A PRIVATE repo FAILS: the load container
// cannot authenticate to github.com over HTTPS (no git insteadOf / token in the container), and the
// SDK's load-time `go mod tidy` ignores a committed vendor/. GOPRIVATE=github.com/denkhaus/dagmar is
// only optional belt-and-suspenders to bypass the sum.golang.org crawl of a freshly-published module
// (it is not wired into CI/load and not needed here). For clean versioning, tag the subdir module
// manifest/vX.Y.Z (the prototype uses a pseudo-version from the extract commit; tag at merge to
// main). A local repo-root go.work (gitignored) gives instant cross-module dev without re-publishing.
//
// Version axes (distinct, do not conflate): `Version` below is the CONTRACT schema revision this
// library implements (v0.1.0); the MODULE release tag (manifest/vX.Y.Z) is the publish artifact.
// They are independent — a schema revision may ship across several module tags.
package manifest

// Versioning note (dagmar-481f): when manifest/ gains new exported types (GateResult, meta-prompts),
// the git tag manifest/vX.Y.Z must be updated and pushed AFTER the commit lands on main. Until then,
// go.work provides local resolution; published builds require the tag. The Version constant tracks
// the contract schema revision independently of the module release tag.
