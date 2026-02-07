package tasks

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Status constants for task state machine.
const (
	StatusPending  = "pending"
	StatusClaimed  = "claimed"
	StatusDone     = "done"
	StatusFailed   = "failed"
	StatusBlocked  = "blocked"
	StatusRequeued = "requeued"
	StatusMerged   = "merged"
)

// TaskFile represents the top-level tasks.yaml structure.
type TaskFile struct {
	SchemaVersion int    `yaml:"schema_version"`
	SessionID     string `yaml:"session_id"`
	WaveCycle     int    `yaml:"wave_cycle"`
	Tasks         []Task `yaml:"tasks"`
}

// Task represents a single task in the task file.
type Task struct {
	ID             string        `yaml:"id"`
	Title          string        `yaml:"title"`
	Description    string        `yaml:"description"`
	Status         string        `yaml:"status"`
	AgentID        string        `yaml:"agent_id,omitempty"`
	Priority       int           `yaml:"priority"`
	CohesionGroup  string        `yaml:"cohesion_group,omitempty"`
	Dependencies   []string      `yaml:"dependencies"`
	FileLocks      []string      `yaml:"file_locks"`
	Worktree       string        `yaml:"worktree,omitempty"`
	Branch         string        `yaml:"branch,omitempty"`
	RetryCount     int           `yaml:"retry_count"`
	Result         TaskResult    `yaml:"result"`
	History        []HistoryEntry `yaml:"history,omitempty"`
}

// TaskResult holds validation results.
type TaskResult struct {
	Status string `yaml:"status,omitempty"`
	Notes  string `yaml:"notes,omitempty"`
}

// HistoryEntry records a prior attempt.
type HistoryEntry struct {
	Attempt         int       `yaml:"attempt"`
	AgentID         string    `yaml:"agent_id"`
	Timestamp       time.Time `yaml:"timestamp"`
	Result          string    `yaml:"result"`
	Notes           string    `yaml:"notes"`
	RejectionReason string    `yaml:"rejection_reason,omitempty"`
	CostUSD         float64   `yaml:"cost_usd"`
	TokensUsed      int       `yaml:"tokens_used"`
}

// Claim transitions a task from pending to claimed.
func (t *Task) Claim(agentID, worktree, branch string) error {
	if t.Status != StatusPending {
		return fmt.Errorf("cannot claim task %s: status is %q, want %q", t.ID, t.Status, StatusPending)
	}
	t.Status = StatusClaimed
	t.AgentID = agentID
	t.Worktree = worktree
	t.Branch = branch
	return nil
}

// Complete transitions a task from claimed to done.
func (t *Task) Complete() error {
	if t.Status != StatusClaimed {
		return fmt.Errorf("cannot complete task %s: status is %q, want %q", t.ID, t.Status, StatusClaimed)
	}
	t.Status = StatusDone
	return nil
}

// Fail transitions a task from claimed to failed.
func (t *Task) Fail(reason string) error {
	if t.Status != StatusClaimed {
		return fmt.Errorf("cannot fail task %s: status is %q, want %q", t.ID, t.Status, StatusClaimed)
	}
	t.Status = StatusFailed
	t.Result.Notes = reason
	return nil
}

// MarkBlocked transitions a task to blocked.
func (t *Task) MarkBlocked(reason string) error {
	if t.Status != StatusPending && t.Status != StatusFailed {
		return fmt.Errorf("cannot block task %s: status is %q, want %q or %q",
			t.ID, t.Status, StatusPending, StatusFailed)
	}
	t.Status = StatusBlocked
	t.Result.Notes = reason
	return nil
}

// Requeue transitions a task back to pending with history.
func (t *Task) Requeue(notes string, entry HistoryEntry) error {
	if t.Status != StatusFailed && t.Status != StatusDone {
		return fmt.Errorf("cannot requeue task %s: status is %q, want %q or %q",
			t.ID, t.Status, StatusFailed, StatusDone)
	}
	t.History = append(t.History, entry)
	t.Status = StatusPending
	t.AgentID = ""
	t.Worktree = ""
	t.Branch = ""
	t.RetryCount++
	return nil
}

// SetValidationResult records the validation outcome.
func (t *Task) SetValidationResult(status string, notes string) error {
	if t.Status != StatusDone {
		return fmt.Errorf("cannot set validation result on task %s: status is %q, want %q",
			t.ID, t.Status, StatusDone)
	}
	t.Result.Status = status
	t.Result.Notes = notes
	return nil
}

// DependsOn returns true if this task depends on the given task ID.
func (t *Task) DependsOn(taskID string) bool {
	for _, dep := range t.Dependencies {
		if dep == taskID {
			return true
		}
	}
	return false
}

// TaskStore manages task persistence and state.
type TaskStore struct {
	path string
	file *TaskFile
}

// NewTaskStore creates a TaskStore from a file path.
func NewTaskStore(path string) *TaskStore {
	return &TaskStore{path: path}
}

// Load reads the task file from disk.
func (s *TaskStore) Load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return fmt.Errorf("read tasks: %w", err)
	}
	var tf TaskFile
	if err := yaml.Unmarshal(data, &tf); err != nil {
		return fmt.Errorf("parse tasks: %w", err)
	}
	s.file = &tf
	return nil
}

// Save writes the task file to disk atomically (write-to-temp-then-rename).
func (s *TaskStore) Save() error {
	if s.file == nil {
		return fmt.Errorf("no task file loaded")
	}
	data, err := yaml.Marshal(s.file)
	if err != nil {
		return fmt.Errorf("marshal tasks: %w", err)
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create tasks dir: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, "tasks-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, s.path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

// File returns the current in-memory TaskFile.
func (s *TaskStore) File() *TaskFile {
	return s.file
}

// SetFile replaces the in-memory TaskFile.
func (s *TaskStore) SetFile(tf *TaskFile) {
	s.file = tf
}

// Tasks returns the task list.
func (s *TaskStore) Tasks() []Task {
	if s.file == nil {
		return nil
	}
	return s.file.Tasks
}

// FindTask returns a pointer to the task with the given ID.
func (s *TaskStore) FindTask(id string) *Task {
	if s.file == nil {
		return nil
	}
	for i := range s.file.Tasks {
		if s.file.Tasks[i].ID == id {
			return &s.file.Tasks[i]
		}
	}
	return nil
}
