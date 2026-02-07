package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir
}

func TestParseFullConfig(t *testing.T) {
	repoDir := setupTestRepo(t)

	data, err := os.ReadFile(filepath.Join("../../testdata/configs/valid_full.yaml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	// Patch repo path to temp dir
	patched := replaceRepoPath(data, repoDir)

	cfg, err := Parse(patched)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if cfg.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", cfg.SchemaVersion)
	}
	if cfg.Project.Name != "test-project" {
		t.Errorf("project.name = %q, want %q", cfg.Project.Name, "test-project")
	}
	if cfg.Concurrency.Development != 4 {
		t.Errorf("concurrency.development = %d, want 4", cfg.Concurrency.Development)
	}
	if cfg.Limits.AgentTimeout != 300*time.Second {
		t.Errorf("limits.agent_timeout = %v, want 300s", cfg.Limits.AgentTimeout)
	}
	if cfg.Limits.MaxSessionCostUSD != 10.0 {
		t.Errorf("limits.max_session_cost_usd = %f, want 10.0", cfg.Limits.MaxSessionCostUSD)
	}
	if cfg.Models.Validator != "haiku" {
		t.Errorf("models.validator = %q, want %q", cfg.Models.Validator, "haiku")
	}
	if !cfg.Planning.Interactive {
		t.Error("planning.interactive = false, want true")
	}
	if cfg.Sandbox.MaxMemoryMB != 2048 {
		t.Errorf("sandbox.max_memory_mb = %d, want 2048", cfg.Sandbox.MaxMemoryMB)
	}
	if len(cfg.Permissions.AllowedPaths) != 2 {
		t.Errorf("len(permissions.allowed_paths) = %d, want 2", len(cfg.Permissions.AllowedPaths))
	}
	if cfg.Validation.CommitFormat.Pattern == "" {
		t.Error("validation.commit_format.pattern is empty")
	}
	if !cfg.Validation.ValidatorDiagnostics.Enabled {
		t.Error("validation.validator_diagnostics.enabled = false, want true")
	}
	if len(cfg.Validation.ValidatorDiagnostics.Commands) != 2 {
		t.Errorf("len(validator_diagnostics.commands) = %d, want 2",
			len(cfg.Validation.ValidatorDiagnostics.Commands))
	}

	// Budget checks
	wb := cfg.Limits.TokenBudget.WorkerBudget()
	if wb.Unit != USD || wb.Value != 1.50 {
		t.Errorf("worker budget = %+v, want USD/1.50", wb)
	}
}

func TestParseMinimalConfig(t *testing.T) {
	repoDir := setupTestRepo(t)

	data, err := os.ReadFile(filepath.Join("../../testdata/configs/minimal.yaml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	patched := replaceRepoPath(data, repoDir)

	cfg, err := Parse(patched)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Check defaults applied
	if cfg.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", cfg.SchemaVersion)
	}
	if cfg.Concurrency.Development != 4 {
		t.Errorf("concurrency.development = %d, want default 4", cfg.Concurrency.Development)
	}
	if cfg.Limits.AgentTimeout != 300*time.Second {
		t.Errorf("limits.agent_timeout = %v, want default 300s", cfg.Limits.AgentTimeout)
	}
	if cfg.Models.Worker != "sonnet" {
		t.Errorf("models.worker = %q, want default %q", cfg.Models.Worker, "sonnet")
	}
	if cfg.Project.WorktreeDir != ".trees" {
		t.Errorf("project.worktree_dir = %q, want default %q", cfg.Project.WorktreeDir, ".trees")
	}
}

func TestValidateRejectsMissingName(t *testing.T) {
	cfg := &Config{
		Project: ProjectConfig{Repo: t.TempDir()},
	}
	applyDefaults(cfg)
	if err := Validate(cfg); err == nil {
		t.Error("expected error for missing project.name")
	}
}

func TestValidateRejectsMissingRepo(t *testing.T) {
	cfg := &Config{
		Project: ProjectConfig{Name: "test"},
	}
	applyDefaults(cfg)
	if err := Validate(cfg); err == nil {
		t.Error("expected error for missing project.repo")
	}
}

func TestValidateRejectsBadConcurrency(t *testing.T) {
	repoDir := setupTestRepo(t)
	cfg := &Config{
		Project:     ProjectConfig{Name: "test", Repo: repoDir},
		Concurrency: ConcurrencyConfig{Development: 10},
	}
	applyDefaults(cfg)
	// Override the default since we set it to 10 before defaults
	cfg.Concurrency.Development = 10
	if err := Validate(cfg); err == nil {
		t.Error("expected error for concurrency.development = 10")
	}
}

func TestValidateRejectsBadRegex(t *testing.T) {
	repoDir := setupTestRepo(t)
	cfg := &Config{
		Project: ProjectConfig{Name: "test", Repo: repoDir},
		Permissions: PermissionsConfig{
			BashRules: BashRules{
				BlockedPatterns: []string{"[invalid"},
			},
		},
	}
	applyDefaults(cfg)
	if err := Validate(cfg); err == nil {
		t.Error("expected error for invalid regex pattern")
	}
}

func TestValidateRejectsDualBudget(t *testing.T) {
	repoDir := setupTestRepo(t)
	cfg := &Config{
		Project: ProjectConfig{Name: "test", Repo: repoDir},
		Limits: LimitsConfig{
			TokenBudget: TokenBudget{
				WorkerUSD:    1.50,
				WorkerTokens: 50000,
			},
		},
	}
	applyDefaults(cfg)
	if err := Validate(cfg); err == nil {
		t.Error("expected error for both _usd and _tokens non-zero")
	}
}

func TestValidateRejectsDualSessionLimits(t *testing.T) {
	repoDir := setupTestRepo(t)
	cfg := &Config{
		Project: ProjectConfig{Name: "test", Repo: repoDir},
		Limits: LimitsConfig{
			MaxSessionCostUSD: 10.0,
			MaxSessionTokens:  50000,
		},
	}
	applyDefaults(cfg)
	if err := Validate(cfg); err == nil {
		t.Error("expected error for both session cost and tokens non-zero")
	}
}

func TestValidateAcceptsZeroBudgets(t *testing.T) {
	repoDir := setupTestRepo(t)
	cfg := &Config{
		Project: ProjectConfig{Name: "test", Repo: repoDir},
		Limits: LimitsConfig{
			TokenBudget: TokenBudget{
				WorkerUSD:    0,
				WorkerTokens: 0,
			},
		},
	}
	applyDefaults(cfg)
	if err := Validate(cfg); err != nil {
		t.Errorf("unexpected error for zero budgets: %v", err)
	}
}

func TestBudgetSpecTokenBased(t *testing.T) {
	tb := &TokenBudget{
		WorkerUSD:    0,
		WorkerTokens: 50000,
	}
	spec := tb.WorkerBudget()
	if spec.Unit != Tokens {
		t.Errorf("unit = %d, want Tokens", spec.Unit)
	}
	if spec.Value != 50000 {
		t.Errorf("value = %f, want 50000", spec.Value)
	}
}

func TestBudgetSpecUSD(t *testing.T) {
	tb := &TokenBudget{
		WorkerUSD:    1.50,
		WorkerTokens: 0,
	}
	spec := tb.WorkerBudget()
	if spec.Unit != USD {
		t.Errorf("unit = %d, want USD", spec.Unit)
	}
	if spec.Value != 1.50 {
		t.Errorf("value = %f, want 1.50", spec.Value)
	}
}

func TestValidateRejectsBadCommitFormatRegex(t *testing.T) {
	repoDir := setupTestRepo(t)
	cfg := &Config{
		Project: ProjectConfig{Name: "test", Repo: repoDir},
		Validation: ValidationConfig{
			CommitFormat: CommitFormatConfig{
				Pattern: "[invalid",
			},
		},
	}
	applyDefaults(cfg)
	if err := Validate(cfg); err == nil {
		t.Error("expected error for invalid commit_format.pattern regex")
	}
}

func replaceRepoPath(data []byte, newPath string) []byte {
	return []byte(
		replaceString(string(data), "/tmp/blueflame-test-repo", newPath),
	)
}

func replaceString(s, old, new string) string {
	// Simple replace for test fixtures
	result := ""
	for i := 0; i < len(s); {
		if i+len(old) <= len(s) && s[i:i+len(old)] == old {
			result += new
			i += len(old)
		} else {
			result += string(s[i])
			i++
		}
	}
	return result
}
