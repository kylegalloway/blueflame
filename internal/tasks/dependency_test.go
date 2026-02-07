package tasks

import "testing"

func TestValidateDependenciesLinear(t *testing.T) {
	// A -> B -> C
	tasks := []Task{
		{ID: "task-001"},
		{ID: "task-002", Dependencies: []string{"task-001"}},
		{ID: "task-003", Dependencies: []string{"task-002"}},
	}
	if err := ValidateDependencies(tasks); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateDependenciesDiamond(t *testing.T) {
	// A -> B, A -> C, B -> D, C -> D
	tasks := []Task{
		{ID: "A"},
		{ID: "B", Dependencies: []string{"A"}},
		{ID: "C", Dependencies: []string{"A"}},
		{ID: "D", Dependencies: []string{"B", "C"}},
	}
	if err := ValidateDependencies(tasks); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateDependenciesCircular(t *testing.T) {
	// A -> B -> A
	tasks := []Task{
		{ID: "A", Dependencies: []string{"B"}},
		{ID: "B", Dependencies: []string{"A"}},
	}
	if err := ValidateDependencies(tasks); err == nil {
		t.Error("expected error for circular dependency")
	}
}

func TestValidateDependenciesCircularThree(t *testing.T) {
	// A -> B -> C -> A
	tasks := []Task{
		{ID: "A", Dependencies: []string{"C"}},
		{ID: "B", Dependencies: []string{"A"}},
		{ID: "C", Dependencies: []string{"B"}},
	}
	if err := ValidateDependencies(tasks); err == nil {
		t.Error("expected error for circular dependency")
	}
}

func TestValidateDependenciesUnknownRef(t *testing.T) {
	tasks := []Task{
		{ID: "A", Dependencies: []string{"nonexistent"}},
	}
	if err := ValidateDependencies(tasks); err == nil {
		t.Error("expected error for unknown dependency reference")
	}
}

func TestValidateDependenciesDuplicateID(t *testing.T) {
	tasks := []Task{
		{ID: "A"},
		{ID: "A"},
	}
	if err := ValidateDependencies(tasks); err == nil {
		t.Error("expected error for duplicate task ID")
	}
}

func TestDependenciesMetAllDone(t *testing.T) {
	tasks := []Task{
		{ID: "task-001", Status: StatusDone},
		{ID: "task-002", Status: StatusPending, Dependencies: []string{"task-001"}},
	}
	if !DependenciesMet(&tasks[1], tasks) {
		t.Error("DependenciesMet = false, want true (dep is done)")
	}
}

func TestDependenciesMetDepPending(t *testing.T) {
	tasks := []Task{
		{ID: "task-001", Status: StatusPending},
		{ID: "task-002", Status: StatusPending, Dependencies: []string{"task-001"}},
	}
	if DependenciesMet(&tasks[1], tasks) {
		t.Error("DependenciesMet = true, want false (dep is pending)")
	}
}

func TestDependenciesMetNoDeps(t *testing.T) {
	tasks := []Task{
		{ID: "task-001", Status: StatusPending},
	}
	if !DependenciesMet(&tasks[0], tasks) {
		t.Error("DependenciesMet = false, want true (no deps)")
	}
}

func TestDependenciesMetMergedCounts(t *testing.T) {
	tasks := []Task{
		{ID: "task-001", Status: StatusMerged},
		{ID: "task-002", Status: StatusPending, Dependencies: []string{"task-001"}},
	}
	if !DependenciesMet(&tasks[1], tasks) {
		t.Error("DependenciesMet = false, want true (dep is merged)")
	}
}

func TestCascadeFailure(t *testing.T) {
	tasks := []Task{
		{ID: "task-001", Status: StatusFailed},
		{ID: "task-002", Status: StatusPending, Dependencies: []string{"task-001"}},
		{ID: "task-003", Status: StatusPending, Dependencies: []string{"task-002"}},
		{ID: "task-004", Status: StatusPending}, // no dependency, should not be affected
	}

	CascadeFailure("task-001", tasks)

	if tasks[1].Status != StatusBlocked {
		t.Errorf("task-002 status = %q, want %q", tasks[1].Status, StatusBlocked)
	}
	if tasks[2].Status != StatusBlocked {
		t.Errorf("task-003 status = %q, want %q", tasks[2].Status, StatusBlocked)
	}
	if tasks[3].Status != StatusPending {
		t.Errorf("task-004 status = %q, want %q (should be unaffected)", tasks[3].Status, StatusPending)
	}
}

func TestTopologicalSort(t *testing.T) {
	tasks := []Task{
		{ID: "C", Dependencies: []string{"B"}},
		{ID: "A"},
		{ID: "B", Dependencies: []string{"A"}},
	}

	sorted, err := TopologicalSort(tasks)
	if err != nil {
		t.Fatalf("TopologicalSort: %v", err)
	}

	// A must come before B, B must come before C
	indexOf := make(map[string]int)
	for i, id := range sorted {
		indexOf[id] = i
	}

	if indexOf["A"] > indexOf["B"] {
		t.Errorf("A (index %d) should come before B (index %d)", indexOf["A"], indexOf["B"])
	}
	if indexOf["B"] > indexOf["C"] {
		t.Errorf("B (index %d) should come before C (index %d)", indexOf["B"], indexOf["C"])
	}
}

func TestTopologicalSortCircular(t *testing.T) {
	tasks := []Task{
		{ID: "A", Dependencies: []string{"B"}},
		{ID: "B", Dependencies: []string{"A"}},
	}

	_, err := TopologicalSort(tasks)
	if err == nil {
		t.Error("expected error for circular dependency")
	}
}
