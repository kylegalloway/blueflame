package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kylegalloway/blueflame/internal/config"
	"github.com/kylegalloway/blueflame/internal/tasks"
)

func TestMatchesAnyGlob(t *testing.T) {
	tests := []struct {
		path     string
		patterns []string
		want     bool
	}{
		{"src/main.go", []string{"src/**"}, true},   // ** matches via filepath.Match on base
		{"src/main.go", []string{"src/*.go"}, true},
		{".env", []string{".env*"}, true},
		{".env.local", []string{".env*"}, true},
		{"pkg/auth/handler.go", []string{"*.secret"}, false},
		{"credentials.secret", []string{"*.secret"}, true},
		{"foo/bar.go", []string{}, false},
	}

	for _, tt := range tests {
		got := matchesAnyGlob(tt.path, tt.patterns)
		if got != tt.want {
			t.Errorf("matchesAnyGlob(%q, %v) = %v, want %v", tt.path, tt.patterns, got, tt.want)
		}
	}
}

func TestWithinFileLocks(t *testing.T) {
	tests := []struct {
		path      string
		fileLocks []string
		want      bool
	}{
		{"pkg/middleware/auth.go", []string{"pkg/middleware/"}, true},
		{"pkg/middleware/nested/deep.go", []string{"pkg/middleware/"}, true},
		{"pkg/other/file.go", []string{"pkg/middleware/"}, false},
		{"internal/auth/handler.go", []string{"internal/auth/"}, true},
		{"internal/auth/handler.go", []string{"internal/other/"}, false},
		{"exact_file.go", []string{"exact_file.go"}, true},
		{"other_file.go", []string{"exact_file.go"}, false},
	}

	for _, tt := range tests {
		got := withinFileLocks(tt.path, tt.fileLocks)
		if got != tt.want {
			t.Errorf("withinFileLocks(%q, %v) = %v, want %v", tt.path, tt.fileLocks, got, tt.want)
		}
	}
}

func TestPostCheckResultViolations(t *testing.T) {
	result := &PostCheckResult{Pass: true}
	if !result.Pass {
		t.Error("initial state should be pass")
	}

	result.AddViolation("blocked_path_modified", ".env")
	if result.Pass {
		t.Error("should be fail after adding violation")
	}
	if len(result.Violations) != 1 {
		t.Errorf("len(violations) = %d, want 1", len(result.Violations))
	}
	if result.Violations[0].Type != "blocked_path_modified" {
		t.Errorf("type = %q", result.Violations[0].Type)
	}
}

// setupGitRepo creates a temp git repo with a base branch and optionally a feature branch
// with a commit. Returns (repoDir, cleanup).
func setupGitRepo(t *testing.T, baseBranch string, withCommit bool) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "checkout", "-b", baseBranch},
		{"git", "commit", "--allow-empty", "-m", "initial"},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v: %s: %v", args, out, err)
		}
	}

	if withCommit {
		featureCmds := [][]string{
			{"git", "checkout", "-b", "blueflame/task-001"},
		}
		for _, args := range featureCmds {
			cmd := exec.Command(args[0], args[1:]...)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(),
				"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
				"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
			)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("setup %v: %s: %v", args, out, err)
			}
		}
		// Create a file and commit
		if err := os.WriteFile(filepath.Join(dir, "hello.go"), []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{
			{"git", "add", "hello.go"},
			{"git", "commit", "-m", "add hello.go"},
		} {
			cmd := exec.Command(args[0], args[1:]...)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(),
				"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
				"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
			)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("setup %v: %s: %v", args, out, err)
			}
		}
	}

	return dir
}

func TestVerifyCommitsExist(t *testing.T) {
	dir := setupGitRepo(t, "main", true)

	// Branch with commits should pass
	if err := verifyCommitsExist(dir, "main", "blueflame/task-001"); err != nil {
		t.Errorf("expected no error for branch with commits: %v", err)
	}
}

func TestVerifyCommitsExistEmpty(t *testing.T) {
	dir := setupGitRepo(t, "main", false)

	// Create empty branch (no commits beyond main)
	cmd := exec.Command("git", "checkout", "-b", "blueflame/empty")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git checkout: %s: %v", out, err)
	}

	if err := verifyCommitsExist(dir, "main", "blueflame/empty"); err == nil {
		t.Error("expected error for branch with no commits")
	}
}

func TestPostCheckNoCommits(t *testing.T) {
	dir := setupGitRepo(t, "main", false)

	// Create empty branch
	cmd := exec.Command("git", "checkout", "-b", "blueflame/task-empty")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git checkout: %s: %v", out, err)
	}

	task := &tasks.Task{
		ID:       "task-empty",
		Worktree: dir,
		Branch:   "blueflame/task-empty",
	}
	cfg := &config.Config{
		Project: config.ProjectConfig{BaseBranch: "main"},
	}

	result, err := PostCheck(task, cfg)
	if err != nil {
		t.Fatalf("PostCheck: %v", err)
	}
	if result.Pass {
		t.Error("PostCheck should fail for branch with no commits")
	}
	found := false
	for _, v := range result.Violations {
		if v.Type == "no_commits" {
			found = true
		}
	}
	if !found {
		t.Error("expected no_commits violation")
	}
}

func TestSensitiveContentDetection(t *testing.T) {
	dir := setupGitRepo(t, "main", false)

	// Create branch with sensitive content
	cmd := exec.Command("git", "checkout", "-b", "blueflame/task-secrets")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git checkout: %s: %v", out, err)
	}

	secret := `package config
const API_KEY = "sk-1234567890abcdef1234567890abcdef"
`
	if err := os.WriteFile(filepath.Join(dir, "config.go"), []byte(secret), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"git", "add", "config.go"},
		{"git", "commit", "-m", "add config"},
	} {
		c := exec.Command(args[0], args[1:]...)
		c.Dir = dir
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s: %v", args, out, err)
		}
	}

	violations := containsSensitiveContent(dir, []string{"config.go"})
	if len(violations) == 0 {
		t.Error("expected sensitive_content violation for API key")
	}
}

func TestSensitiveContentClean(t *testing.T) {
	dir := setupGitRepo(t, "main", true)
	violations := containsSensitiveContent(dir, []string{"hello.go"})
	if len(violations) != 0 {
		t.Errorf("expected no violations for clean file, got %v", violations)
	}
}
