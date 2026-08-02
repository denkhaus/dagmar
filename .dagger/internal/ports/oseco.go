// Package ports defines dagmar's Tier-B driven interfaces: the os-eco backing services
// (seeds / mulch / canopy) consumed behind adapter ports, bound per-Project (ADR-0001,
// ADR-0005). os-eco tool names appear ONLY in adapter implementations, never in domain.
// Tier-A Dagger primitives are deliberately NOT ported (ADR-0010 §3). Mocks are generated
// by mockgen (go.uber.org/mock) — see generate.go (ADR-0010 §6).
package ports

import "context"

// IssueTracker is the adapter port for seeds — the canonical work handle. CRUD on Tasks
// (issues) and dependency/plan management (CONTEXT.md: Task ≡ 1 seeds issue).
type IssueTracker interface {
	// CreateIssue creates a Task (= one seeds issue) and returns its canonical handle.
	CreateIssue(ctx context.Context, title, body string) (string, error)
}

// Memory is the adapter port for mulch — read/write per-Project expertise (conventions,
// patterns, failures, decisions).
type Memory interface {
	// Read recalls expertise for a query in the bound Project's mulch store.
	Read(ctx context.Context, query string) (string, error)
}

// Prompts is the adapter port for canopy — cross-store prompt composition (ADR-0005):
// dagmar operational mixins ⊕ project-content prompts, emitted as the resolved prompt.
type Prompts interface {
	// Compose resolves an Agent's prompt for the bound Project (ADR-0005 Variant A).
	Compose(ctx context.Context, agent string) (string, error)
}

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
