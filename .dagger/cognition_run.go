// cognition_run.go — CognitionRun pipeline as a Dagger Custom Object (ADR-0027).
//
// The struct holds the pipeline state in exported fields. Each step is a method
// that reads from and writes to these fields. Dagger serializes the state between
// calls, enabling CLI chaining:
//
//	dagger call cognition-run --source . --task-context "..." with-prompt --phase pre-code | with-code | with-gate | with-review | result
//
// The gate step calls the project's dagmar-gate Hook via a typed cross-module call:
// dag.Dagmar().DagmarGate(ctx, source). The project defines what the gate is (ADR-0014).
package main

import (
	"context"
	"encoding/json"

	"dagger/dagmar/internal/app"
	"dagger/dagmar/internal/dagger"
)

// CognitionRun is the pipeline state object (ADR-0027 D1).
// Exported fields are serialized by Dagger between chained method calls.
type CognitionRun struct {
	// Input configuration (set by constructor).
	Source           *dagger.Directory
	TaskContext      string
	Model            string
	MaxAPICalls      int
	ModuleRef        string
	CoverageFloorBps int
	MaxRevise        int
	CallbackURL      string
	CallbackToken    string

	// Accumulated pipeline state (written by step methods via mergeResult).
	PromptText string            `json:"prompt_text,omitempty"`
	PromptFile *dagger.File      `json:"-"`
	Workspace  *dagger.Directory `json:"-"`
	GateJSON   string            `json:"gate_result,omitempty"`
	GatePassed bool              `json:"gate_passed"`
	ReviewJSON string            `json:"review_verdict,omitempty"`
	Rounds     int               `json:"rounds"`
}

