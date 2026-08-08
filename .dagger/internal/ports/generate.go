package ports

// Mock generation for the Tracer port. KB guide.golang.testing: mocks MUST be
// generated with go.uber.org/mock/mockgen (NOT the deprecated google mockgen). Internal
// services are mocked in-package (KB placement). Regenerate after changing the port:
//
//	go generate ./internal/ports/...

//go:generate go run go.uber.org/mock/mockgen -source=tracer.go -destination=mock_tracer.go -package=ports
