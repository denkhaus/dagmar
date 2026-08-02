package domain

import "testing"

func TestSandboxSpec_Validate(t *testing.T) {
	tests := []struct {
		name    string
		spec    SandboxSpec
		wantErr bool
	}{
		{"valid", SandboxSpec{Image: "alpine:3.20"}, false},
		{"valid with workdir", SandboxSpec{Image: "golang:1.26", Workdir: "/src"}, false},
		{"missing image", SandboxSpec{Image: ""}, true},
		{"missing image with workdir", SandboxSpec{Image: "", Workdir: "/src"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.spec.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}
