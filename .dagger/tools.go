//go:build tools

// Package tools tracks development-tool dependencies so `go mod` records them reproducibly
// (KB guide.golang.testing). The build constraint keeps these out of the runtime binary;
// the blank imports keep them pinned in go.mod so `go generate` resolves them from the
// module cache, not live network on every run.
package tools

import (
	_ "go.uber.org/mock/mockgen" // generates the os-eco port mocks (ports/generate.go)
)
