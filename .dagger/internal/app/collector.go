package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// StepResultPayload is the JSON body pushed to the controller's Collector endpoint.
// It matches the stepResultPayload in internal/controller/collector.go.
type StepResultPayload struct {
	Step   string          `json:"step"`
	Round  int             `json:"round"`
	Result json.RawMessage `json:"result"`
}

// PushStepResult sends a step result to the controller's Collector HTTP endpoint
// (ADR-0027 D3). Fire-and-forget: errors are silently ignored — the pipeline runs
// fine without the controller receiving pushes.
func PushStepResult(ctx context.Context, callbackURL, callbackToken string, payload StepResultPayload) {
	if callbackURL == "" {
		return
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
