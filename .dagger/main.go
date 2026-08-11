// dagmar — autonomous Dagger/Kubernetes multi-agent system.
//
// This is dagmar's Dagger module: the execution engine that runs the coder loop,
// prompter loop, adjudicator loop, gate, and sandbox. The Kubernetes controller
// dispatches these functions via `dagger call` from agent pods.

package main

import (
	"context"

	"dagger/dagmar/internal/app"
	"dagger/dagmar/internal/dagger"
	"dagger/dagmar/internal/domain"
)

// entry point into dagmar's Dagger functionality AND the per-Project binding seam: the New
// constructor binds the target Project once, and every method (Run, Sandbox, Gate, ...)
// reuses that bound state (ADR-0010 §5). Project Hook Services (issues, memory, prompts)
// are exposed as native Dagger functions via WithMainModule(), not Go ports (ADR-0018).
type Dagmar struct {
	// Project is the source directory of the target Project dagmar operates on
	// (per-Project binding). *dagger.Directory, not *dagger.Workspace: Workspace-as-input
	// is unsupported by engine v0.21.8's codegen ("cannot code-generate ... unsupported
	// types"), and a Directory is the version-independent representation of a project
	// source tree (the SDK itself maps Workspace -> Directory; ADR-0010 §5).
	// +private
	Project *dagger.Directory
}

// New is dagmar's constructor (ADR-0010 §5). Its argument binds the per-Project context
// (the Project source) that every method reuses. The argument is optional so the
// infra/spike methods (Up, DeployEngine, Probe) remain callable without a Project
// binding — they ignore the bound state.
//
// Project Hook Services (issues, memory, prompts) are NOT bound here — they are native
// Dagger functions registered via WithMainModule() (ADR-0017, ADR-0018).
func New(
	// The target Project's source directory (per-Project binding seam).
	// +optional
	project *dagger.Directory,
) *Dagmar {
	return &Dagmar{Project: project}
}

// Sandbox realizes an isolated, credentialed execution slot (a Dagger Container — Tier A,
// used directly; ADR-0010 §3). This is the v0 vertical proving the layout seams (functional
// core -> app Tier-A-direct -> main delegation -> a chainable custom return object) without
// an LLM call. Delegates to app.BuildSandbox.
//
// NOTE: the args are primitives (not a domain.SandboxSpec) because Dagger cannot code-generate
// for a foreign (non-main-package) input type. The pure domain.SandboxSpec is constructed at
// this seam from the primitives; domain stays Dagger-free and unit-tested (ADR-0010 §3).
func (m *Dagmar) Sandbox(
	// Base OCI image for the Sandbox container.
	image string,
	// Working directory inside the Sandbox (empty = image default). Named workingDir, not
	// workdir, to avoid a CLI flag collision with *dagger.Container's own workdir field.
	// +optional
	workingDir string,
) (*Sandbox, error) {
	ctr, err := app.BuildSandbox(domain.SandboxSpec{Image: image, Workdir: workingDir})
	if err != nil {
		return nil, err
	}
	return &Sandbox{ctr: ctr}, nil
}

// Code is dagmar's coder-loop entry point (Phase 2 cognition, ADR-0021 D1). It constructs
// the Env, drives the LLM Loop, and returns the modified workspace Directory. The controller
// dispatches this via `dagger call -m .dagger code --source <dir> --prompt-file <md>`.
// Delegates to app.Code.
//
// The args are primitives + Dagger types (Directory, File) because Dagger codegen requires
// main-package types only. The app layer builds the Env + LLM + Loop from these (ADR-0010 §3:
// Tier A direct). The prompt file is pre-composed by the controller (ADR-0005 merge).
func (m *Dagmar) Code(
	ctx context.Context,
	// source is the workspace Directory — the project source the agent works on
	// (clone from ADR-0020 D1: dag.Git(repoURL).Branch(branchName).Tree()).
	source *dagger.Directory,
	// promptFile is the resolved prompt .md (ADR-0005 cross-store merge, pre-computed
	// by the controller). The agent receives this via WithPromptFile.
	promptFile *dagger.File,
	// model is the LLM model identifier (e.g. "anthropic/claude-sonnet-4").
	// +optional
	// +default="anthropic/claude-sonnet-4"
	model string,
	// maxAPICalls bounds the LLM API calls for this Run (token/cost cap, ADR-0021 D4).
	// Engine-enforced hard stop: when exhausted, the Loop terminates.
	// +optional
	// +default=100
	maxAPICalls int,
	// moduleRef is the project module reference (the Project CR's moduleRef).
	// Defaults to ".dagmar" (dagmar dogfooding itself).
	// +optional
	// +default=".dagmar"
	moduleRef string,
) (*dagger.Directory, error) {
	src := source
	if src == nil {
		src = m.Project
	}
	return app.Code(ctx, src, promptFile, model, maxAPICalls, moduleRef)
}

