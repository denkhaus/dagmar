# Build the dagmar-controller manager binary (Phase 0, ADR-0012). Multi-stage.
FROM golang:1.26 AS builder
WORKDIR /workspace
# Cache deps first.
COPY go.mod go.sum ./
RUN go mod download
# Source (root module only; .dagger/ is a separate module, untouched).
COPY api/ api/
COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 GOOS=linux go build -o manager ./cmd/dagmar-controller

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=builder /workspace/manager /manager
USER nonroot:nonroot
ENTRYPOINT ["/manager"]
