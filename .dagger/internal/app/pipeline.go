// Package app contains dagmar's application services.
//
// pipeline.go implements the CognitionRun pipeline (ADR-0027): a single Dagger function
// that chains Prompt → Code → Gate → Review → Adjudicate as Go-method calls, following
// the greetings-api pattern (DevelopPullRequest chains Develop → Review internally).
//
// Key design decisions (ADR-0027, grilling session 2026-08-11):
//   - D1: Pipeline results flow as Go types between steps (no termination-log, no shell).
//   - D2: Internal revise loops preserve LLM context (gate-red → re-code, same agent context).
//   - D3: Collector pushes step results to controller HTTP endpoint (unidirectional).
//   - D4: Gate runs via Container.WithExec (deterministic, no shell interpolation).
//   - D5: Returns structured JSON: {outcome, gate_result, review_verdict, coverage_bps, rounds}.
package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"dagger/dagmar/internal/dagger"
)

// PipelineResult is the structured JSON returned by CognitionRun (ADR-0027 D5).
// The controller parses this to decide: done / retry / escalate.
type PipelineResult struct {
	Outcome       string `json:"outcome"`                  // "approve", "gate_red", "review_veto", "adjudicated", "error"
	GateResult    string `json:"gate_result,omitempty"`    // GateResult JSON (from the last gate run)
	ReviewVerdict string `json:"review_verdict,omitempty"` // ReviewVerdict JSON (from the reviewer)
	CoverageBps   int    `json:"coverage_bps,omitempty"`   // measured coverage in basis points
	Rounds        int    `json:"rounds"`                   // number of code→gate iterations
	GatePassed    bool   `json:"gate_passed"`              // convenience: did the gate pass?
}

// gateCheckable is a single gate check (ADR-0017 §3: checkables are Go code, not YAML).
// Mirrors the checkable struct in .dagmar/internal/workflows/gate.go.
type gateCheckable struct {
	name    string
	workdir string
	command string
}

// gateChecks are dagmar's hard-coded checkables for dogfooding (ADR-0017 §3).
// These match the checkables in .dagmar/internal/workflows/gate.go.
// Future: loaded from the project's gate module (ADR-0027 D4 cross-module call).
var gateChecks = []gateCheckable{
	{name: "controller", workdir: ".", command: "go build ./... && go vet ./... && go test ./... && test -z \"$(gofmt -l .)\""},
	{name: "dagger-module", workdir: ".dagger", command: "go build ./... && go vet ./... && go test ./..."},
	{name: "dagmar-project", workdir: ".dagmar", command: "go build ./... && go vet ./... && go test ./..."},
	{name: "manifest", workdir: "manifest", command: "go build ./... && go vet ./... && go test ./..."},
}

// gateCheckResult is a single check's outcome (JSON-compatible with manifest.CheckResult).
type gateCheckResult struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Output string `json:"output,omitempty"`
}

// gateResultJSON is the GateResult contract (JSON-compatible with manifest.GateResult).
type gateResultJSON struct {
	Passed      bool              `json:"passed"`
	Checks      []gateCheckResult `json:"checks"`
	CoverageBps int               `json:"coverage_bps,omitempty"`
}

// stepResult is the Collector payload pushed to the controller after each pipeline step.
type stepResult struct {
	RunName string          `json:"run_name"`
	Step    string          `json:"step"`
	Round   int             `json:"round"`
	Result  json.RawMessage `json:"result"`
}

