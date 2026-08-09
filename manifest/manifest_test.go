package manifest

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGateResultJSONRoundTrip(t *testing.T) {
	result := GateResult{
		Passed:      true,
		CoverageBps: 8230,
		FloorBps:    7850,
		Checks: []CheckResult{
			{Name: "controller", Passed: true},
			{Name: "coverage", Passed: true},
		},
	}
	b, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed GateResult
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !parsed.Passed {
		t.Error("expected passed=true")
	}
	if parsed.CoverageBps != 8230 {
		t.Errorf("coverage_bps = %d, want 8230", parsed.CoverageBps)
	}
	if len(parsed.Checks) != 2 {
		t.Errorf("checks len = %d, want 2", len(parsed.Checks))
	}
}

func TestGateResultFailedNoCoverage(t *testing.T) {
	result := GateResult{
		Passed: false,
		Checks: []CheckResult{
			{Name: "controller", Passed: false, Output: "build error"},
		},
	}
	b, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed GateResult
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.Passed {
		t.Error("expected passed=false")
	}
	if parsed.CoverageBps != 0 {
		t.Errorf("coverage_bps = %d, want 0", parsed.CoverageBps)
	}
	if !strings.Contains(parsed.Checks[0].Output, "build error") {
		t.Errorf("output mismatch: %s", parsed.Checks[0].Output)
	}
}

// TestSharedTypesExist verifies that the shared contract types are exported and accessible.
// This is the duplication-prevention mechanism (dagmar-481f): manifest/ is the single source
// of truth — all modules import from here, so there is nothing to duplicate.
func TestSharedTypesExist(t *testing.T) {
	// GateResult contract
	g := GateResult{Passed: true, CoverageBps: 8230, FloorBps: 7850}
	if !g.Passed {
		t.Error("GateResult.Passed not settable")
	}
	c := CheckResult{Name: "test", Passed: true}
	if c.Name != "test" {
		t.Error("CheckResult.Name not settable")
	}

	// Meta-prompt constants
	if CoderMetaPrompt == "" {
		t.Error("CoderMetaPrompt is empty — go:embed failed")
	}
	if ReviewerMetaPrompt == "" {
		t.Error("ReviewerMetaPrompt is empty — go:embed failed")
	}
	if AdjudicatorMetaPrompt == "" {
		t.Error("AdjudicatorMetaPrompt is empty — go:embed failed")
	}
	if MetaPromptForPhase("pre-code") == "" {
		t.Error("MetaPromptForPhase(pre-code) returned empty")
	}
	if MetaPromptForPhase("pre-review") == "" {
		t.Error("MetaPromptForPhase(pre-review) returned empty")
	}
}