// Prompt is dagmar's prompter-loop entry point (ADR-0023 D1). It synthesizes a tailored
// prompt for the Coder or Reviewer by running a short LLM loop that reads project source,
// issues, and memory. The synthesized prompt is returned as a string — the controller
// forwards it as --prompt-file to the subsequent Code or Review Run.
//
// The args are primitives + Dagger types because Dagger codegen requires main-package types
// only. The app layer builds the read-only Env, selects the meta-prompt by phase, and drives
// the Loop (ADR-0010 §3: Tier A direct). Delegates to app.Prompt.
func (m *Dagmar) Prompt(
	ctx context.Context,
	// source is the project source directory (read-only). The prompter reads files
	// from here to ground the synthesized prompt in real project context.
	source *dagger.Directory,
	// phase selects which meta-prompt to use: "pre-code" (coder) or "pre-review" (reviewer).
	phase string,
	// taskContext is the issue text / task description from the orchestrating Run.
	taskContext string,
	// model is the LLM model identifier (e.g. "anthropic/claude-sonnet-4").
	// The prompter may use a smaller/faster model — synthesis is well-bounded (ADR-0023 D6).
	// +optional
	// +default="anthropic/claude-sonnet-4"
	model string,
	// maxAPICalls bounds the LLM API calls for this synthesis Run. Low budget —
	// prompt synthesis is well-bounded (ADR-0023 D1).
	// +optional
	// +default=10
	maxAPICalls int,
	// moduleRef is the project module reference (the Project CR's moduleRef).
	// Registers dagmar-issues + dagmar-memory as LLM-Tool hooks via WithMainModule.
	// Defaults to ".dagmar" (dagmar dogfooding itself).
	// +optional
	// +default=".dagmar"
	moduleRef string,
) (string, error) {
	src := source
	if src == nil {
		src = m.Project
	}
	return app.Prompt(ctx, src, phase, taskContext, model, maxAPICalls, moduleRef)
}

