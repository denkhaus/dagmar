package config

import (
	"strings"
	"testing"
)

func TestParseManifest(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string // substring of the error; "" means no error expected
	}{
		{
			name: "valid two checkables",
			yaml: "checkables:\n" +
				"  - name: controller\n    workdir: .\n    command: go build ./...\n" +
				"  - name: dagger-module\n    workdir: .dagger\n    command: go test ./...\n",
			wantErr: "",
		},
		{
			name:    "no checkables",
			yaml:    "checkables: []\n",
			wantErr: "no checkables",
		},
		{
			name:    "missing name",
			yaml:    "checkables:\n  - workdir: .\n    command: go build ./...\n",
			wantErr: "must have both name and command",
		},
		{
			name:    "missing command",
			yaml:    "checkables:\n  - name: x\n    workdir: .\n",
			wantErr: "must have both name and command",
		},
		{
			name:    "workdir escapes via ..",
			yaml:    "checkables:\n  - name: x\n    workdir: ../etc\n    command: true\n",
			wantErr: `".." components not allowed`,
		},
		{
			name:    "workdir absolute",
			yaml:    "checkables:\n  - name: x\n    workdir: /etc\n    command: true\n",
			wantErr: "absolute paths not allowed",
		},
		{
			name:    "workdir empty",
			yaml:    "checkables:\n  - name: x\n    workdir: \"\"\n    command: true\n",
			wantErr: "empty",
		},
		{
			name:    "malformed yaml",
			yaml:    "checkables: [this is not : valid : yaml\n",
			wantErr: "parse .dagmar/project.yaml",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := ParseManifest([]byte(tt.yaml))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ParseManifest: unexpected error: %v", err)
				}
				if len(m.Checkables) == 0 {
					t.Fatalf("expected checkables, got none")
				}
			} else {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want substring %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}
