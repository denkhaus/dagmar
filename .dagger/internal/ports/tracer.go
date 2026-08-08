// Package ports defines dagmar's observability port. The Tracer interface is the sole
// remaining Tier-B driven interface after ADR-0018: the Project Hook Services (issues,
// memory, prompts) are now native Dagger functions, not Go ports. Mocks are generated
// by mockgen (go.uber.org/mock) — see generate.go (ADR-0010 §6).
package ports

import "context"

// Tracer is the observability port (ADR-0010 §8). Default implementation = Dagger otel +
// TokenUsage (adapters/otel); Langfuse is a deferred opt-in adapter behind this port.
type Tracer interface {
	// StartSpan begins a named span; the returned context carries it for downstream calls.
	StartSpan(ctx context.Context, name string) (context.Context, Span)
}

// Span is a tracing span, ended by the caller.
type Span interface {
	End()
}
