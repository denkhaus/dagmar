// Package app contains dagmar's application services.
//
// output.go implements the JSON-output extraction + retry mechanism for LLM agents.
//
// All agent roles use WithJSONValueOutput to declare structured outputs. After Loop(),
// the output binding may or may not be set — the agent might not have called Save, or
// the JSON might not validate against the expected schema. This file provides:
//
//   - extractJSONOutput: reads a JSONValue output binding, with retry-on-failure.
//   - The retry re-prompts the agent (same LLM context) with a correction prompt,
//     then re-runs the Loop to give it another chance to set the output correctly.
package app

import (
	"context"
	"encoding/json"
	"fmt"

	"dagger/dagmar/internal/dagger"
)

// maxOutputRetries bounds how many times the agent is re-prompted to fix its output.
const maxOutputRetries = 2

// extractJSONOutput reads a named JSONValue output binding from the env after the Loop.
// If the output is missing, empty, or doesn't unmarshal into the target, it retries:
// the agent is re-prompted with a correction prompt and the Loop runs again.
//
// The target must be a pointer to a struct (json.Unmarshal semantics).
// Returns the raw JSON string (for logging/persistence) and the unmarshaled target.
func extractJSONOutput(
	ctx context.Context,
	llm *dagger.LLM,
	env *dagger.Env,
	outputName string,
	target any,
	correctionPrompt string,
) (string, error) {
	for attempt := 0; attempt <= maxOutputRetries; attempt++ {
		// Try to read the output binding.
		binding := env.Output(outputName)
		if binding == nil {
			return "", fmt.Errorf("output: binding %q not found on env", outputName)
		}

		rawJSON, err := binding.AsJSONValue().AsString(ctx)
		if err == nil && rawJSON != "" && rawJSON != "null" {
			// Try to unmarshal into the target.
			if err := json.Unmarshal([]byte(rawJSON), target); err == nil {
				return rawJSON, nil // success
			}
			// JSON present but doesn't fit schema — retry with schema hint.
		}

		// Output missing or invalid — retry if budget remains.
		if attempt >= maxOutputRetries {
			if rawJSON == "" || rawJSON == "null" {
				return "", fmt.Errorf("output: agent did not set %q after %d retries", outputName, maxOutputRetries)
			}
			return rawJSON, fmt.Errorf("output: %q did not validate against schema after %d retries (last value: %.200s)", outputName, maxOutputRetries, rawJSON)
		}

		// Re-prompt the agent with a correction prompt and re-run the Loop.
		llm = llm.WithPrompt(fmt.Sprintf(correctionPrompt, rawJSON))
		llm = llm.Loop()
		if _, err := llm.Sync(ctx); err != nil {
			return "", fmt.Errorf("output: retry loop failed: %w", err)
		}
	}

	return "", fmt.Errorf("output: exhausted retries for %q", outputName)
}
