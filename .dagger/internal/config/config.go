// Package config holds dagmar's configuration. Two sources, clearly separated
// (ADR-0010 §8): runtime/environment config via envconfig sub-structs (KB
// guide.golang.config), and the declarative ProjectManifest (.dagmar/project.yaml,
// ADR-0003) for per-Project conformance. Env for runtime (engine host, toggles), manifest
// for declarative per-Project conformance.
package config

// OsEcoConfig holds the os-eco runtime binding (env-driven subset). Per-Project manifest
// binding (store paths) is carried by the ProjectManifest and surfaced via main.OsEcoBinding
// at the constructor seam. envconfig tags follow KB guide.golang.config (prefix added at a
// parent struct when a resolver is wired in).
type OsEcoConfig struct {
	Seeds  string `envconfig:"SEEDS"`
	Mulch  string `envconfig:"MULCH"`
	Canopy string `envconfig:"CANOPY"`
}
