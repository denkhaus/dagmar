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
	ctr, err := BuildSandbox(domain.SandboxSpec{Image: "alpine:3.20", Workdir: "/work"})
	if err != nil {
		t.Fatalf("BuildSandbox: %v", err)
	}
	// Sync forces realization against the engine (pulls the base image) — proves the
	// Tier-A-direct binding round-trips end to end.
	if _, err := ctr.Sync(context.Background()); err != nil {
		t.Fatalf("BuildSandbox Sync: %v", err)
	}
}

func TestBuildSandbox_validation_integration(t *testing.T) {
	// A malformed spec must fail at BuildSandbox's validation gate, NOT reach the engine.
	// (The integration tag is kept here so this stays next to the only caller-tier test,
	// even though it needs no engine.)
	if _, err := BuildSandbox(domain.SandboxSpec{Image: ""}); err == nil {
		t.Fatal("BuildSandbox: expected validation error for empty Image, got nil")
	}
}
