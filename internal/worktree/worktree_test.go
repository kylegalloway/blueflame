package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// setupGitRepo creates a temp directory with an initialized git repo and an initial commit.
func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	commands := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "checkout", "-b", "main"},
	}
	for _, args := range commands {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s: %v", args, output, err)
		}
	}

	// Create initial commit so we have a base branch
	testFile := filepath.Join(dir, "README.md")
	if err := os.WriteFile(testFile, []byte("# Test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "initial commit"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s: %v", args, output, err)
		}
	}

	return dir
}

func TestBranchName(t *testing.T) {
	if got := BranchName("task-001"); got != "blueflame/task-001" {
		t.Errorf("BranchName = %q, want %q", got, "blueflame/task-001")
	}
}

func TestCreateAndRemoveWorktree(t *testing.T) {
	repoDir := setupGitRepo(t)
	wtDir := filepath.Join(repoDir, ".trees")
	mgr := NewManager(repoDir, wtDir, "main")

	// Create
	wtPath, branch, err := mgr.Create("worker-abc12345", "task-001")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if branch != "blueflame/task-001" {
		t.Errorf("branch = %q, want %q", branch, "blueflame/task-001")
	}

	// Verify worktree directory exists
	if _, err := os.Stat(wtPath); err != nil {
		t.Errorf("worktree dir not created: %v", err)
	}

	// Verify it's in the list
	paths, err := mgr.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, p := range paths {
		if p == wtPath {
			found = true
		}
	}
	if !found {
		t.Errorf("worktree %q not in list %v", wtPath, paths)
	}

	// Remove
	if err := mgr.Remove("worker-abc12345"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Error("worktree dir still exists after remove")
	}
}

func TestCreateDuplicateBranch(t *testing.T) {
	repoDir := setupGitRepo(t)
	wtDir := filepath.Join(repoDir, ".trees")
	mgr := NewManager(repoDir, wtDir, "main")

	_, _, err := mgr.Create("worker-1", "task-001")
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}

	// Second create with same task ID should fail (branch exists)
	_, _, err = mgr.Create("worker-2", "task-001")
	if err == nil {
		t.Error("expected error for duplicate branch")
	}

	// Cleanup
	mgr.Remove("worker-1")
}

func TestWorktreePath(t *testing.T) {
	mgr := NewManager("/repo", "/repo/.trees", "main")
	got := mgr.WorktreePath("worker-abc")
	want := "/repo/.trees/worker-abc"
	if got != want {
		t.Errorf("WorktreePath = %q, want %q", got, want)
	}
}

func TestRelativeWorktreeDir(t *testing.T) {
	mgr := NewManager("/repo", ".trees", "main")
	got := mgr.WorktreePath("worker-abc")
	want := "/repo/.trees/worker-abc"
	if got != want {
		t.Errorf("WorktreePath = %q, want %q", got, want)
	}
}

func TestListEmpty(t *testing.T) {
	repoDir := setupGitRepo(t)
	mgr := NewManager(repoDir, filepath.Join(repoDir, ".trees"), "main")

	paths, err := mgr.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("len(paths) = %d, want 0", len(paths))
	}
}

func TestFindStaleEmpty(t *testing.T) {
	mgr := NewManager("/nonexistent", "/nonexistent/.trees", "main")
	stale, err := mgr.FindStale()
	if err != nil {
		t.Fatalf("FindStale: %v", err)
	}
	if len(stale) != 0 {
		t.Errorf("len(stale) = %d, want 0", len(stale))
	}
}

func TestDiff(t *testing.T) {
	repoDir := setupGitRepo(t)
	wtDir := filepath.Join(repoDir, ".trees")
	mgr := NewManager(repoDir, wtDir, "main")

	wtPath, _, err := mgr.Create("worker-diff", "task-diff")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Make a change in the worktree
	os.WriteFile(filepath.Join(wtPath, "new_file.go"), []byte("package main\n"), 0o644)
	for _, args := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "feat(task-diff): add new file"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = wtPath
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@test.com")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s: %v", args, output, err)
		}
	}

	diff, err := mgr.Diff("task-diff")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if diff == "" {
		t.Error("expected non-empty diff")
	}

	mgr.Remove("worker-diff")
}
