//go:build integration

// Integration tests exercise the Tier-A-direct seam via a REAL Dagger engine. They are
// excluded from the default `go test` run (no -tags=integration) so the unit suite stays
// engine-free and fast. Run deliberately: `go test -tags=integration ./internal/app/...`.
package app

import (
	"context"
	"testing"

	"dagger/dagmar/internal/domain"
)

func TestBuildSandbox_integration(t *testing.T) {
	ctr := BuildSandbox(domain.SandboxSpec{Image: "alpine:3.20", Workdir: "/work"})
	// Sync forces realization against the engine (pulls the base image) — proves the
	// Tier-A-direct binding round-trips end to end.
	if _, err := ctr.Sync(context.Background()); err != nil {
		t.Fatalf("BuildSandbox Sync: %v", err)
	}
}
