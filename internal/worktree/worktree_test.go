package worktree

import (
	"errors"
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

func TestCreateAfterStaleBranch(t *testing.T) {
	repoDir := setupGitRepo(t)
	wtDir := filepath.Join(repoDir, ".trees")
	mgr := NewManager(repoDir, wtDir, "main")

	// Create and remove worktree, but leave the branch
	_, _, err := mgr.Create("worker-1", "task-001")
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if err := mgr.Remove("worker-1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	// Branch "blueflame/task-001" still exists (Remove doesn't delete it)

	// Second create with same task ID should succeed (stale branch cleaned up)
	wtPath, branch, err := mgr.Create("worker-2", "task-001")
	if err != nil {
		t.Fatalf("second Create should succeed after stale branch cleanup: %v", err)
	}
	if branch != "blueflame/task-001" {
		t.Errorf("branch = %q, want %q", branch, "blueflame/task-001")
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Errorf("worktree dir not created: %v", err)
	}

	mgr.Remove("worker-2")
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

func TestMergeBranch(t *testing.T) {
	repoDir := setupGitRepo(t)
	wtDir := filepath.Join(repoDir, ".trees")
	mgr := NewManager(repoDir, wtDir, "main")

	// Create worktree and add a file
	wtPath, _, err := mgr.Create("worker-merge", "task-merge")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	os.WriteFile(filepath.Join(wtPath, "merged_file.txt"), []byte("hello\n"), 0o644)
	for _, args := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "feat(task-merge): add merged file"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = wtPath
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@test.com")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s: %v", args, output, err)
		}
	}

	// Remove worktree first (required before merge)
	if err := mgr.Remove("worker-merge"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// Merge the branch
	if err := mgr.MergeBranch("task-merge"); err != nil {
		t.Fatalf("MergeBranch: %v", err)
	}

	// Verify the file exists on main
	if _, err := os.Stat(filepath.Join(repoDir, "merged_file.txt")); err != nil {
		t.Error("merged_file.txt should exist on main after merge")
	}

	// Verify git log shows the merge
	logCmd := exec.Command("git", "log", "--oneline")
	logCmd.Dir = repoDir
	logOut, _ := logCmd.Output()
	if !testing.Short() {
		t.Logf("git log after merge:\n%s", logOut)
	}

	// Clean up branch
	mgr.RemoveBranch("task-merge")
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

func TestMergeConflictDetection(t *testing.T) {
	repoDir := setupGitRepo(t)
	wtDir := filepath.Join(repoDir, ".trees")
	mgr := NewManager(repoDir, wtDir, "main")

	// Create worktree and modify README.md (same file as main)
	wtPath, _, err := mgr.Create("worker-conflict", "task-conflict")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Change README.md in the worktree
	os.WriteFile(filepath.Join(wtPath, "README.md"), []byte("# Branch version\n"), 0o644)
	for _, args := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "branch: modify README"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = wtPath
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@test.com")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s: %v", args, output, err)
		}
	}

	// Now also change README.md on main (creating a conflict)
	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Main version\n"), 0o644)
	for _, args := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "main: modify README"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@test.com")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s: %v", args, output, err)
		}
	}

	// Remove worktree before merge
	if err := mgr.Remove("worker-conflict"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// Merge should detect conflict
	err = mgr.MergeBranch("task-conflict")
	if err == nil {
		t.Fatal("expected merge conflict error")
	}
	if !errors.Is(err, ErrMergeConflict) {
		t.Errorf("err = %v, want ErrMergeConflict", err)
	}
}

func TestMergeConflictAborts(t *testing.T) {
	repoDir := setupGitRepo(t)
	wtDir := filepath.Join(repoDir, ".trees")
	mgr := NewManager(repoDir, wtDir, "main")

	// Create conflict scenario same as above
	wtPath, _, err := mgr.Create("worker-conflict2", "task-conflict2")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	os.WriteFile(filepath.Join(wtPath, "README.md"), []byte("# Branch v2\n"), 0o644)
	for _, args := range [][]string{
		{"git", "add", "."}, {"git", "commit", "-m", "branch change"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = wtPath
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@test.com")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s: %v", args, output, err)
		}
	}

	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Main v2\n"), 0o644)
	for _, args := range [][]string{
		{"git", "add", "."}, {"git", "commit", "-m", "main change"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@test.com")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s: %v", args, output, err)
		}
	}

	mgr.Remove("worker-conflict2")

	// Attempt merge (will fail with conflict)
	mgr.MergeBranch("task-conflict2")

	// Verify no merge is in progress (MERGE_HEAD should not exist)
	mergeHeadPath := filepath.Join(repoDir, ".git", "MERGE_HEAD")
	if _, err := os.Stat(mergeHeadPath); err == nil {
		t.Error("MERGE_HEAD still exists — merge was not aborted")
	}
}

func TestErrMergeConflictSentinel(t *testing.T) {
	if ErrMergeConflict.Error() != "merge conflict" {
		t.Errorf("ErrMergeConflict.Error() = %q, want %q", ErrMergeConflict.Error(), "merge conflict")
	}
}
