package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// Config represents the full blueflame.yaml configuration.
type Config struct {
	SchemaVersion int               `yaml:"schema_version"`
	Project       ProjectConfig     `yaml:"project"`
	Concurrency   ConcurrencyConfig `yaml:"concurrency"`
	Limits        LimitsConfig      `yaml:"limits"`
	Sandbox       SandboxConfig     `yaml:"sandbox"`
	Planning      PlanningConfig    `yaml:"planning"`
	Models        ModelsConfig      `yaml:"models"`
	Permissions   PermissionsConfig `yaml:"permissions"`
	Validation    ValidationConfig  `yaml:"validation"`
	Superpowers   SuperpowersConfig `yaml:"superpowers"`
	Beads         BeadsConfig       `yaml:"beads"`
	Hooks         HooksConfig       `yaml:"hooks"`
}

type ProjectConfig struct {
	Name        string `yaml:"name"`
	Repo        string `yaml:"repo"`
	BaseBranch  string `yaml:"base_branch"`
	WorktreeDir string `yaml:"worktree_dir"`
	TasksFile   string `yaml:"tasks_file"`
}

type ConcurrencyConfig struct {
	Planning               int  `yaml:"planning"`
	Development            int  `yaml:"development"`
	Validation             int  `yaml:"validation"`
	Merge                  int  `yaml:"merge"`
	Adaptive               bool `yaml:"adaptive"`
	AdaptiveMinRAMPerAgentMB int `yaml:"adaptive_min_ram_per_agent_mb"`
}

type LimitsConfig struct {
	AgentTimeout      time.Duration `yaml:"agent_timeout"`
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval"`
	MaxRetries        int           `yaml:"max_retries"`
	MaxWaveCycles     int           `yaml:"max_wave_cycles"`
	MaxSessionCostUSD float64       `yaml:"max_session_cost_usd"`
	MaxSessionTokens  int           `yaml:"max_session_tokens"`
	TokenBudget       TokenBudget   `yaml:"token_budget"`
}

type TokenBudget struct {
	PlannerUSD     float64 `yaml:"planner_usd"`
	PlannerTokens  int     `yaml:"planner_tokens"`
	WorkerUSD      float64 `yaml:"worker_usd"`
	WorkerTokens   int     `yaml:"worker_tokens"`
	ValidatorUSD   float64 `yaml:"validator_usd"`
	ValidatorTokens int    `yaml:"validator_tokens"`
	MergerUSD      float64 `yaml:"merger_usd"`
	MergerTokens   int     `yaml:"merger_tokens"`
	WarnThreshold  float64 `yaml:"warn_threshold"`
}

// BudgetUnit distinguishes USD from token budgets.
type BudgetUnit int

const (
	USD    BudgetUnit = iota
	Tokens
)

// BudgetSpec represents a budget with its unit.
type BudgetSpec struct {
	Unit  BudgetUnit
	Value float64
}

func (tb *TokenBudget) PlannerBudget() BudgetSpec {
	return budgetFor(tb.PlannerUSD, tb.PlannerTokens)
}

func (tb *TokenBudget) WorkerBudget() BudgetSpec {
	return budgetFor(tb.WorkerUSD, tb.WorkerTokens)
}

func (tb *TokenBudget) ValidatorBudget() BudgetSpec {
	return budgetFor(tb.ValidatorUSD, tb.ValidatorTokens)
}

func (tb *TokenBudget) MergerBudget() BudgetSpec {
	return budgetFor(tb.MergerUSD, tb.MergerTokens)
}

func budgetFor(usd float64, tokens int) BudgetSpec {
	if tokens > 0 {
		return BudgetSpec{Unit: Tokens, Value: float64(tokens)}
	}
	return BudgetSpec{Unit: USD, Value: usd}
}

type SandboxConfig struct {
	MaxCPUSeconds int  `yaml:"max_cpu_seconds"`
	MaxMemoryMB   int  `yaml:"max_memory_mb"`
	MaxFileSizeMB int  `yaml:"max_file_size_mb"`
	MaxOpenFiles  int  `yaml:"max_open_files"`
	AllowNetwork  bool `yaml:"allow_network"`
}

type PlanningConfig struct {
	Interactive bool `yaml:"interactive"`
}

type ModelsConfig struct {
	Planner   string `yaml:"planner"`
	Worker    string `yaml:"worker"`
	Validator string `yaml:"validator"`
	Merger    string `yaml:"merger"`
}

type PermissionsConfig struct {
	AllowedPaths []string  `yaml:"allowed_paths"`
	BlockedPaths []string  `yaml:"blocked_paths"`
	AllowedTools []string  `yaml:"allowed_tools"`
	BlockedTools []string  `yaml:"blocked_tools"`
	BashRules    BashRules `yaml:"bash_rules"`
}

type BashRules struct {
	AllowedCommands []string `yaml:"allowed_commands"`
	BlockedPatterns []string `yaml:"blocked_patterns"`
}

type ValidationConfig struct {
	CommitFormat          CommitFormatConfig          `yaml:"commit_format"`
	FileNaming            FileNamingConfig            `yaml:"file_naming"`
	RequireTests          RequireTestsConfig          `yaml:"require_tests"`
	FileScope             FileScopeConfig             `yaml:"file_scope"`
	ValidatorDiagnostics  ValidatorDiagnosticsConfig  `yaml:"validator_diagnostics"`
}

