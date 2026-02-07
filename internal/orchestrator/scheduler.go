package orchestrator

import (
	"sort"

	"github.com/kylegalloway/blueflame/internal/tasks"
)

// Scheduler selects tasks ready for execution based on priority, dependencies, and lock conflicts.
type Scheduler struct {
	maxConcurrency int
}

// NewScheduler creates a Scheduler with the given concurrency limit.
func NewScheduler(maxConcurrency int) *Scheduler {
	return &Scheduler{maxConcurrency: maxConcurrency}
}

// ReadyTasks returns tasks eligible for execution:
// - Status is "pending"
// - All dependencies are met (done or merged)
// - No file_lock conflict with already-selected tasks
// Results are sorted by priority (ascending = higher priority first).
func (s *Scheduler) ReadyTasks(allTasks []tasks.Task) []tasks.Task {
	// Filter to pending tasks with met dependencies
	var candidates []tasks.Task
	for i := range allTasks {
		t := &allTasks[i]
		if t.Status != tasks.StatusPending {
			continue
		}
		if !tasks.DependenciesMet(t, allTasks) {
			continue
		}
		candidates = append(candidates, *t)
	}

	// Sort by priority (lower = higher priority)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Priority < candidates[j].Priority
	})

	// Select up to maxConcurrency tasks without lock conflicts
	var selected []tasks.Task
	usedLocks := make(map[string]bool)

	for _, task := range candidates {
		if len(selected) >= s.maxConcurrency {
			break
		}

		// Check for lock conflicts with already-selected tasks
		conflict := false
		for _, lock := range task.FileLocks {
			if usedLocks[lock] {
				conflict = true
				break
			}
		}
		if conflict {
			continue
		}

		// Mark locks as used
		for _, lock := range task.FileLocks {
			usedLocks[lock] = true
		}
		selected = append(selected, task)
	}

	return selected
}

// HasLocksConflict checks if two tasks have overlapping file locks.
func HasLocksConflict(a, b *tasks.Task) bool {
	locks := make(map[string]bool, len(a.FileLocks))
	for _, l := range a.FileLocks {
		locks[l] = true
	}
	for _, l := range b.FileLocks {
		if locks[l] {
			return true
		}
	}
	return false
}
