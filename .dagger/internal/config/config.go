// Package config holds dagmar's configuration. Two sources, clearly separated
// (ADR-0010 §8): runtime/environment config via envconfig-STYLE struct tags (KB
// guide.golang.config; the env resolver is not yet wired — these tags are prospective until
// one lands), and the declarative ProjectManifest (.dagmar/project.yaml, ADR-0003) for
// per-Project conformance. Env for runtime (engine host, toggles), manifest for declarative
// per-Project conformance.
package config

// OsEcoConfig is the env/runtime view of the os-eco backing-service binding (seeds /
// mulch / canopy store paths). It is the SAME triple as main.OsEcoBinding (the
// constructor-seam binding) viewed from the env side. The two reconcile at runtime
// (ADR-0010 §8): the per-Project constructor binding (main.OsEcoBinding, sourced from the
// ProjectManifest) is AUTHORITATIVE for which store paths a Run uses; OsEcoConfig is the
// env-override surface, resolved only once an env reader is wired. Today only the
// constructor seam is live — these envconfig tags are prospective (resolver TBD).
type OsEcoConfig struct {
	Seeds  string `envconfig:"SEEDS"`
	Mulch  string `envconfig:"MULCH"`
	Canopy string `envconfig:"CANOPY"`
}
