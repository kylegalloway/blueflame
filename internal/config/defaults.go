package config

import "time"

func applyDefaults(cfg *Config) {
	if cfg.SchemaVersion == 0 {
		cfg.SchemaVersion = 1
	}
	if cfg.Project.WorktreeDir == "" {
		cfg.Project.WorktreeDir = ".trees"
	}
	if cfg.Project.TasksFile == "" {
		cfg.Project.TasksFile = ".blueflame/tasks.yaml"
	}

	// Concurrency defaults
	if cfg.Concurrency.Planning == 0 {
		cfg.Concurrency.Planning = 1
	}
	if cfg.Concurrency.Development == 0 {
		cfg.Concurrency.Development = 4
	}
	if cfg.Concurrency.Validation == 0 {
		cfg.Concurrency.Validation = 2
	}
	if cfg.Concurrency.Merge == 0 {
		cfg.Concurrency.Merge = 1
	}
	if cfg.Concurrency.AdaptiveMinRAMPerAgentMB == 0 {
		cfg.Concurrency.AdaptiveMinRAMPerAgentMB = 600
	}

	// Limits defaults
	if cfg.Limits.AgentTimeout == 0 {
		cfg.Limits.AgentTimeout = 300 * time.Second
	}
	if cfg.Limits.HeartbeatInterval == 0 {
		cfg.Limits.HeartbeatInterval = 30 * time.Second
	}
	if cfg.Limits.MaxRetries == 0 {
		cfg.Limits.MaxRetries = 2
	}
	if cfg.Limits.MaxWaveCycles == 0 {
		cfg.Limits.MaxWaveCycles = 5
	}
	if cfg.Limits.TokenBudget.WarnThreshold == 0 {
		cfg.Limits.TokenBudget.WarnThreshold = 0.8
	}

	// Sandbox defaults
	if cfg.Sandbox.MaxCPUSeconds == 0 {
		cfg.Sandbox.MaxCPUSeconds = 600
	}
	if cfg.Sandbox.MaxMemoryMB == 0 {
		cfg.Sandbox.MaxMemoryMB = 2048
	}
	if cfg.Sandbox.MaxFileSizeMB == 0 {
		cfg.Sandbox.MaxFileSizeMB = 50
	}
	if cfg.Sandbox.MaxOpenFiles == 0 {
		cfg.Sandbox.MaxOpenFiles = 1024
	}

	// Model defaults
	if cfg.Models.Planner == "" {
		cfg.Models.Planner = "sonnet"
	}
	if cfg.Models.Worker == "" {
		cfg.Models.Worker = "sonnet"
	}
	if cfg.Models.Validator == "" {
		cfg.Models.Validator = "haiku"
	}
	if cfg.Models.Merger == "" {
		cfg.Models.Merger = "sonnet"
	}

	// Validation defaults
	if cfg.Validation.ValidatorDiagnostics.Timeout == 0 {
		cfg.Validation.ValidatorDiagnostics.Timeout = 120 * time.Second
	}
}
