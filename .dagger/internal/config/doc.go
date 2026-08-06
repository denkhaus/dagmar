// Package config holds dagmar's PLATFORM runtime/environment config (ADR-0010 §8): the
// envconfig-STYLE OsEcoConfig — the env-override surface for the os-eco backing-service binding
// (seeds/mulch/canopy), prospective until an env reader is wired (KB guide.golang.config).
//
// The manifest / PROJECT-scope half (ProjectManifest types + YAML parser, ADR-0003) traveled
// with the gate-family to the project module at the ADR-0014 scope split — see
// .dagmar/internal/config/. This package is the platform runtime side only.
package config