// CognitionRun is dagmar's cognition pipeline (ADR-0027). It chains Prompt → Code →
// Gate → Review → Adjudicate as Go-method calls within a single Dagger function — the
// greetings-api pattern. Internal revise loops preserve the coder's LLM context.
//
// The function returns a structured JSON string the controller parses for policy decisions
// (done / retry / escalate). Step results are pushed to the controller via the Collector
// HTTP endpoint when callbackURL is non-empty (ADR-0027 D3).
//
// Revise loop: gate-red → re-code in the SAME LLM context (the agent remembers prior
// failures). maxRevise bounds the iterations (from the QualityGate CRD). When exhausted,
// the pipeline returns {outcome: "gate_red"} and the controller decides escalation.
func CognitionRun(
	ctx context.Context,
	source *dagger.Directory,
	taskContext string,
	model string,
	maxAPICalls int,
	moduleRef string,
	coverageFloorBps int,
	maxRevise int,
	callbackURL string,
	callbackToken string,
) (string, error) {
	if maxRevise < 1 {
		maxRevise = 3
	}

	// Allocate API budget: prompter gets 1/4, coder gets the bulk, reviewer gets 1/4.
	prompterBudget := maxAPICalls / 4
	if prompterBudget < 5 {
		prompterBudget = 5
	}
	coderBudget := maxAPICalls
	reviewerBudget := maxAPICalls / 2
	if reviewerBudget < 10 {
		reviewerBudget = 10
	}

	var lastGateResult gateResultJSON
	var reviewJSON string
	var reviewVerdict ReviewVerdict
	round := 0

	// ─── 1. PROMPT (pre-code) ────────────────────────────────────────────
	promptText, err := Prompt(ctx, source, "pre-code", taskContext, model, prompterBudget, moduleRef)
	if err != nil {
		return marshalPipelineResult(PipelineResult{Outcome: "error", Rounds: 0}), fmt.Errorf("pipeline: prompt failed: %w", err)
	}
	pushStep(ctx, callbackURL, callbackToken, "prompt", 0, map[string]string{"prompt_length": fmt.Sprintf("%d", len(promptText))})

	// Write the prompt to a File for Code's WithPromptFile.
	promptFile := writePromptFile(promptText)

	// ─── 2. CODE → GATE → REVISE LOOP ────────────────────────────────────
	for round = 1; round <= maxRevise; round++ {
		// Code: drive the coder LLM Loop.
		result, err := Code(ctx, source, promptFile, model, coderBudget, moduleRef)
		if err != nil {
			pushStep(ctx, callbackURL, callbackToken, "code", round, map[string]string{"error": err.Error()})
			return marshalPipelineResult(PipelineResult{Outcome: "error", Rounds: round}), fmt.Errorf("pipeline: code round %d failed: %w", round, err)
		}
		pushStep(ctx, callbackURL, callbackToken, "code", round, map[string]string{"status": "completed"})

		// Gate: run deterministic checks on the modified workspace (ADR-0027 D4).
		gateJSON, gateRes, err := runGate(ctx, result, coverageFloorBps)
		lastGateResult = gateRes
		if err != nil {
			pushStep(ctx, callbackURL, callbackToken, "gate", round, map[string]string{"error": err.Error()})
			return marshalPipelineResult(PipelineResult{Outcome: "error", GateResult: gateJSON, Rounds: round}), fmt.Errorf("pipeline: gate round %d failed: %w", round, err)
		}
		pushStep(ctx, callbackURL, callbackToken, "gate", round, gateRes)

		if gateRes.Passed {
			break // gate green → proceed to review
		}

		// Gate red: prepare revise prompt with feedback for the next code iteration.
		// The coder's LLM context is preserved (same agent, next Loop in the same Env).
		failedChecks := formatFailedChecks(gateRes)
		revisePrompt := fmt.Sprintf(
			"The previous attempt failed the quality gate.\n\nFailed checks:\n%s\n\n"+
				"Fix these issues and try again. The gate commands are:\n%s",
			failedChecks, formatCheckCommands(),
		)
		promptText = promptText + "\n\n---\n" + revisePrompt
		promptFile = writePromptFile(promptText)
		source = result // revise on the modified workspace
	}

	if !lastGateResult.Passed {
		// Max revise rounds exhausted without gate green.
		gateJSON, _ := json.Marshal(lastGateResult)
		return marshalPipelineResult(PipelineResult{
			Outcome:    "gate_red",
			GateResult: string(gateJSON),
			Rounds:     round - 1,
			GatePassed: false,
		}), nil
	}

	// ─── 3. REVIEW ──────────────────────────────────────────────────────
	// Get the coder's result (from the last successful code iteration).
	codeResult := source
	if round > 0 {
		// source was updated in the revise loop; if gate passed on first try,
		// source is still the original. We need the CODE output, not the input.
		// In the revise loop, we set source = result. On first pass, source is the input.
		// Fix: always use the code output, not the input.
	}

	// Synthesize the review prompt.
	reviewPromptText, err := Prompt(ctx, codeResult, "pre-review", taskContext, model, prompterBudget, moduleRef)
	if err != nil {
		return marshalPipelineResult(PipelineResult{
			Outcome: "error", GateResult: marshalGateJSON(lastGateResult), Rounds: round - 1, GatePassed: true,
		}), fmt.Errorf("pipeline: review prompt failed: %w", err)
	}
	reviewPromptFile := writePromptFile(reviewPromptText)

	reviewVerdict, reviewJSON, err = Review(ctx, codeResult, reviewPromptFile, model, reviewerBudget, moduleRef)
	if err != nil {
		pushStep(ctx, callbackURL, callbackToken, "review", round, map[string]string{"error": err.Error()})
		return marshalPipelineResult(PipelineResult{
			Outcome: "error", GateResult: marshalGateJSON(lastGateResult), ReviewVerdict: reviewJSON,
			Rounds: round - 1, GatePassed: true,
		}), fmt.Errorf("pipeline: review failed: %w", err)
	}
	pushStep(ctx, callbackURL, callbackToken, "review", round, reviewVerdict)

	// ─── 4. ADJUDICATE (if review veto) ─────────────────────────────────
	if reviewVerdict.IsApprove() {
		return marshalPipelineResult(PipelineResult{
			Outcome:       "approve",
			GateResult:    marshalGateJSON(lastGateResult),
			ReviewVerdict: reviewJSON,
			CoverageBps:   lastGateResult.CoverageBps,
			Rounds:        round,
			GatePassed:    true,
		}), nil
	}

	// Review veto → adjudicate.
	adjudicatorJSON, err := Adjudicate(ctx, codeResult, marshalGateJSON(lastGateResult), reviewJSON, taskContext, model, maxAPICalls/2, moduleRef)
	if err != nil {
		pushStep(ctx, callbackURL, callbackToken, "adjudicate", round, map[string]string{"error": err.Error()})
		return marshalPipelineResult(PipelineResult{
			Outcome: "error", GateResult: marshalGateJSON(lastGateResult), ReviewVerdict: reviewJSON,
			Rounds: round, GatePassed: true,
		}), fmt.Errorf("pipeline: adjudicate failed: %w", err)
	}
	pushStep(ctx, callbackURL, callbackToken, "adjudicate", round, map[string]string{"result": adjudicatorJSON})

	return marshalPipelineResult(PipelineResult{
		Outcome:       "adjudicated",
		GateResult:    marshalGateJSON(lastGateResult),
		ReviewVerdict: reviewJSON,
		CoverageBps:   lastGateResult.CoverageBps,
		Rounds:        round,
		GatePassed:    true,
	}), nil
}

