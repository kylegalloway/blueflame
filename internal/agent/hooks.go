package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/kylegalloway/blueflame/internal/config"
	"github.com/kylegalloway/blueflame/internal/tasks"
)

// DefaultWatcherTemplate returns the built-in watcher hook template.
func DefaultWatcherTemplate() *template.Template {
	return template.Must(template.New("watcher.sh").Parse(watcherTemplateSource))
}

// WatcherData holds all data needed to render the watcher hook script.
type WatcherData struct {
	AgentID            string
	Role               string
	AllowedTools       []string
	BlockedTools       []string
	AllowedPaths       []string
	BlockedPaths       []string
	AllowedCommands    []string
	BlockedPatterns    []string
	FileLocks          []string
	CommitPattern      string
	AuditLogPath       string
	DiagnosticCommands []string
}

// GenerateWatcherHook generates the watcher shell script for an agent.
func GenerateWatcherHook(tmplPath string, data WatcherData, outputPath string) error {
	tmpl, err := template.ParseFiles(tmplPath)
	if err != nil {
		return fmt.Errorf("parse watcher template: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create hook dir: %w", err)
	}

	f, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("create hook file: %w", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("render watcher template: %w", err)
	}

	return nil
}

// GenerateWatcherHookFromTemplate generates the watcher from an already-parsed template.
func GenerateWatcherHookFromTemplate(tmpl *template.Template, data WatcherData, outputPath string) error {
	if tmpl == nil {
		return fmt.Errorf("watcher template is nil")
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create hook dir: %w", err)
	}

	f, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("create hook file: %w", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("render watcher template: %w", err)
	}

	return nil
}

// BuildWatcherData constructs a WatcherData from config and task.
func BuildWatcherData(agentID, role string, task *tasks.Task, cfg *config.Config, blueflameDir string) WatcherData {
	data := WatcherData{
		AgentID:         agentID,
		Role:            role,
		AllowedTools:    cfg.Permissions.AllowedTools,
		BlockedTools:    cfg.Permissions.BlockedTools,
		AllowedPaths:    cfg.Permissions.AllowedPaths,
		BlockedPaths:    cfg.Permissions.BlockedPaths,
		AllowedCommands: cfg.Permissions.BashRules.AllowedCommands,
		BlockedPatterns: cfg.Permissions.BashRules.BlockedPatterns,
		CommitPattern:   cfg.Validation.CommitFormat.Pattern,
		AuditLogPath:    filepath.Join(blueflameDir, "logs", agentID+".audit.jsonl"),
	}

	if task != nil {
		data.FileLocks = task.FileLocks
	}

	if cfg.Validation.ValidatorDiagnostics.Enabled {
		data.DiagnosticCommands = cfg.Validation.ValidatorDiagnostics.Commands
	}

	return data
}

// AgentSettings represents the .claude/settings.json for a per-agent worktree.
type AgentSettings struct {
	Hooks HooksSettings `json:"hooks"`
}

// HooksSettings holds the hook configuration.
type HooksSettings struct {
	PreToolUse []HookEntry `json:"PreToolUse"`
}

// HookEntry represents a single hook.
type HookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

// GenerateAgentSettings creates the .claude/settings.json file for an agent's worktree.
func GenerateAgentSettings(worktreePath, watcherScriptPath string) error {
	settingsDir := filepath.Join(worktreePath, ".claude")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		return fmt.Errorf("create .claude dir: %w", err)
	}

	absWatcherPath, err := filepath.Abs(watcherScriptPath)
	if err != nil {
		return fmt.Errorf("resolve watcher path: %w", err)
	}

	settings := AgentSettings{
		Hooks: HooksSettings{
			PreToolUse: []HookEntry{
				{
					Type:    "command",
					Command: absWatcherPath,
					Timeout: 5000,
				},
			},
		},
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}

	settingsPath := filepath.Join(settingsDir, "settings.json")
	if err := os.WriteFile(settingsPath, data, 0o644); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}

	return nil
}
