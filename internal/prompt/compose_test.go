package prompt

import (
	"strings"
	"testing"
)

func TestCompose_EmptyInput(t *testing.T) {
	result := Compose(nil, nil)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestCompose_OnlyProject(t *testing.T) {
	project := []Section{
		{Name: "role", Body: "You are a coder."},
		{Name: "task", Body: "Fix the bug."},
	}
	result := Compose(project, nil)
	if !strings.Contains(result, "## role") {
		t.Errorf("missing role section")
	}
	if !strings.Contains(result, "You are a coder.") {
		t.Errorf("missing role body")
	}
}

func TestCompose_OnlyDagmar(t *testing.T) {
	dagmar := []Section{
		{Name: "safety", Body: "Never merge without review."},
	}
	result := Compose(nil, dagmar)
	if !strings.Contains(result, "Never merge without review.") {
		t.Errorf("missing safety body")
	}
}

func TestCompose_MergeAndOverride(t *testing.T) {
	dagmar := []Section{
		{Name: "output", Body: "Use markdown."},
		{Name: "safety", Body: "Be careful."},
	}
	project := []Section{
		{Name: "output", Body: "Use JSON."},
		{Name: "task", Body: "Refactor module X."},
	}
	result := Compose(project, dagmar)

	// "output" should be overridden by project (last wins)
	if strings.Contains(result, "Use markdown.") {
		t.Error("dagmar output section should be overridden by project")
	}
	if !strings.Contains(result, "Use JSON.") {
		t.Error("project output section should win")
	}
	// "safety" should survive (only in dagmar)
	if !strings.Contains(result, "Be careful.") {
		t.Error("dagmar-only safety section should be present")
	}
	// "task" should be present (only in project)
	if !strings.Contains(result, "Refactor module X.") {
		t.Error("project-only task section should be present")
	}
}

func TestShellComposeCommand(t *testing.T) {
	cmd := ShellComposeCommand("coder-prompt", nil, "/workspace", "/tmp/prompt.md")
	if !strings.Contains(cmd, "cn render coder-prompt") {
		t.Errorf("expected cn render in command, got: %s", cmd)
	}
	if !strings.Contains(cmd, "/tmp/prompt.md") {
		t.Errorf("expected output path in command")
	}
}

func TestShellComposeCommand_WithMixins(t *testing.T) {
	cmd := ShellComposeCommand("coder-prompt", []string{"safety", "output-format"}, "/workspace", "/tmp/prompt.md")
	if !strings.Contains(cmd, "cn render coder-prompt") {
		t.Errorf("expected project prompt render, got: %s", cmd)
	}
	if !strings.Contains(cmd, "cn render safety") {
		t.Errorf("expected mixin safety render, got: %s", cmd)
	}
	if !strings.Contains(cmd, "cn render output-format") {
		t.Errorf("expected mixin output-format render, got: %s", cmd)
	}
	// Mixins should appear before the project prompt (lower priority in the merge).
	safetyIdx := strings.Index(cmd, "cn render safety")
	projectIdx := strings.Index(cmd, "cn render coder-prompt")
	if safetyIdx == -1 || projectIdx == -1 || safetyIdx > projectIdx {
		t.Errorf("mixins should be rendered before project prompt")
	}
}
