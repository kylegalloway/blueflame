package tasks

import "fmt"

// ValidateDependencies checks for circular dependencies and invalid references.
func ValidateDependencies(tasks []Task) error {
	ids := make(map[string]bool, len(tasks))
	for _, t := range tasks {
		if ids[t.ID] {
			return fmt.Errorf("duplicate task ID: %s", t.ID)
		}
		ids[t.ID] = true
	}

	for _, t := range tasks {
		for _, dep := range t.Dependencies {
			if !ids[dep] {
				return fmt.Errorf("task %s depends on unknown task %s", t.ID, dep)
			}
		}
	}

	if err := detectCycles(tasks); err != nil {
		return err
	}

	return nil
}

// detectCycles uses topological sort (Kahn's algorithm) to find circular dependencies.
func detectCycles(tasks []Task) error {
	inDegree := make(map[string]int, len(tasks))
	dependents := make(map[string][]string, len(tasks))

	for _, t := range tasks {
		if _, ok := inDegree[t.ID]; !ok {
			inDegree[t.ID] = 0
		}
		for _, dep := range t.Dependencies {
			inDegree[t.ID]++
			dependents[dep] = append(dependents[dep], t.ID)
		}
	}

	// Start with nodes that have no incoming edges
	var queue []string
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	visited := 0
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		visited++
		for _, dep := range dependents[node] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if visited != len(tasks) {
		return fmt.Errorf("circular dependency detected among tasks")
	}
	return nil
}

// DependenciesMet returns true if all dependencies of the task are in a completed state.
func DependenciesMet(task *Task, tasks []Task) bool {
	if len(task.Dependencies) == 0 {
		return true
	}

	taskByID := make(map[string]*Task, len(tasks))
	for i := range tasks {
		taskByID[tasks[i].ID] = &tasks[i]
	}

	for _, depID := range task.Dependencies {
		dep, ok := taskByID[depID]
		if !ok {
			return false
		}
		if dep.Status != StatusDone && dep.Status != StatusMerged {
			return false
		}
	}
	return true
}

// CascadeFailure marks all transitive dependents of a failed task as blocked.
func CascadeFailure(failedTaskID string, tasks []Task) {
	queue := []string{failedTaskID}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for i := range tasks {
			if tasks[i].DependsOn(id) && tasks[i].Status != StatusBlocked {
				if tasks[i].Status == StatusPending || tasks[i].Status == StatusFailed {
					tasks[i].MarkBlocked(fmt.Sprintf("dependency %s failed", failedTaskID))
					queue = append(queue, tasks[i].ID)
				}
			}
		}
	}
}

// TopologicalSort returns task IDs in dependency order.
func TopologicalSort(tasks []Task) ([]string, error) {
	inDegree := make(map[string]int, len(tasks))
	dependents := make(map[string][]string, len(tasks))

	for _, t := range tasks {
		if _, ok := inDegree[t.ID]; !ok {
			inDegree[t.ID] = 0
		}
		for _, dep := range t.Dependencies {
			inDegree[t.ID]++
			dependents[dep] = append(dependents[dep], t.ID)
		}
	}

	var queue []string
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	var sorted []string
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		sorted = append(sorted, node)
		for _, dep := range dependents[node] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if len(sorted) != len(tasks) {
		return nil, fmt.Errorf("circular dependency detected")
	}
	return sorted, nil
}
