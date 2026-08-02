package ports

// Mock generation for the Tier-B os-eco ports. KB guide.golang.testing: mocks MUST be
// generated with go.uber.org/mock/mockgen (NOT the deprecated google mockgen). Internal
// services are mocked in-package (KB placement). Regenerate after changing any port:
//
//	go generate ./internal/ports/...

//go:generate go run go.uber.org/mock/mockgen -source=oseco.go -destination=mock_oseco.go -package=ports
