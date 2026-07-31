# dagmar

A (target: fully-autonomous) Dagger/Kubernetes-hybrid multi-agent coding system, written
in Go, that works on the owner's own repositories and their forks. dagmar reacts to
repository events and runs proactive cron-driven housekeeping, bounded by an evolving
quality gate and multi-agent review.

## Status

Early / documentation-only. The domain model and foundational architectural decisions
are pinned; there is no application code yet. See the wayfinder map (seeds
`dagmar-80dd`).

## Read first

- [`CONTEXT.md`](CONTEXT.md) — domain model & ubiquitous language (the vocabulary every
  issue, design, and code symbol uses).
- [`docs/adr/`](docs/adr/) — architectural decision records.
- [`CLAUDE.md`](CLAUDE.md) — agent/CLI instructions (mulch expertise, seeds tracker).

## Run

Not yet available — there is no application entrypoint. Added as the self-bootstrap
trajectory (seeds `dagmar-e795`) progresses.
