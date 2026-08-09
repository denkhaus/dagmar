// gate.go — GateResult JSON contract, shared across modules (dagmar-481f).
//
// This is the standardized JSON contract that every project's dagmar-gate function MUST return.
// The gate producer (.dagmar/internal/workflows/gate.go) marshals it; the controller consumer
// (internal/controller/orchestration.go) unmarshals it. Both import from here — no duplication.

package manifest

// GateResult is the standardized JSON output contract for dagmar-gate. The gate NEVER returns
// an error — failures are represented as {"passed": false} with check details. The controller
// reads this from the pod's termination log; CI reads it from stdout.
type GateResult struct {
	// Passed is true when ALL checks succeeded. This is the single field the controller
	// gates on: true → gate green, false → gate red.
	Passed bool `json:"passed"`
	// Checks is the per-check outcome list (ordered).
	Checks []CheckResult `json:"checks"`
	// CoverageBps is the total go test coverage in basis points (0–10000, e.g. 7850 = 78.50%).
	// 0 when coverage was not measured (no floor set).
	CoverageBps int `json:"coverage_bps,omitempty"`
	// FloorBps is the coverage floor that was enforced (0 when not checked).
	FloorBps int `json:"floor_bps,omitempty"`
}

// CheckResult is the per-check outcome inside a GateResult.
type CheckResult struct {
	// Name is the check name (e.g. "controller", "coverage").
	Name string `json:"name"`
	// Passed is true when the check succeeded.
	Passed bool `json:"passed"`
	// Output contains failure details when Passed is false (truncated for the termination log).
	// Empty when the check passed.
	Output string `json:"output,omitempty"`
}