// StepResult is the unified schema for all pipeline step outputs (ADR-0027 D3).
// Every step method (WithPrompt, WithCode, WithGate, etc.) wraps its result in a
// StepResult, then calls mergeResult to mutate the shared CognitionRun state.
// This gives us a single merge point, a single schema for Collector pushes, and
// a uniform serialization shape for the controller.
type StepResult struct {
	Step   string          `json:"step"`   // "prompt" | "code" | "gate" | "review" | "adjudicate"
	Status string          `json:"status"` // "ok" | "error"
	Round  int             `json:"round,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// NewCognitionRun constructs the pipeline state object.
func NewCognitionRun(
	source *dagger.Directory,
	taskContext string,
	model string,
	maxAPICalls int,
	moduleRef string,
	coverageFloorBps int,
	maxRevise int,
	callbackURL string,
	callbackToken string,
) *CognitionRun {
	if maxRevise < 1 {
		maxRevise = 3
	}
	return &CognitionRun{
		Source:           source,
		TaskContext:      taskContext,
		Model:            model,
		MaxAPICalls:      maxAPICalls,
		ModuleRef:        moduleRef,
		CoverageFloorBps: coverageFloorBps,
		MaxRevise:        maxRevise,
		CallbackURL:      callbackURL,
		CallbackToken:    callbackToken,
	}
}

// mergeResult is the central state-mutation point (ADR-0027).
// Each step's output is funneled through here — one schema, one merge function.
// The switch dispatches on Step and updates the relevant CognitionRun fields.
func (c *CognitionRun) mergeResult(r StepResult) {
	if r.Status != "ok" {
		return
	}

	switch r.Step {
	case "prompt":
		var data struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(r.Data, &data); err == nil {
			c.PromptText = data.Text
			c.PromptFile = dag.Directory().
				WithNewFile("prompt.md", data.Text).
				File("prompt.md")
		}

	case "code":
		// Code output is a *dagger.Directory — stored directly, not via JSON.
		// The WithCode method sets c.Workspace before calling mergeResult.
		// mergeResult only tracks the round here.
		c.Rounds = r.Round

	case "gate":
		// Gate result is GateResult JSON — stored raw + parsed for convenience.
		c.GateJSON = string(r.Data)
		var parsed struct {
			Passed bool `json:"passed"`
		}
		json.Unmarshal(r.Data, &parsed)
		c.GatePassed = parsed.Passed

	case "review":
		c.ReviewJSON = string(r.Data)

	case "adjudicate":
		c.ReviewJSON = string(r.Data)
	}
}

// okResult builds a successful StepResult from any data payload.
func okResult(step string, round int, data any) StepResult {
	raw, _ := json.Marshal(data)
	return StepResult{Step: step, Status: "ok", Round: round, Data: raw}
}

// errorResult builds an error StepResult.
func errorResult(step string, round int, err error) StepResult {
	return StepResult{Step: step, Status: "error", Round: round, Error: err.Error()}
}

// WithPrompt synthesizes a coder prompt via the Prompter-LLM (ADR-0023 D1).
//
// Delegates to app.Prompt — the existing, tested prompter function that reads
// project source, issues, and memory, then synthesizes a tailored prompt.
// Meta-prompts are go:embed files (coder-meta.md / reviewer-meta.md), not inline
// strings (ADR-0023 D9).
//
// The phase selects which meta-prompt drives synthesis:
//   - "pre-code": synthesizes the prompt the coder needs
//   - "pre-review": synthesizes the prompt the reviewer needs
func (c *CognitionRun) WithPrompt(
	ctx context.Context,
	// phase selects the meta-prompt: "pre-code" (coder) or "pre-review" (reviewer).
	// +optional
	// +default="pre-code"
	phase string,
) *CognitionRun {
	promptText, err := app.Prompt(ctx, c.Source, phase, c.TaskContext, c.Model, c.prompterBudget(), c.ModuleRef)
	if err != nil {
		c.mergeResult(errorResult("prompt", c.Rounds, err))
		return c
	}

	c.mergeResult(okResult("prompt", c.Rounds, map[string]string{"text": promptText}))
	return c
}

// WithCode runs the coder LLM Loop on the workspace (ADR-0021 D2).
//
// Delegates to app.Code — the existing, tested coder function that builds the Env
// (Privileged + Writable + DirectoryInput/Output), drives the LLM Loop, and returns
// the modified workspace Directory.
//
// Reads from c.Source (the workspace) and c.PromptFile (the synthesized prompt from
// WithPrompt). Stores the modified workspace in c.Workspace.
func (c *CognitionRun) WithCode(ctx context.Context) *CognitionRun {
	result, err := app.Code(ctx, c.Source, c.PromptFile, c.Model, c.MaxAPICalls, c.ModuleRef)
	if err != nil {
		c.mergeResult(errorResult("code", c.Rounds+1, err))
		return c
	}

	c.Workspace = result
	c.mergeResult(okResult("code", c.Rounds+1, nil))
	return c
}

// WithGate runs the project's dagmar-gate Hook (ADR-0027 D4).
//
// Calls dag.Dagmar().DagmarGate — a typed cross-module call to the project's
// conformant module. The project defines what the gate checks (ADR-0014, ADR-0017 §3).
// No gate logic is duplicated here; the project's gate runs as-is.
func (c *CognitionRun) WithGate(
	ctx context.Context,
) *CognitionRun {
	opts := dagger.DagmarDagmarGateOpts{
		CoverageFloorBps: c.CoverageFloorBps,
	}

	gateJSON, err := dag.Dagmar().DagmarGate(ctx, c.Workspace, opts)
	if err != nil {
		c.mergeResult(errorResult("gate", c.Rounds, err))
		return c
	}

	// gateJSON is raw GateResult JSON — feed it directly into mergeResult.
	c.mergeResult(okResult("gate", c.Rounds, json.RawMessage(gateJSON)))
	return c
}

// WithReview runs the reviewer LLM Loop (ADR-0024 D4).
//
// Delegates to app.Review — the existing, tested reviewer function that builds
// a read-only Env, drives the LLM Loop, and extracts a structured ReviewVerdict
// via WithJSONValueOutput (ADR-0025).
//
// Reads from c.Workspace (the coder's modified workspace) and c.PromptFile (the
// synthesized review prompt from WithPrompt with phase "pre-review").
func (c *CognitionRun) WithReview(ctx context.Context) *CognitionRun {
	_, rawJSON, err := app.Review(ctx, c.Workspace, c.PromptFile, c.Model, c.reviewerBudget(), c.ModuleRef)
	if err != nil {
		c.mergeResult(errorResult("review", c.Rounds, err))
		return c
	}

	c.mergeResult(okResult("review", c.Rounds, json.RawMessage(rawJSON)))
	return c
}

// WithAdjudicate runs the adjudicator LLM Loop (ADR-0023 D4).
//
// Delegates to app.Adjudicate — the existing, tested adjudicator function.
// Passes the gate result and review verdict as disagreement context.
func (c *CognitionRun) WithAdjudicate(ctx context.Context) *CognitionRun {
	resolution, err := app.Adjudicate(ctx, c.Workspace, c.GateJSON, c.ReviewJSON, c.TaskContext, c.Model, c.MaxAPICalls/2, c.ModuleRef)
	if err != nil {
		c.mergeResult(errorResult("adjudicate", c.Rounds, err))
		return c
	}

	c.mergeResult(okResult("adjudicate", c.Rounds, json.RawMessage(resolution)))
	return c
}

// Run orchestrates the full pipeline with internal decision points (ADR-0027).
//
// This is the primary entry point for the controller. It chains all steps with
// gate-red revise loops and review-veto adjudication. Decision points are the
// only places where Go branching occurs — between them, steps compose lazily.
//
// Segment flow:
//
//	Segment A: WithPrompt(pre-code) → WithCode → WithGate
//	Decision 1: gate green? → proceed to Segment B
//	            gate red? → revise loop (append feedback, re-code, re-gate)
//	Segment B: WithPrompt(pre-review) → WithReview
//	Decision 2: review approve? → done
//	            review veto? → Segment C
//	Segment C: WithAdjudicate → done
//
// Returns the final pipeline result as JSON (see Result method).
func (c *CognitionRun) Run(ctx context.Context) (string, error) {
	// ── Segment A: Prompt → Code → Gate (with revise loop) ──────────────
	for round := 1; round <= c.MaxRevise; round++ {
		c.Rounds = round

		// Prompt: synthesize the coder prompt.
		c.WithPrompt(ctx, "pre-code")

		// On revise rounds, append gate feedback to the existing prompt.
		if round > 1 && c.GateJSON != "" {
			c.PromptText += "\n\n---\nPrevious attempt failed the gate:\n" + c.GateJSON
			c.PromptFile = dag.Directory().
				WithNewFile("prompt.md", c.PromptText).
				File("prompt.md")
		}

		c.pushStep(ctx, "prompt")

		// Code: run the coder LLM Loop.
		c.WithCode(ctx)
		c.pushStep(ctx, "code")

		// Gate: run the project's dagmar-gate Hook.
		c.WithGate(ctx)
		c.pushStep(ctx, "gate")

		if c.GatePassed {
			break
		}

		// Gate red: update source for next round's revise.
		if c.Workspace != nil {
			c.Source = c.Workspace
		}
	}

	if !c.GatePassed {
		return c.JSON(), nil
	}

	// ── Segment B: Prompt(review) → Review ──────────────────────────────
	c.WithPrompt(ctx, "pre-review")
	c.pushStep(ctx, "prompt")

	c.WithReview(ctx)
	c.pushStep(ctx, "review")

	// Decision: check the review verdict.
	if c.isReviewApproved() {
		return c.JSON(), nil
	}

	// ── Segment C: Adjudicate (on review veto) ──────────────────────────
	c.WithAdjudicate(ctx)
	c.pushStep(ctx, "adjudicate")

	return c.JSON(), nil
}

// JSON serializes the pipeline state as JSON (ADR-0027 D5).
// This is the terminal method — the controller parses this to decide policy.
func (c *CognitionRun) JSON() string {
	type pipelineResult struct {
		Outcome       string `json:"outcome"`
		GateResult    string `json:"gate_result,omitempty"`
		ReviewVerdict string `json:"review_verdict,omitempty"`
		Rounds        int    `json:"rounds"`
		GatePassed    bool   `json:"gate_passed"`
	}

	outcome := "approve"
	if !c.GatePassed {
		outcome = "max_retries"
	} else if c.ReviewJSON != "" && !c.isReviewApproved() {
		outcome = "adjudicated"
	}

	r := pipelineResult{
		Outcome:       outcome,
		GateResult:    c.GateJSON,
		ReviewVerdict: c.ReviewJSON,
		Rounds:        c.Rounds,
		GatePassed:    c.GatePassed,
	}
	b, _ := json.Marshal(r)
	return string(b)
}

// isReviewApproved parses the review verdict JSON for an approve decision.
func (c *CognitionRun) isReviewApproved() bool {
	if c.ReviewJSON == "" {
		return false
	}
	var verdict struct {
		Decision string `json:"decision"`
	}
	json.Unmarshal([]byte(c.ReviewJSON), &verdict)
	return verdict.Decision == "approve"
}

// pushStep sends the current pipeline state to the controller's Collector endpoint
// after each step completes. The payload includes the step name AND the full
// accumulated CognitionRun state — so the controller always sees the complete
// picture: current round, gate pass/fail, review verdict, etc.
//
// This gives the controller everything it needs for policy decisions:
//   - Round counter (how many revise loops so far)
//   - GatePassed (did the gate pass?)
//   - GateResult (which checks failed, coverage)
//   - ReviewVerdict (approve/veto + rationale)
//   - The step that just completed
func (c *CognitionRun) pushStep(ctx context.Context, step string) {
	if c.CallbackURL == "" {
		return
	}

	// Build the full state snapshot — everything the controller might need.
	state := struct {
		Step       string `json:"step"`
		Round      int    `json:"round"`
		GatePassed bool   `json:"gate_passed"`
		GateResult string `json:"gate_result,omitempty"`
		ReviewJSON string `json:"review_verdict,omitempty"`
		Outcome    string `json:"outcome,omitempty"`
	}{
		Step:       step,
		Round:      c.Rounds,
		GatePassed: c.GatePassed,
		GateResult: c.GateJSON,
		ReviewJSON: c.ReviewJSON,
	}

	// Compute outcome so the controller sees the current trajectory.
	if !c.GatePassed && step == "gate" {
		state.Outcome = "gate_red"
	} else if c.GatePassed && c.isReviewApproved() {
		state.Outcome = "approve"
	} else if c.ReviewJSON != "" && !c.isReviewApproved() {
		state.Outcome = "review_veto"
	}

	data, _ := json.Marshal(state)

	app.PushStepResult(ctx, c.CallbackURL, c.CallbackToken, app.StepResultPayload{
		Step:   step,
		Round:  c.Rounds,
		Result: data,
	})
}

// prompterBudget allocates API budget for the prompter (1/4 of total, min 5).
func (c *CognitionRun) prompterBudget() int {
	b := c.MaxAPICalls / 4
	if b < 5 {
		b = 5
	}
	return b
}

// reviewerBudget allocates API budget for the reviewer (1/2 of total, min 10).
func (c *CognitionRun) reviewerBudget() int {
	b := c.MaxAPICalls / 2
	if b < 10 {
		b = 10
	}
	return b
}
