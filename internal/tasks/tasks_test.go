package tasks

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTaskStateTransitions(t *testing.T) {
	tests := []struct {
		name      string
		initial   string
		action    func(t *Task) error
		expected  string
		expectErr bool
	}{
		{
			name:    "claim pending task",
			initial: StatusPending,
			action: func(tk *Task) error {
				return tk.Claim("worker-1", "/tmp/wt", "blueflame/task-001")
			},
			expected: StatusClaimed,
		},
		{
			name:    "claim claimed task",
			initial: StatusClaimed,
			action: func(tk *Task) error {
				return tk.Claim("worker-2", "/tmp/wt2", "blueflame/task-001")
			},
			expectErr: true,
		},
		{
			name:     "complete claimed task",
			initial:  StatusClaimed,
			action:   func(tk *Task) error { return tk.Complete() },
			expected: StatusDone,
		},
		{
			name:      "complete pending task",
			initial:   StatusPending,
			action:    func(tk *Task) error { return tk.Complete() },
			expectErr: true,
		},
		{
			name:     "fail claimed task",
			initial:  StatusClaimed,
			action:   func(tk *Task) error { return tk.Fail("timeout") },
			expected: StatusFailed,
		},
		{
			name:      "fail pending task",
			initial:   StatusPending,
			action:    func(tk *Task) error { return tk.Fail("timeout") },
			expectErr: true,
		},
		{
			name:    "requeue failed task",
			initial: StatusFailed,
			action: func(tk *Task) error {
				return tk.Requeue("retry", HistoryEntry{
					Attempt:   1,
					AgentID:   "worker-1",
					Timestamp: time.Now(),
					Result:    "failed",
					Notes:     "timeout",
				})
			},
			expected: StatusPending,
		},
		{
			name:    "requeue done task (validation fail)",
			initial: StatusDone,
			action: func(tk *Task) error {
				return tk.Requeue("validation failed", HistoryEntry{
					Attempt: 1,
					Result:  "validation_failed",
				})
			},
			expected: StatusPending,
		},
		{
			name:      "requeue pending task",
			initial:   StatusPending,
			action:    func(tk *Task) error { return tk.Requeue("retry", HistoryEntry{}) },
			expectErr: true,
		},
		{
			name:     "block pending task",
			initial:  StatusPending,
			action:   func(tk *Task) error { return tk.MarkBlocked("dep failed") },
			expected: StatusBlocked,
		},
		{
			name:      "block claimed task",
			initial:   StatusClaimed,
			action:    func(tk *Task) error { return tk.MarkBlocked("dep failed") },
			expectErr: true,
		},
		{
			name:    "set validation result on done task",
			initial: StatusDone,
			action: func(tk *Task) error {
				return tk.SetValidationResult("pass", "looks good")
			},
			expected: StatusDone,
		},
		{
			name:    "set validation result on pending task",
			initial: StatusPending,
			action: func(tk *Task) error {
				return tk.SetValidationResult("pass", "")
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &Task{ID: "task-001", Status: tt.initial}
			err := tt.action(task)

			if tt.expectErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if task.Status != tt.expected {
				t.Errorf("status = %q, want %q", task.Status, tt.expected)
			}
		})
	}
}

func TestClaimSetsFields(t *testing.T) {
	task := &Task{ID: "task-001", Status: StatusPending}
	err := task.Claim("worker-abc", "/tmp/worktree", "blueflame/task-001")
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if task.AgentID != "worker-abc" {
		t.Errorf("AgentID = %q, want %q", task.AgentID, "worker-abc")
	}
	if task.Worktree != "/tmp/worktree" {
		t.Errorf("Worktree = %q", task.Worktree)
	}
	if task.Branch != "blueflame/task-001" {
		t.Errorf("Branch = %q", task.Branch)
	}
}