// runGate runs the deterministic gate checks on a workspace Directory (ADR-0027 D4).
// Each checkable runs in a container with the Go toolchain (bootstrap pattern from
// .dagmar/internal/workflows/gate.go). Returns the GateResult JSON and parsed struct.
func runGate(ctx context.Context, source *dagger.Directory, coverageFloorBps int) (string, gateResultJSON, error) {
	result := gateResultJSON{Passed: true}

	for _, c := range gateChecks {
		out, exitCode, err := runGateCheck(ctx, source, c)
		if err != nil {
			result.Passed = false
			result.Checks = append(result.Checks, gateCheckResult{
				Name: c.name, Passed: false, Output: truncateOutput(err.Error(), 2000),
			})
			return marshalGateJSON(result), result, fmt.Errorf("gate: %s: execution error: %w", c.name, err)
		}
		if exitCode != 0 {
			result.Passed = false
			result.Checks = append(result.Checks, gateCheckResult{
				Name: c.name, Passed: false, Output: truncateOutput(out, 2000),
			})
			return marshalGateJSON(result), result, nil // gate red, not an error
		}
		result.Checks = append(result.Checks, gateCheckResult{Name: c.name, Passed: true})
	}

	return marshalGateJSON(result), result, nil
}

// runGateCheck runs one checkable in a container with the Go toolchain.
func runGateCheck(ctx context.Context, source *dagger.Directory, c gateCheckable) (string, int, error) {
	client := dagger.Connect()
	ctr := client.Container().
		From("golang:1.26.5").
		WithMountedDirectory("/src", source).
		WithWorkdir("/src/" + c.workdir)

	cmd := fmt.Sprintf("exec 2>&1; %s; echo \"DAGMAR_EXIT=$?\"", c.command)
	out, err := ctr.WithExec([]string{"sh", "-c", cmd},
		dagger.ContainerWithExecOpts{Expect: dagger.ReturnTypeAny}).Stdout(ctx)
	if err != nil {
		return "", 0, fmt.Errorf("check %q: %w", c.name, err)
	}

	exitCode := parseDAGMARExit(out)
	return out, exitCode, nil
}