// Adjudicate is dagmar's adjudicator-loop entry point (ADR-0023 D4). When the deterministic
// Gate and the Reviewer-LLM disagree (gate green + reviewer veto, or gate red + reviewer
// approve), the Adjudicator resolves the conflict — it is the final automated decision maker
// before human escalation.
//
// The Adjudicator is read-only: it reads source, issues, and memory to investigate the
// disagreement, but modifies nothing. It returns a structured verdict string naming one of
// three resolution paths: reviewer-wrong (calibrate reviewer, proceed), gate-wrong (coder
// repairs the gate checkables, full re-run), or escalate (unresolvable, human needed).
//
// Unlike Prompt/Code, the Adjudicator does NOT use a chained prompter — its instructions come
// directly from the adjudicator meta-prompt (prompts.AdjudicatorMetaPrompt). The controller
// dispatches this via `dagger call -m .dagger adjudicate --source <dir> ...`. Delegates to
// app.Adjudicate.
//
// The args are primitives + Dagger types because Dagger codegen requires main-package types
// only. The app layer builds the read-only Env, sends the meta-prompt + disagreement context,
// and drives the Loop (ADR-0010 §3: Tier A direct).
func (m *Dagmar) Adjudicate(
	ctx context.Context,
	// source is the project source directory (read-only). The Adjudicator reads files
	// from here to investigate the root cause of the disagreement.
	source *dagger.Directory,
	// gateResult is the gate's outcome: "green" or "red" plus which checkables failed
	// (if red) and their failure messages.
	gateResult string,
	// reviewResult is the reviewer's outcome: "approve" or "veto" plus the reviewer's
	// rationale.
	reviewResult string,
	// taskContext is the original issue text / task description the coder was asked
	// to implement.
	taskContext string,
	// model is the LLM model identifier (e.g. "anthropic/claude-sonnet-4").
	// The Adjudicator needs strong reasoning (ADR-0023 D6).
	// +optional
	// +default="anthropic/claude-sonnet-4"
	model string,
	// maxAPICalls bounds the LLM API calls for this adjudication Run. Higher than the
	// prompter (adjudication may require deeper investigation of source + issues).
	// +optional
	// +default=30
	maxAPICalls int,
	// moduleRef is the project module reference (the Project CR's moduleRef).
	// Registers dagmar-issues + dagmar-memory as LLM-Tool hooks via WithMainModule.
	// Defaults to ".dagmar" (dagmar dogfooding itself).
	// +optional
	// +default=".dagmar"
	moduleRef string,
) (string, error) {
	src := source
	if src == nil {
		src = m.Project
	}
	return app.Adjudicate(ctx, src, gateResult, reviewResult, taskContext, model, maxAPICalls, moduleRef)
}

// Review is dagmar's reviewer-loop entry point (ADR-0024 D4). The reviewer reads the
// coder's workspace, applies review criteria from the prompt, and returns a structured
// JSON verdict (approve/veto + rationale). Unlike Code(), Review is read-only (no
// Writable, no DirectoryOutput) and returns a JSON string, not a Directory.
//
// The controller dispatches this via `dagger call -m .dagger review --source <dir> ...`.
// The output is a JSON string the controller parses for the approve/veto decision
// (ADR-0025: structured JSON output via WithJSONValueOutput, not termination-log).
func (m *Dagmar) Review(
	ctx context.Context,
	// source is the workspace Directory — the project source the reviewer reads
	// (read-only: the reviewer does NOT modify code).
	source *dagger.Directory,
	// promptFile is the pre-synthesized reviewer prompt (from the chained prompter
	// with phase "pre-review"). Contains the review criteria + task context.
	promptFile *dagger.File,
	// model is the LLM model identifier.
	// +optional
	// +default="anthropic/claude-sonnet-4"
	model string,
	// maxAPICalls bounds the LLM API calls for this review Run.
	// +optional
	// +default=50
	maxAPICalls int,
	// moduleRef is the project module reference (registers dagmar-issues + dagmar-memory).
	// +optional
	// +default=".dagmar"
	moduleRef string,
) (string, error) {
	verdict, rawJSON, err := app.Review(ctx, source, promptFile, model, maxAPICalls, moduleRef)
	if err != nil {
		return rawJSON, err
	}
	_ = verdict // parsed struct available if needed by the caller
	return rawJSON, nil
}

// Diff computes the difference between a pre-Loop and post-Loop workspace (ADR-0021 D8).
// The controller calls this after Code() to extract the agent's changes for the PR flow
// (ADR-0020 D3). Returns a Directory containing only the changed files.
func (m *Dagmar) Diff(
	ctx context.Context,
	// after is the post-Loop workspace (Code's return value).
	after *dagger.Directory,
	// before is the pre-Loop workspace (the original clone).
	before *dagger.Directory,
) *dagger.Directory {
	return app.Diff(after, before)
}

// Sandbox is the Dagger object returned by Dagmar.Sandbox — a thin, chainable wrapper over
// the realized Container. Exported methods on it become callable Dagger functions.
type Sandbox struct {
	// +private
	ctr *dagger.Container
}

// Container returns the underlying Dagger Container (Tier A).
func (s *Sandbox) Container() *dagger.Container {
	return s.ctr
}
