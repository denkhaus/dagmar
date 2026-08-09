package manifest

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseManifest(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string // substring of the error; "" means no error expected
	}{
		{
			name: "valid two checkables",
			yaml: "checkables:\n" +
				"  - name: controller\n    workdir: .\n    command: go build ./...\n" +
				"  - name: dagger-module\n    workdir: .dagger\n    command: go test ./...\n",
			wantErr: "",
		},
		{
			name:    "no checkables",
			yaml:    "checkables: []\n",
			wantErr: "no checkables",
		},
		{
			name:    "missing name",
			yaml:    "checkables:\n  - workdir: .\n    command: go build ./...\n",
			wantErr: "must have both name and command",
		},
		{
			name:    "missing command",
			yaml:    "checkables:\n  - name: x\n    workdir: .\n",
			wantErr: "must have both name and command",
		},
		{
			name:    "workdir escapes via ..",
			yaml:    "checkables:\n  - name: x\n    workdir: ../etc\n    command: true\n",
			wantErr: `".." components not allowed`,
		},
		{
			name:    "workdir absolute",
			yaml:    "checkables:\n  - name: x\n    workdir: /etc\n    command: true\n",
			wantErr: "absolute paths not allowed",
		},
		{
			name:    "workdir empty",
			yaml:    "checkables:\n  - name: x\n    workdir: \"\"\n    command: true\n",
			wantErr: "empty",
		},
		{
			name:    "malformed yaml",
			yaml:    "checkables: [this is not : valid : yaml\n",
			wantErr: "parse .dagmar/project.yaml",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := ParseManifest([]byte(tt.yaml))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ParseManifest: unexpected error: %v", err)
				}
				if len(m.Checkables) == 0 {
					t.Fatalf("expected checkables, got none")
				}
			} else {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want substring %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

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
