// gate.go — dagmar-gate, the always-Dagger verify wrapper (ADR-0009 §2 / ADR-0012 §4 / ADR-0017 §3).
package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"dagger/dagmar-project/internal/dagger"
	"github.com/denkhaus/dagmar/manifest"
)

// checkable is a hard-coded gate check (ADR-0017 §3: checkables are Go code, NOT YAML).
type checkable struct {
	name    string
	workdir string
	command string
}

// gateChecks is dagmar's hard-coded checkable list (ADR-0017 §3).
var gateChecks = []checkable{
	{name: "controller", workdir: ".", command: "go build ./... && go vet ./... && go test ./... && test -z \"$(gofmt -l .)\""},
	{name: "dagger-module", workdir: ".dagger", command: "go build ./... && go vet ./... && go test ./..."},
	{name: "dagmar-project", workdir: ".dagmar", command: "go build ./... && go vet ./... && go test ./..."},
	{name: "manifest", workdir: "manifest", command: "go build ./... && go vet ./... && go test ./..."},
	{name: "secrets", workdir: ".", command: "betterleaks dir ."},
}

// Gate is dagmar-gate: the always-Dagger verify wrapper. Runs hard-coded Go checkables
// (ADR-0017 §3) and returns a structured JSON GateResult — NEVER an error. Callers (CI,
// controller) check the "passed" field. Coverage ratcheting (dagmar-4154) runs when
// coverageFloorBps > 0.
//
// The JSON output is the contract: it flows to the pod's /dev/termination-log, where the
// controller reads pod.Status.ContainerStatuses[0].State.Terminated.Message.
func Gate(ctx context.Context, source *dagger.Directory, githubToken *dagger.Secret, coverageFloorBps int) (string, error) {
	result := manifest.GateResult{Passed: true}

	for _, c := range gateChecks {
		out, exit, err := runCheck(ctx, source, githubToken, c)
		if err != nil {
			result.Passed = false
			result.Checks = append(result.Checks, manifest.CheckResult{
				Name: c.name, Passed: false,
				Output: fmt.Sprintf("execution error: %v", err),
			})
			return marshalGate(result)
		}
		if exit != 0 {
			result.Passed = false
			result.Checks = append(result.Checks, manifest.CheckResult{
				Name: c.name, Passed: false,
				Output: truncateForTerminationLog(out),
			})
			return marshalGate(result)
		}
		result.Checks = append(result.Checks, manifest.CheckResult{Name: c.name, Passed: true})
	}

	// Coverage check (dagmar-4154).
	if coverageFloorBps > 0 {
		coverageBps, err := runCoverageCheck(ctx, source, githubToken, coverageFloorBps)
		if err != nil {
			result.Passed = false
			result.CoverageBps = 0
			result.FloorBps = coverageFloorBps
			result.Checks = append(result.Checks, manifest.CheckResult{
				Name: "coverage", Passed: false,
				Output: truncateForTerminationLog(err.Error()),
			})
			return marshalGate(result)
		}
		result.CoverageBps = coverageBps
		result.FloorBps = coverageFloorBps
		result.Checks = append(result.Checks, manifest.CheckResult{Name: "coverage", Passed: true})
	}

	return marshalGate(result)
}

// marshalGate serializes a GateResult to JSON. Never returns an error (the struct is simple).
func marshalGate(r manifest.GateResult) (string, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return `{"passed":false,"checks":[{"name":"marshal","passed":false,"output":"json marshal failed"}]}`, nil
	}
	return string(b), nil
}

// truncateForTerminationLog caps a string to fit within the K8s termination-message limit
// (4096 bytes total, but we leave room for the JSON envelope).
func truncateForTerminationLog(s string) string {
	const maxOutputLen = 2000
	s = strings.TrimSpace(s)
	if len(s) > maxOutputLen {
		return s[:maxOutputLen] + "\n... (truncated)"
	}
	return s
}

// runCoverageCheck runs `go test -coverprofile=coverage.out ./...` in the root module,
// parses total coverage, and returns it in basis points. Returns an error if below floor
// or if the tests themselves fail.
func runCoverageCheck(ctx context.Context, source *dagger.Directory, githubToken *dagger.Secret, floorBps int) (int, error) {
	ctr := bootstrapBase(source, githubToken).WithWorkdir("/src")

	cmd := `exec 2>&1; go test -coverprofile=coverage.out ./...; echo "DAGMAR_EXIT=$?"`
	out, err := ctr.WithExec([]string{"sh", "-c", cmd},
		dagger.ContainerWithExecOpts{Expect: dagger.ReturnTypeAny}).Stdout(ctx)
	if err != nil {
		return 0, fmt.Errorf("dagmar-gate: coverage: go test: %w", err)
	}
	if exit := parseDagmarExit(out); exit != 0 {
		return 0, fmt.Errorf("dagmar-gate: coverage: go test FAILED (exit %d)\n--- output ---\n%s",
			exit, strings.TrimSpace(out))
	}

	coverOut, err := ctr.WithExec([]string{"sh", "-c", "go tool cover -func=coverage.out"}).Stdout(ctx)
	if err != nil {
		return 0, fmt.Errorf("dagmar-gate: coverage: go tool cover: %w", err)
	}
	coverageBps, ok := parseCoverTotal(coverOut)
	if !ok {
		return 0, fmt.Errorf("dagmar-gate: coverage: could not parse total from `go tool cover`:\n%s",
			strings.TrimSpace(coverOut))
	}

	if coverageBps < floorBps {
		return 0, fmt.Errorf("dagmar-gate: coverage BELOW floor: %s < %s",
			formatBps(coverageBps), formatBps(floorBps))
	}
	return coverageBps, nil
}

func parseCoverTotal(coverOut string) (int, bool) {
	lines := strings.Split(coverOut, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "total:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		pctStr := strings.TrimSuffix(fields[len(fields)-1], "%")
		pct, err := strconv.ParseFloat(pctStr, 64)
		if err != nil {
			continue
		}
		bps := int(pct*100 + 0.5)
		return bps, true
	}
	return 0, false
}

func formatBps(bps int) string {
	return fmt.Sprintf("%.2f%%", float64(bps)/100)
}

// runCheck runs one hard-coded checkable and returns (stdout, exitCode, err).
func runCheck(ctx context.Context, source *dagger.Directory, githubToken *dagger.Secret, c checkable) (string, int, error) {
	ctr := bootstrapBase(source, githubToken).
		WithWorkdir("/src/" + c.workdir)
	cmd := `exec 2>&1; ` + c.command + `; echo "DAGMAR_EXIT=$?"`
	out, err := ctr.WithExec([]string{"sh", "-c", cmd},
		dagger.ContainerWithExecOpts{Expect: dagger.ReturnTypeAny}).Stdout(ctx)
	if err != nil {
		return "", 0, err
	}
	exit := parseDagmarExit(out)
	return out, exit, nil
}

func parseDagmarExit(out string) int {
	last := -1
	for _, line := range strings.Split(out, "\n") {
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(line), "DAGMAR_EXIT=%d", &n); err == nil {
			last = n
		}
	}
	if last < 0 {
		return 1
	}
	return last
}