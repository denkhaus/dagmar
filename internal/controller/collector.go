// Package controller contains dagmar's Kubernetes reconciliation logic.
//
// collector.go implements the Step-Result HTTP endpoint (ADR-0027 D3). The controller
// runs a small HTTP server as a manager Runnable. The CognitionRun pipeline's Collector
// pushes step results here after each pipeline step. The results are stored in
// Run.Status.StepResults for policy decisions and k9s observability.
//
// Auth: per-run Bearer token. The controller generates a token at dispatch time and
// injects it as a pod env var. The pipeline sends it in the Authorization header.
package controller

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/denkhaus/dagmar/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// CollectorServer is the HTTP server that receives step-result pushes from the
// CognitionRun pipeline's Collector (ADR-0027 D3). It runs as a controller-runtime
// Runnable alongside the reconciler.
type CollectorServer struct {
	Client client.Client
	// tokens maps run names to their Bearer tokens (generated at dispatch time).
	tokens sync.Map // map[string]string (runName → token)
}

// GenerateToken creates a random Bearer token for a Run and stores it for later
// verification. Called by the reconciler when dispatching a pipeline Run.
func (s *CollectorServer) GenerateToken(runName string) string {
	b := make([]byte, 32)
	rand.Read(b)
	token := hex.EncodeToString(b)
	s.tokens.Store(runName, token)
	return token
}

// Start implements manager.Runnable (controller-runtime lifecycle).
func (s *CollectorServer) Start(ctx context.Context) error {
	logger := log.FromContext(ctx)
	logger.Info("starting collector HTTP server", "port", collectorPort)

	mux := http.NewServeMux()
	mux.HandleFunc("/step-result", s.handleStepResult)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", collectorPort),
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("collector server: %w", err)
	}
	return nil
}

// handleStepResult processes a POST /step-result from the pipeline Collector.
func (s *CollectorServer) handleStepResult(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse the payload.
	var payload stepResultPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Authenticate via Bearer token.
	authHeader := r.Header.Get("Authorization")
	token := ""
	if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
		token = authHeader[7:]
	}

	// Find the run name that matches this token.
	runName := s.findRunByToken(token)
	if runName == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Store the step result in the Run's status.
	if err := s.appendStepResult(r.Context(), runName, payload); err != nil {
		http.Error(w, "store result: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status":"ok"}`)
}

// findRunByToken looks up the run name associated with a Bearer token.
func (s *CollectorServer) findRunByToken(token string) string {
	var found string
	s.tokens.Range(func(key, value any) bool {
		if value.(string) == token {
			found = key.(string)
			return false
		}
		return true
	})
	return found
}

// appendStepResult appends a step result to the Run's Status.StepResults.
func (s *CollectorServer) appendStepResult(ctx context.Context, runName string, payload stepResultPayload) error {
	run := &v1alpha1.Run{}
	if err := s.Client.Get(ctx, client.ObjectKey{Name: runName, Namespace: "default"}, run); err != nil {
		return fmt.Errorf("get run %q: %w", runName, err)
	}

	base := run.DeepCopy()
	run.Status.StepResults = append(run.Status.StepResults, v1alpha1.StepResult{
		Step:   payload.Step,
		Round:  payload.Round,
		Result: string(payload.Result),
	})

	return s.Client.Status().Patch(ctx, run, client.MergeFrom(base))
}

// stepResultPayload is the JSON body pushed by the pipeline Collector.
type stepResultPayload struct {
	RunName string          `json:"run_name,omitempty"`
	Step    string          `json:"step"`
	Round   int             `json:"round"`
	Result  json.RawMessage `json:"result"`
}

// collectorPort is the HTTP port for the step-result endpoint.
const collectorPort = 8082

// Ensure CollectorServer implements manager.Runnable.
var _ = (*CollectorServer)(nil)