func TestRequeueIncrementsRetryAndClearsFields(t *testing.T) {
	task := &Task{
		ID:       "task-001",
		Status:   StatusFailed,
		AgentID:  "worker-1",
		Worktree: "/tmp/wt",
		Branch:   "blueflame/task-001",
	}
	err := task.Requeue("retry", HistoryEntry{Attempt: 1, Result: "failed"})
	if err != nil {
		t.Fatalf("Requeue: %v", err)
	}
	if task.RetryCount != 1 {
		t.Errorf("RetryCount = %d, want 1", task.RetryCount)
	}
	if task.AgentID != "" {
		t.Errorf("AgentID = %q, want empty", task.AgentID)
	}
	if task.Worktree != "" {
		t.Errorf("Worktree = %q, want empty", task.Worktree)
	}
	if len(task.History) != 1 {
		t.Errorf("len(History) = %d, want 1", len(task.History))
	}
}

func TestDependsOn(t *testing.T) {
	task := &Task{
		ID:           "task-002",
		Dependencies: []string{"task-001"},
	}
	if !task.DependsOn("task-001") {
		t.Error("DependsOn(task-001) = false, want true")
	}
	if task.DependsOn("task-003") {
		t.Error("DependsOn(task-003) = true, want false")
	}
}

func TestTaskStoreLoadAndSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yaml")

	// Create initial file
	tf := &TaskFile{
		SchemaVersion: 1,
		SessionID:     "ses-001",
		WaveCycle:     1,
		Tasks: []Task{
			{ID: "task-001", Title: "First task", Status: StatusPending, Priority: 1},
			{ID: "task-002", Title: "Second task", Status: StatusPending, Priority: 2,
				Dependencies: []string{"task-001"}},
		},
	}

	store := NewTaskStore(path)
	store.SetFile(tf)
	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}

	// Load it back
	store2 := NewTaskStore(path)
	if err := store2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if store2.File().SessionID != "ses-001" {
		t.Errorf("SessionID = %q, want %q", store2.File().SessionID, "ses-001")
	}
	if len(store2.Tasks()) != 2 {
		t.Fatalf("len(Tasks) = %d, want 2", len(store2.Tasks()))
	}
	if store2.Tasks()[1].Dependencies[0] != "task-001" {
		t.Errorf("task-002 dependency = %q, want %q",
			store2.Tasks()[1].Dependencies[0], "task-001")
	}
}

func TestTaskStoreFindTask(t *testing.T) {
	store := NewTaskStore("")
	store.SetFile(&TaskFile{
		Tasks: []Task{
			{ID: "task-001", Title: "First"},
			{ID: "task-002", Title: "Second"},
		},
	})

	task := store.FindTask("task-002")
	if task == nil {
		t.Fatal("FindTask returned nil")
	}
	if task.Title != "Second" {
		t.Errorf("Title = %q, want %q", task.Title, "Second")
	}

	if store.FindTask("task-999") != nil {
		t.Error("FindTask(task-999) should return nil")
	}
}

func TestTaskStoreAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "tasks.yaml")

	store := NewTaskStore(path)
	store.SetFile(&TaskFile{
		SchemaVersion: 1,
		Tasks:         []Task{{ID: "task-001", Status: StatusPending}},
	})

	if err := store.Save(); err != nil {
		t.Fatalf("Save to nested dir: %v", err)
	}

	// Verify file exists and is valid
	store2 := NewTaskStore(path)
	if err := store2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(store2.Tasks()) != 1 {
		t.Errorf("len(Tasks) = %d, want 1", len(store2.Tasks()))
	}
}

func TestTaskStoreLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yaml")
	os.WriteFile(path, []byte("{{{invalid"), 0o644)

	store := NewTaskStore(path)
	if err := store.Load(); err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestTaskStoreLoadMissing(t *testing.T) {
	store := NewTaskStore("/nonexistent/tasks.yaml")
	if err := store.Load(); err == nil {
		t.Error("expected error for missing file")
	}
}
