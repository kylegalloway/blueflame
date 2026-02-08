package agent

import (
	"strings"
	"testing"

	"github.com/kylegalloway/blueflame/internal/tasks"
)

func TestDefaultPromptRendererWorker(t *testing.T) {
	r := &DefaultPromptRenderer{}

	task := &tasks.Task{
		ID:          "task-001",
		Title:       "Add auth",
		Description: "Add authentication middleware",
		FileLocks:   []string{"pkg/auth/", "internal/middleware/"},
	}

	prompt, err := r.RenderPrompt(RoleWorker, WorkerPromptData{
		Task:      task,
		FileLocks: task.FileLocks,
	})
	if err != nil {
		t.Fatalf("RenderPrompt: %v", err)
	}

	if !strings.Contains(prompt, "task-001") {
		t.Error("prompt should contain task ID")
	}
	if !strings.Contains(prompt, "Add auth") {
		t.Error("prompt should contain task title")
	}
	if !strings.Contains(prompt, "pkg/auth/") {
		t.Error("prompt should contain file locks")
	}
}

func TestDefaultPromptRendererWorkerWithRetryNotes(t *testing.T) {
	r := &DefaultPromptRenderer{}

	task := &tasks.Task{
		ID:    "task-001",
		Title: "Add auth",
	}

	prompt, err := r.RenderPrompt(RoleWorker, WorkerPromptData{
		Task:       task,
		RetryNotes: "previous attempt had compile errors",
	})
	if err != nil {
		t.Fatalf("RenderPrompt: %v", err)
	}

	if !strings.Contains(prompt, "previous attempt") {
		t.Error("prompt should contain retry notes")
	}
}

func TestDefaultPromptRendererPlanner(t *testing.T) {
	r := &DefaultPromptRenderer{}

	prompt, err := r.RenderPrompt(RolePlanner, PlannerPromptData{
		Description:  "Add user authentication",
		PriorContext: "task-005 failed due to missing deps",
		ProjectName:  "myproject",
	})
	if err != nil {
		t.Fatalf("RenderPrompt: %v", err)
	}

	if !strings.Contains(prompt, "Add user authentication") {
		t.Error("prompt should contain description")
	}
	if !strings.Contains(prompt, "task-005 failed") {
		t.Error("prompt should contain prior context")
	}
}

func TestDefaultPromptRendererValidator(t *testing.T) {
	r := &DefaultPromptRenderer{}

	task := &tasks.Task{ID: "task-001", Title: "Add auth"}

	prompt, err := r.RenderPrompt(RoleValidator, ValidatorPromptData{
		Task: task,
		Diff: "+func Auth() {}",
	})
	if err != nil {
		t.Fatalf("RenderPrompt: %v", err)
	}

	if !strings.Contains(prompt, "task-001") {
		t.Error("prompt should contain task ID")
	}
	if !strings.Contains(prompt, "+func Auth") {
		t.Error("prompt should contain diff")
	}
}

func TestDefaultPromptRendererMerger(t *testing.T) {
	r := &DefaultPromptRenderer{}

	prompt, err := r.RenderPrompt(RoleMerger, MergerPromptData{
		Branches: []BranchInfo{
			{Name: "blueflame/task-001", TaskID: "task-001", TaskTitle: "Add auth"},
			{Name: "blueflame/task-002", TaskID: "task-002", TaskTitle: "Add tests"},
		},
		BaseBranch: "main",
	})
	if err != nil {
		t.Fatalf("RenderPrompt: %v", err)
	}

	if !strings.Contains(prompt, "blueflame/task-001") {
		t.Error("prompt should contain branch name")
	}
	if !strings.Contains(prompt, "Add tests") {
		t.Error("prompt should contain task title")
	}
	if !strings.Contains(prompt, "into main") {
		t.Error("prompt should specify base branch")
	}
	if !strings.Contains(prompt, "git checkout main") {
		t.Error("prompt should include checkout step")
	}
	if !strings.Contains(prompt, "git merge blueflame/task-001") {
		t.Error("prompt should include merge step")
	}
}

func TestDefaultPromptRendererSystemPrompts(t *testing.T) {
	r := &DefaultPromptRenderer{}

	for _, role := range []string{RolePlanner, RoleWorker, RoleValidator, RoleMerger} {
		sp, err := r.RenderSystemPrompt(role, nil)
		if err != nil {
			t.Fatalf("RenderSystemPrompt(%s): %v", role, err)
		}
		if sp == "" {
			t.Errorf("system prompt for %s should not be empty", role)
		}
	}
}

func TestDefaultPromptRendererUnknownRole(t *testing.T) {
	r := &DefaultPromptRenderer{}

	_, err := r.RenderPrompt("unknown", nil)
	if err == nil {
		t.Error("expected error for unknown role")
	}

	_, err = r.RenderSystemPrompt("unknown", nil)
	if err == nil {
		t.Error("expected error for unknown role")
	}
}

func TestDefaultPromptRendererWrongDataType(t *testing.T) {
	r := &DefaultPromptRenderer{}

	_, err := r.RenderPrompt(RoleWorker, "not a WorkerPromptData")
	if err == nil {
		t.Error("expected error for wrong data type")
	}
}

func TestValidatorDiagnosticCommands(t *testing.T) {
	r := &DefaultPromptRenderer{}

	task := &tasks.Task{ID: "task-001", Title: "Add auth"}
	prompt, err := r.RenderPrompt(RoleValidator, ValidatorPromptData{
		Task:               task,
		Diff:               "+func Auth() {}",
		DiagnosticCommands: []string{"go test ./...", "go vet ./..."},
	})
	if err != nil {
		t.Fatalf("RenderPrompt: %v", err)
	}
	if !strings.Contains(prompt, "go test ./...") {
		t.Error("prompt should contain diagnostic command")
	}
	if !strings.Contains(prompt, "go vet ./...") {
		t.Error("prompt should contain diagnostic command")
	}
	if !strings.Contains(prompt, "Diagnostic") {
		t.Error("prompt should contain diagnostic section header")
	}
}
