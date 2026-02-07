package orchestrator

import (
	"testing"

	"github.com/kylegalloway/blueflame/internal/tasks"
)

func TestReadyTasksPriorityOrdering(t *testing.T) {
	scheduler := NewScheduler(4)
	allTasks := []tasks.Task{
		{ID: "task-003", Status: tasks.StatusPending, Priority: 3, FileLocks: []string{"pkg/c/"}},
		{ID: "task-001", Status: tasks.StatusPending, Priority: 1, FileLocks: []string{"pkg/a/"}},
		{ID: "task-002", Status: tasks.StatusPending, Priority: 2, FileLocks: []string{"pkg/b/"}},
	}

	ready := scheduler.ReadyTasks(allTasks)
	if len(ready) != 3 {
		t.Fatalf("len(ready) = %d, want 3", len(ready))
	}
	if ready[0].ID != "task-001" {
		t.Errorf("first task = %s, want task-001 (highest priority)", ready[0].ID)
	}
	if ready[1].ID != "task-002" {
		t.Errorf("second task = %s, want task-002", ready[1].ID)
	}
}

func TestReadyTasksRespectsConcurrencyLimit(t *testing.T) {
	scheduler := NewScheduler(2)
	allTasks := []tasks.Task{
		{ID: "task-001", Status: tasks.StatusPending, Priority: 1, FileLocks: []string{"pkg/a/"}},
		{ID: "task-002", Status: tasks.StatusPending, Priority: 2, FileLocks: []string{"pkg/b/"}},
		{ID: "task-003", Status: tasks.StatusPending, Priority: 3, FileLocks: []string{"pkg/c/"}},
	}

	ready := scheduler.ReadyTasks(allTasks)
	if len(ready) != 2 {
		t.Errorf("len(ready) = %d, want 2 (concurrency limit)", len(ready))
	}
}

func TestReadyTasksSkipsNonPending(t *testing.T) {
	scheduler := NewScheduler(4)
	allTasks := []tasks.Task{
		{ID: "task-001", Status: tasks.StatusPending, Priority: 1, FileLocks: []string{"a/"}},
		{ID: "task-002", Status: tasks.StatusClaimed, Priority: 2, FileLocks: []string{"b/"}},
		{ID: "task-003", Status: tasks.StatusDone, Priority: 3, FileLocks: []string{"c/"}},
		{ID: "task-004", Status: tasks.StatusFailed, Priority: 4, FileLocks: []string{"d/"}},
		{ID: "task-005", Status: tasks.StatusBlocked, Priority: 5, FileLocks: []string{"e/"}},
	}

	ready := scheduler.ReadyTasks(allTasks)
	if len(ready) != 1 {
		t.Errorf("len(ready) = %d, want 1 (only pending)", len(ready))
	}
	if ready[0].ID != "task-001" {
		t.Errorf("task = %s, want task-001", ready[0].ID)
	}
}

func TestReadyTasksRespectsUnmetDependencies(t *testing.T) {
	scheduler := NewScheduler(4)
	allTasks := []tasks.Task{
		{ID: "task-001", Status: tasks.StatusPending, Priority: 1, FileLocks: []string{"a/"}},
		{ID: "task-002", Status: tasks.StatusPending, Priority: 2,
			Dependencies: []string{"task-001"}, FileLocks: []string{"b/"}},
	}

	ready := scheduler.ReadyTasks(allTasks)
	if len(ready) != 1 {
		t.Fatalf("len(ready) = %d, want 1", len(ready))
	}
	if ready[0].ID != "task-001" {
		t.Errorf("task = %s, want task-001 (task-002 blocked by dep)", ready[0].ID)
	}
}

