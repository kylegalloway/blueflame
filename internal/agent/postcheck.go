package agent

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kylegalloway/blueflame/internal/config"
	"github.com/kylegalloway/blueflame/internal/tasks"
)

// sensitivePatterns detects common secrets that should never be committed.
var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:api[_-]?key|apikey)\s*[:=]\s*["']?[a-zA-Z0-9_\-]{16,}`),
	regexp.MustCompile(`-----BEGIN (?:RSA |EC |DSA )?PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)(?:password|passwd|pwd)\s*[:=]\s*["'].+["']`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`), // AWS access key ID
	regexp.MustCompile(`(?i)(?:secret[_-]?key|secretkey)\s*[:=]\s*["']?[a-zA-Z0-9_\-]{16,}`),
}

// PostCheckResult holds the results of post-execution filesystem validation.
type PostCheckResult struct {
	Pass       bool
	Violations []Violation
}

// Violation represents a single post-check violation.
type Violation struct {
	Type string // "path_not_allowed", "blocked_path_modified", "outside_file_scope", etc.
	Path string
}

// AddViolation records a violation and marks the result as failed.
func (r *PostCheckResult) AddViolation(violationType, path string) {
	r.Pass = false
	r.Violations = append(r.Violations, Violation{Type: violationType, Path: path})
}

// PostCheck validates the filesystem changes made by an agent.
func PostCheck(task *tasks.Task, cfg *config.Config) (*PostCheckResult, error) {
	result := &PostCheckResult{Pass: true}

	// Skip git-based checks if the worktree is not a git repo
	if !isGitRepo(task.Worktree) {
		return result, nil
	}

	// Verify commits exist on the branch
	if err := verifyCommitsExist(task.Worktree, cfg.Project.BaseBranch, task.Branch); err != nil {
		result.AddViolation("no_commits", task.Branch)
		return result, nil
	}

	changes, err := gitDiffNameStatus(task.Worktree, cfg.Project.BaseBranch, task.Branch)
	if err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}

	for _, change := range changes {
		// Check: file is within allowed_paths
		if len(cfg.Permissions.AllowedPaths) > 0 && !matchesAnyGlob(change, cfg.Permissions.AllowedPaths) {
			result.AddViolation("path_not_allowed", change)
		}
		// Check: file is NOT in blocked_paths
		if matchesAnyGlob(change, cfg.Permissions.BlockedPaths) {
			result.AddViolation("blocked_path_modified", change)
		}
		// Check: file is within task's file_locks scope
		if cfg.Validation.FileScope.Enforce && !withinFileLocks(change, task.FileLocks) {
			result.AddViolation("outside_file_scope", change)
		}
	}

	// Check for sensitive content in changed files
	for _, v := range containsSensitiveContent(task.Worktree, changes) {
		result.AddViolation(v.Type, v.Path)
	}

	return result, nil
}

// isGitRepo returns true if the given directory is inside a git repository.
func isGitRepo(dir string) bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	return cmd.Run() == nil
}

// verifyCommitsExist checks that there are commits on branch that aren't on baseBranch.
func verifyCommitsExist(repoDir, baseBranch, branch string) error {
	cmd := exec.Command("git", "log", "--oneline", baseBranch+".."+branch)
	cmd.Dir = repoDir
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("git log: %w", err)
	}
	if strings.TrimSpace(string(output)) == "" {
		return fmt.Errorf("no commits on %s beyond %s", branch, baseBranch)
	}
	return nil
}

// containsSensitiveContent scans changed files for secrets like API keys, private keys, etc.
func containsSensitiveContent(repoDir string, files []string) []Violation {
	var violations []Violation
	for _, file := range files {
		cmd := exec.Command("git", "show", "HEAD:"+file)
		cmd.Dir = repoDir
		output, err := cmd.Output()
		if err != nil {
			continue // file may have been deleted
		}
		for _, pat := range sensitivePatterns {
			if pat.Match(output) {
				violations = append(violations, Violation{
					Type: "sensitive_content",
					Path: file,
				})
				break // one violation per file is enough
			}
		}
	}
	return violations
}

// gitDiffNameStatus returns the list of files changed between base and branch.
func gitDiffNameStatus(repoDir, baseBranch, branch string) ([]string, error) {
	cmd := exec.Command("git", "diff", "--name-only", baseBranch+"..."+branch)
	cmd.Dir = repoDir
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --name-only %s...%s: %w", baseBranch, branch, err)
	}

	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// matchesAnyGlob checks if a path matches any of the given glob patterns.
func matchesAnyGlob(path string, patterns []string) bool {
	for _, pattern := range patterns {
		matched, err := filepath.Match(pattern, path)
		if err == nil && matched {
			return true
		}
		// Also try matching against just the filename for simple patterns
		matched, err = filepath.Match(pattern, filepath.Base(path))
		if err == nil && matched {
			return true
		}
	}
	return false
}

// withinFileLocks checks if a path falls within any of the task's declared file locks.
func withinFileLocks(path string, fileLocks []string) bool {
	for _, lock := range fileLocks {
		// Directory lock: path starts with the lock path
		if strings.HasSuffix(lock, "/") {
			if strings.HasPrefix(path, lock) || strings.HasPrefix(path+"/", lock) {
				return true
			}
		} else {
			// File lock: exact match or prefix match
			if path == lock || strings.HasPrefix(path, lock) {
				return true
			}
		}
	}
	return false
}
