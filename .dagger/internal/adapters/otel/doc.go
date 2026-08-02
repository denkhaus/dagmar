// Package otel will hold the default observability adapter: a ports.Tracer implemented
// over Dagger's built-in OpenTelemetry (go.opentelemetry.io/otel, already a dependency)
// and TokenUsage (ADR-0010 §8). Langfuse is a deferred opt-in adapter behind the same
// Tracer port.
package otel