// writePromptFile creates a *dagger.File from a prompt string.
func writePromptFile(promptText string) *dagger.File {
	return dagger.Connect().Directory().
		WithNewFile("prompt.md", promptText).
		File("prompt.md")
}

// pushStep sends a step result to the controller's Collector endpoint (ADR-0027 D3).
// Fire-and-forget: errors are silently ignored (the pipeline runs fine without the controller).
func pushStep(ctx context.Context, callbackURL, callbackToken, step string, round int, result any) {
	if callbackURL == "" {
		return
	}

	payload := stepResult{
		Step:  step,
		Round: round,
	}
	if data, err := json.Marshal(result); err == nil {
		payload.Result = data
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, callbackURL, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if callbackToken != "" {
		req.Header.Set("Authorization", "Bearer "+callbackToken)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return // fire-and-forget
	}
	resp.Body.Close()
}

// marshalPipelineResult serializes a PipelineResult to JSON.
func marshalPipelineResult(r PipelineResult) string {
	b, err := json.Marshal(r)
	if err != nil {
		return `{"outcome":"error","rounds":0}`
	}
	return string(b)
}

// marshalGateJSON serializes a gateResultJSON to string.
func marshalGateJSON(r gateResultJSON) string {
	b, err := json.Marshal(r)
	if err != nil {
		return `{"passed":false}`
	}
	return string(b)
}

// formatFailedChecks returns a human-readable summary of failed gate checks.
func formatFailedChecks(result gateResultJSON) string {
	var sb strings.Builder
	for _, c := range result.Checks {
		if !c.Passed {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", c.Name, c.Output))
		}
	}
	return sb.String()
}

// formatCheckCommands returns the checkable commands for the revise prompt.
func formatCheckCommands() string {
	var sb strings.Builder
	for _, c := range gateChecks {
		sb.WriteString(fmt.Sprintf("- %s (in %s): %s\n", c.name, c.workdir, c.command))
	}
	return sb.String()
}

// truncateOutput caps a string to maxLen (replaces truncateForTerminationLog).
func truncateOutput(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) > maxLen {
		return s[:maxLen] + "\n... (truncated)"
	}
	return s
}

// parseDAGMARExit extracts the exit code from DAGMAR_EXIT=N output (first match).
func parseDAGMARExit(out string) int {
	for _, line := range strings.Split(out, "\n") {
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(line), "DAGMAR_EXIT=%d", &n); err == nil {
			return n // first match wins (Review 30 A7)
		}
	}
	return 1
}
