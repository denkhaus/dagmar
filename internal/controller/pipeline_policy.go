// Package controller contains dagmar's Kubernetes reconciliation logic.
//
// pipeline_policy.go implements the controller-side policy layer for CognitionRun
// pipeline Runs (ADR-0027). The pipeline orchestrates prompt→code→gate→review→
// adjudicate internally and pushes step results to the controller's Collector
// HTTP endpoint after each step. The controller's only job is:
//
//  1. Dispatch the CognitionRun pod (done in run_controller.go).
//  2. Observe the pushed StepResults for visibility (done in collector.go).
//  3. When the pod completes, evaluate the final pipeline outcome and set the
//     Run's terminal status (done here).
//
// No step-level state machine is needed — the pipeline's internal decision
// points (gate-red revise loops, review-veto adjudication) are opaque to the
// controller. The controller sees only: did the pipeline ultimately approve
// (outcome=approve) or fail (outcome=max_retries / error)?
package controller

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/denkhaus/dagmar/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Pipeline policy phases (ADR-0027). These replace the former step-level FSM
// states (coding/gating/reviewing/adjudicating). The pipeline runs its own
// internal state machine; the controller tracks only policy-level progress.
const (
	PipelineDispatching = "dispatching"
	PipelineRunning     = "running"
	PipelineDone        = "done"
	PipelineEscalated   = "escalated"
)

// pipelineResult is the JSON shape pushed by the CognitionRun pipeline's final
// step (cognition_run.go JSON()). The controller parses this from the last
// StepResult to make its terminal policy decision.
type pipelineResult struct {
	Outcome       string `json:"outcome"`
	GateResult    string `json:"gate_result,omitempty"`
	ReviewVerdict string `json:"review_verdict,omitempty"`
	Rounds        int    `json:"rounds"`
	GatePassed    bool   `json:"gate_passed"`
}

// evaluatePipelineOutcome reads the last StepResult pushed by the CognitionRun
// pipeline and determines whether the Run should be marked done or escalated.
//
// The pipeline pushes after every step (prompt, code, gate, review, adjudicate).
// The last push carries the final outcome:
//   - "approve" → pipeline succeeded (gate green + review approved).
//   - "max_retries" → gate never passed within the revise budget.
//   - "adjudicated" → gate green but review vetoed, adjudicator resolved.
//   - "gate_red" → interim push (more steps may follow); not terminal.
//
// Returns (outcome string, isTerminal bool). Non-terminal outcomes (e.g. an
// interim "gate_red" push while the pod is still running) return isTerminal=false.
func evaluatePipelineOutcome(stepResults []v1alpha1.StepResult) (outcome string, isTerminal bool) {
	if len(stepResults) == 0 {
		return "", false
	}

	// Scan from the end for the most recent result that carries an outcome.
	for i := len(stepResults) - 1; i >= 0; i-- {
		sr := stepResults[i]
		if sr.Result == "" {
			continue
		}
		var pr pipelineResult
		if err := json.Unmarshal([]byte(sr.Result), &pr); err != nil {
			continue
		}
		if pr.Outcome == "" {
			continue
		}
		switch pr.Outcome {
		case "approve":
			return "approve", true
		case "max_retries":
			return "max_retries", true
		case "adjudicated":
			return "adjudicated", true
		case "gate_red", "review_veto":
			return pr.Outcome, false // interim — pod still running
		}
	}
	return "", false
}

// coverageFloorFor resolves the coverage floor (basis points) to annotate on the
// cognition-run pod. Returns 0 (disabled) when CoveragePolicy is not set or not
// enabled. When enabled, returns the current ratcheted floor (Status.CoverageFloor),
// or MinimumFloor if the floor has not yet been initialized (dagmar-4154).
func coverageFloorFor(project *v1alpha1.Project) int {
	if project.Spec.CoveragePolicy == nil || !project.Spec.CoveragePolicy.Enabled {
		return 0
	}
	if project.Status.CoverageFloor > 0 {
		return project.Status.CoverageFloor
	}
	return project.Spec.CoveragePolicy.MinimumFloor
}

// extractCoverageBps parses coverage_bps from the GateResult JSON that the
// pipeline pushes as part of its step results. The gate result is embedded in
// the pipelineResult.GateResult field (raw GateResult JSON).
func extractCoverageBps(stepResults []v1alpha1.StepResult) int {
	for i := len(stepResults) - 1; i >= 0; i-- {
		sr := stepResults[i]
		if sr.Result == "" {
			continue
		}
		var pr pipelineResult
		if err := json.Unmarshal([]byte(sr.Result), &pr); err != nil {
			continue
		}
		if pr.GateResult == "" {
			continue
		}
		var gr struct {
			CoverageBps int `json:"coverage_bps"`
		}
		if json.Unmarshal([]byte(pr.GateResult), &gr) == nil && gr.CoverageBps > 0 {
			return gr.CoverageBps
		}
	}
	return 0
}

// ratchetCoverage implements the coverage-floor ratcheting logic (dagmar-4154).
// The measured coverage now comes from the pipeline's pushed StepResults
// (extractCoverageBps) rather than the former termination-log path.
func (r *RunReconciler) ratchetCoverage(ctx context.Context, project *v1alpha1.Project, measuredCoverageBps int) error {
	if project.Spec.CoveragePolicy == nil || !project.Spec.CoveragePolicy.Enabled {
		return nil
	}
	if project.Status.CoverageFloor == 0 && measuredCoverageBps == 0 {
		return r.patchProjectCoverageFloor(ctx, project, project.Spec.CoveragePolicy.MinimumFloor)
	}
	if measuredCoverageBps > 0 {
		margin := project.Spec.CoveragePolicy.RatchetMargin
		if margin == 0 {
			margin = 200
		}
		newFloor := measuredCoverageBps - margin
		minFloor := project.Spec.CoveragePolicy.MinimumFloor
		if newFloor < minFloor {
			newFloor = minFloor
		}
		if newFloor > project.Status.CoverageFloor {
			return r.patchProjectCoverageFloor(ctx, project, newFloor)
		}
	}
	return nil
}

// patchProjectCoverageFloor patches the Project's Status.CoverageFloor via the
// status subresource (dagmar-4154). Used for ratcheting: the controller updates
// the floor after a successful gate.
func (r *RunReconciler) patchProjectCoverageFloor(ctx context.Context, project *v1alpha1.Project, floorBps int) error {
	fresh := &v1alpha1.Project{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(project), fresh); err != nil {
		return fmt.Errorf("get project for coverage-floor patch: %w", err)
	}
	base := fresh.DeepCopy()
	fresh.Status.CoverageFloor = floorBps
	if err := r.Status().Patch(ctx, fresh, client.MergeFrom(base)); err != nil {
		return fmt.Errorf("patch project coverage-floor: %w", err)
	}
	return nil
}

// getAgent fetches an Agent CR by name in the given namespace.
func (r *RunReconciler) getAgent(ctx context.Context, name, namespace string) (*v1alpha1.Agent, error) {
	agent := &v1alpha1.Agent{}
	if err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, agent); err != nil {
		return nil, fmt.Errorf("get Agent %q: %w", name, err)
	}
	return agent, nil
}