type CommitFormatConfig struct {
	Pattern string `yaml:"pattern"`
	Example string `yaml:"example"`
}

type FileNamingConfig struct {
	Style      string   `yaml:"style"`
	EnforceFor []string `yaml:"enforce_for"`
}

type RequireTestsConfig struct {
	Enabled        bool     `yaml:"enabled"`
	SourcePatterns []string `yaml:"source_patterns"`
	TestPatterns   []string `yaml:"test_patterns"`
}

type FileScopeConfig struct {
	Enforce bool `yaml:"enforce"`
}

type ValidatorDiagnosticsConfig struct {
	Enabled  bool          `yaml:"enabled"`
	Commands []string      `yaml:"commands"`
	Timeout  time.Duration `yaml:"timeout"`
}

type SuperpowersConfig struct {
	Enabled bool     `yaml:"enabled"`
	Skills  []string `yaml:"skills"`
}

type BeadsConfig struct {
	Enabled             bool        `yaml:"enabled"`
	ArchiveAfterWave    bool        `yaml:"archive_after_wave"`
	IncludeFailureNotes bool        `yaml:"include_failure_notes"`
	MemoryDecay         bool        `yaml:"memory_decay"`
	DecayPolicy         DecayPolicy `yaml:"decay_policy"`
}

type DecayPolicy struct {
	SummarizeAfterSessions  int `yaml:"summarize_after_sessions"`
	PreserveFailuresSessions int `yaml:"preserve_failures_sessions"`
}

type HooksConfig struct {
	PostPlan      string `yaml:"post_plan"`
	PreValidation string `yaml:"pre_validation"`
	PostMerge     string `yaml:"post_merge"`
	OnFailure     string `yaml:"on_failure"`
}

// Load reads and parses a blueflame.yaml file, applying defaults and validation.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return Parse(data)
}

// Parse parses raw YAML bytes into a validated Config.
func Parse(data []byte) (*Config, error) {
	cfg, err := Migrate(data)
	if err != nil {
		return nil, err
	}

	applyDefaults(cfg)

	if err := Validate(cfg); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return cfg, nil
}

// Validate checks a Config for logical errors.
func Validate(cfg *Config) error {
	if cfg.Project.Name == "" {
		return fmt.Errorf("project.name is required")
	}
	if cfg.Project.Repo == "" {
		return fmt.Errorf("project.repo is required")
	}

	// Validate repo is a directory (git check done at runtime)
	if info, err := os.Stat(cfg.Project.Repo); err != nil || !info.IsDir() {
		if err != nil {
			return fmt.Errorf("project.repo %q: %w", cfg.Project.Repo, err)
		}
		return fmt.Errorf("project.repo %q is not a directory", cfg.Project.Repo)
	}

	if cfg.Concurrency.Development < 1 || cfg.Concurrency.Development > 8 {
		return fmt.Errorf("concurrency.development must be 1-8, got %d", cfg.Concurrency.Development)
	}

	if cfg.Limits.MaxWaveCycles < 1 {
		return fmt.Errorf("limits.max_wave_cycles must be >= 1, got %d", cfg.Limits.MaxWaveCycles)
	}

	// Validate that at most one of session cost/token limits is non-zero
	if cfg.Limits.MaxSessionCostUSD > 0 && cfg.Limits.MaxSessionTokens > 0 {
		return fmt.Errorf("at most one of limits.max_session_cost_usd or limits.max_session_tokens may be non-zero")
	}

	// Validate per-role budget: at most one of _usd or _tokens non-zero
	tb := cfg.Limits.TokenBudget
	if err := validateRoleBudget("planner", tb.PlannerUSD, tb.PlannerTokens); err != nil {
		return err
	}
	if err := validateRoleBudget("worker", tb.WorkerUSD, tb.WorkerTokens); err != nil {
		return err
	}
	if err := validateRoleBudget("validator", tb.ValidatorUSD, tb.ValidatorTokens); err != nil {
		return err
	}
	if err := validateRoleBudget("merger", tb.MergerUSD, tb.MergerTokens); err != nil {
		return err
	}

	// Validate glob patterns
	for _, p := range cfg.Permissions.AllowedPaths {
		if _, err := filepath.Match(p, ""); err != nil {
			return fmt.Errorf("invalid allowed_paths glob %q: %w", p, err)
		}
	}
	for _, p := range cfg.Permissions.BlockedPaths {
		if _, err := filepath.Match(p, ""); err != nil {
			return fmt.Errorf("invalid blocked_paths glob %q: %w", p, err)
		}
	}

	// Validate regex patterns
	for _, p := range cfg.Permissions.BashRules.BlockedPatterns {
		if _, err := regexp.Compile(p); err != nil {
			return fmt.Errorf("invalid blocked_patterns regex %q: %w", p, err)
		}
	}

	if cfg.Validation.CommitFormat.Pattern != "" {
		if _, err := regexp.Compile(cfg.Validation.CommitFormat.Pattern); err != nil {
			return fmt.Errorf("invalid commit_format.pattern regex %q: %w", cfg.Validation.CommitFormat.Pattern, err)
		}
	}

	return nil
}

func validateRoleBudget(role string, usd float64, tokens int) error {
	if usd > 0 && tokens > 0 {
		return fmt.Errorf("token_budget.%s: at most one of _usd or _tokens may be non-zero", role)
	}
	return nil
}
