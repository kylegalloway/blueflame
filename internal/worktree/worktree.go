package worktree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Manager handles git worktree operations.
type Manager struct {
	repoDir     string
	worktreeDir string
	baseBranch  string
}

// NewManager creates a new worktree Manager.
func NewManager(repoDir, worktreeDir, baseBranch string) *Manager {
	wtDir := worktreeDir
	if !filepath.IsAbs(wtDir) {
		wtDir = filepath.Join(repoDir, wtDir)
	}
	// Resolve symlinks so paths match git's resolved paths (macOS /var -> /private/var)
	if resolved, err := filepath.EvalSymlinks(filepath.Dir(wtDir)); err == nil {
		wtDir = filepath.Join(resolved, filepath.Base(wtDir))
	}
	return &Manager{
		repoDir:     repoDir,
		worktreeDir: wtDir,
		baseBranch:  baseBranch,
	}
}

// BranchName returns the standard branch name for a task.
func BranchName(taskID string) string {
	return "blueflame/" + taskID
}

// WorktreePath returns the filesystem path for an agent's worktree.
func (m *Manager) WorktreePath(agentID string) string {
	return filepath.Join(m.worktreeDir, agentID)
}

// Create creates a new git worktree with a dedicated branch.
func (m *Manager) Create(agentID, taskID string) (string, string, error) {
	wtPath := m.WorktreePath(agentID)
	branch := BranchName(taskID)

	if err := os.MkdirAll(filepath.Dir(wtPath), 0o755); err != nil {
		return "", "", fmt.Errorf("create worktree parent dir: %w", err)
	}

	// Ensure the base branch exists (handles empty repos with no commits).
	if err := m.ensureBaseBranch(); err != nil {
		return "", "", fmt.Errorf("ensure base branch: %w", err)
	}

	// Best-effort cleanup of stale branch from previous run
	delCmd := exec.Command("git", "branch", "-D", branch)
	delCmd.Dir = m.repoDir
	_ = delCmd.Run()

	cmd := exec.Command("git", "worktree", "add", "-b", branch, wtPath, m.baseBranch)
	cmd.Dir = m.repoDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("git worktree add: %s: %w", strings.TrimSpace(string(output)), err)
	}

	return wtPath, branch, nil
}

// ensureBaseBranch creates the base branch with an initial commit if the repo has no commits.
func (m *Manager) ensureBaseBranch() error {
	// Check if the base branch ref exists.
	cmd := exec.Command("git", "rev-parse", "--verify", m.baseBranch)
	cmd.Dir = m.repoDir
	if err := cmd.Run(); err == nil {
		return nil // branch exists
	}

	// No base branch — create an initial empty commit.
	commitCmd := exec.Command("git", "commit", "--allow-empty", "-m", "Initial commit (blueflame)")
	commitCmd.Dir = m.repoDir
	if output, err := commitCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("create initial commit: %s: %w", strings.TrimSpace(string(output)), err)
	}

	// If the default branch isn't our base branch, rename it.
	headCmd := exec.Command("git", "symbolic-ref", "--short", "HEAD")
	headCmd.Dir = m.repoDir
	headOut, err := headCmd.Output()
	if err != nil {
		return nil // commit was created, branch naming is best-effort
	}
	currentBranch := strings.TrimSpace(string(headOut))
	if currentBranch != m.baseBranch {
		renameCmd := exec.Command("git", "branch", "-m", currentBranch, m.baseBranch)
		renameCmd.Dir = m.repoDir
		if output, err := renameCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("rename branch to %s: %s: %w", m.baseBranch, strings.TrimSpace(string(output)), err)
		}
	}

	return nil
}

// Remove removes a git worktree and its branch.
func (m *Manager) Remove(agentID string) error {
	wtPath := m.WorktreePath(agentID)

	cmd := exec.Command("git", "worktree", "remove", "--force", wtPath)
	cmd.Dir = m.repoDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree remove: %s: %w", strings.TrimSpace(string(output)), err)
	}

	return nil
}

// RemoveBranch deletes a worktree branch.
func (m *Manager) RemoveBranch(taskID string) error {
	branch := BranchName(taskID)
	cmd := exec.Command("git", "branch", "-D", branch)
	cmd.Dir = m.repoDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git branch -D %s: %s: %w", branch, strings.TrimSpace(string(output)), err)
	}
	return nil
}

// List returns all active worktree paths managed by blueflame.
func (m *Manager) List() ([]string, error) {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = m.repoDir
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git worktree list: %w", err)
	}

	// Resolve symlinks in our managed directory for comparison (macOS /var -> /private/var)
	resolvedWtDir := m.worktreeDir
	if resolved, err := filepath.EvalSymlinks(m.worktreeDir); err == nil {
		resolvedWtDir = resolved
	}

	var paths []string
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "worktree ") {
			path := strings.TrimPrefix(line, "worktree ")
			if strings.HasPrefix(path, resolvedWtDir) || strings.HasPrefix(path, m.worktreeDir) {
				paths = append(paths, path)
			}
		}
	}
	return paths, nil
}

// FindStale returns worktree paths that exist on disk but may be from a previous session.
func (m *Manager) FindStale() ([]string, error) {
	entries, err := os.ReadDir(m.worktreeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read worktree dir: %w", err)
	}

	var stale []string
	for _, entry := range entries {
		if entry.IsDir() {
			stale = append(stale, filepath.Join(m.worktreeDir, entry.Name()))
		}
	}
	return stale, nil
}

// Diff returns the git diff between the base branch and the task branch.
func (m *Manager) Diff(taskID string) (string, error) {
	branch := BranchName(taskID)
	cmd := exec.Command("git", "diff", m.baseBranch+"..."+branch)
	cmd.Dir = m.repoDir
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git diff: %w", err)
	}
	return string(output), nil
}