func TestReadyTasksDependencyMet(t *testing.T) {
	scheduler := NewScheduler(4)
	allTasks := []tasks.Task{
		{ID: "task-001", Status: tasks.StatusDone, Priority: 1, FileLocks: []string{"a/"}},
		{ID: "task-002", Status: tasks.StatusPending, Priority: 2,
			Dependencies: []string{"task-001"}, FileLocks: []string{"b/"}},
	}

	ready := scheduler.ReadyTasks(allTasks)
	if len(ready) != 1 {
		t.Fatalf("len(ready) = %d, want 1", len(ready))
	}
	if ready[0].ID != "task-002" {
		t.Errorf("task = %s, want task-002 (dep met)", ready[0].ID)
	}
}

func TestReadyTasksLockConflictDeferral(t *testing.T) {
	scheduler := NewScheduler(4)
	allTasks := []tasks.Task{
		{ID: "task-001", Status: tasks.StatusPending, Priority: 1,
			FileLocks: []string{"pkg/middleware/"}},
		{ID: "task-002", Status: tasks.StatusPending, Priority: 2,
			FileLocks: []string{"pkg/middleware/"}}, // conflicts with task-001
		{ID: "task-003", Status: tasks.StatusPending, Priority: 3,
			FileLocks: []string{"pkg/auth/"}}, // no conflict
	}

	ready := scheduler.ReadyTasks(allTasks)
	if len(ready) != 2 {
		t.Fatalf("len(ready) = %d, want 2", len(ready))
	}
	if ready[0].ID != "task-001" {
		t.Errorf("first = %s, want task-001", ready[0].ID)
	}
	if ready[1].ID != "task-003" {
		t.Errorf("second = %s, want task-003 (task-002 deferred due to conflict)", ready[1].ID)
	}
}

func TestReadyTasksHighPriorityConflict(t *testing.T) {
	// Higher priority task has conflict, lower priority doesn't
	scheduler := NewScheduler(2)
	allTasks := []tasks.Task{
		{ID: "task-001", Status: tasks.StatusPending, Priority: 1,
			FileLocks: []string{"shared/"}},
		{ID: "task-002", Status: tasks.StatusPending, Priority: 2,
			FileLocks: []string{"shared/"}},
		{ID: "task-003", Status: tasks.StatusPending, Priority: 3,
			FileLocks: []string{"other/"}},
	}

	ready := scheduler.ReadyTasks(allTasks)
	if len(ready) != 2 {
		t.Fatalf("len(ready) = %d, want 2", len(ready))
	}
	// task-001 gets priority, task-002 deferred, task-003 fills the slot
	if ready[0].ID != "task-001" {
		t.Errorf("first = %s, want task-001", ready[0].ID)
	}
	if ready[1].ID != "task-003" {
		t.Errorf("second = %s, want task-003", ready[1].ID)
	}
}

func TestReadyTasksEmptyList(t *testing.T) {
	scheduler := NewScheduler(4)
	ready := scheduler.ReadyTasks(nil)
	if len(ready) != 0 {
		t.Errorf("len(ready) = %d, want 0", len(ready))
	}
}

func TestReadyTasksAllDone(t *testing.T) {
	scheduler := NewScheduler(4)
	allTasks := []tasks.Task{
		{ID: "task-001", Status: tasks.StatusDone},
		{ID: "task-002", Status: tasks.StatusMerged},
	}
	ready := scheduler.ReadyTasks(allTasks)
	if len(ready) != 0 {
		t.Errorf("len(ready) = %d, want 0", len(ready))
	}
}

func TestHasLocksConflict(t *testing.T) {
	a := &tasks.Task{FileLocks: []string{"pkg/a/", "pkg/b/"}}
	b := &tasks.Task{FileLocks: []string{"pkg/b/", "pkg/c/"}}
	c := &tasks.Task{FileLocks: []string{"pkg/c/", "pkg/d/"}}

	if !HasLocksConflict(a, b) {
		t.Error("expected conflict between a and b (share pkg/b/)")
	}
	if HasLocksConflict(a, c) {
		t.Error("unexpected conflict between a and c")
	}
}
