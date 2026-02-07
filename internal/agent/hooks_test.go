package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/kylegalloway/blueflame/internal/config"
	"github.com/kylegalloway/blueflame/internal/tasks"
)

func TestGenerateWatcherHook(t *testing.T) {
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "watcher.sh.tmpl")

	// Use the actual template
	srcTmpl, err := os.ReadFile("../../templates/watcher.sh.tmpl")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	os.WriteFile(tmplPath, srcTmpl, 0o644)

	data := WatcherData{
		AgentID:         "worker-abc12345",
		Role:            "worker",
		AllowedTools:    []string{"Read", "Write", "Edit", "Bash"},
		BlockedTools:    []string{"WebFetch", "Task"},
		AllowedPaths:    []string{"src/**"},
		BlockedPaths:    []string{".env*"},
		AllowedCommands: []string{"go test", "go build"},
		BlockedPatterns: []string{"rm\\s+-rf", "git\\s+push"},
		FileLocks:       []string{"pkg/middleware/"},
		AuditLogPath:    filepath.Join(dir, "audit.jsonl"),
	}

	outputPath := filepath.Join(dir, "hooks", "watcher.sh")
	err = GenerateWatcherHook(tmplPath, data, outputPath)
	if err != nil {
		t.Fatalf("GenerateWatcherHook: %v", err)
	}

	// Verify file exists and is executable
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("stat hook: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Error("hook should be executable")
	}

	// Verify content contains key fields
	content, _ := os.ReadFile(outputPath)
	script := string(content)

	checks := []string{
		"worker-abc12345",  // agent ID
		"WebFetch",         // blocked tool
		"go test",          // allowed command
		"rm\\s+-rf",        // blocked pattern
		"pkg/middleware/",   // file lock
	}
	for _, check := range checks {
		if !strings.Contains(script, check) {
			t.Errorf("generated script missing %q", check)
		}
	}
}

func TestGenerateWatcherHookFromTemplate(t *testing.T) {
	tmplContent := `#!/bin/bash
# Agent: {{.AgentID}}
# Role: {{.Role}}
echo "ok"
`
	tmpl, err := template.New("test").Parse(tmplContent)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}

	dir := t.TempDir()
	outputPath := filepath.Join(dir, "watcher.sh")

	data := WatcherData{AgentID: "test-agent", Role: "worker"}
	err = GenerateWatcherHookFromTemplate(tmpl, data, outputPath)
	if err != nil {
		t.Fatalf("GenerateWatcherHookFromTemplate: %v", err)
	}

	content, _ := os.ReadFile(outputPath)
	if !strings.Contains(string(content), "test-agent") {
		t.Error("template not rendered correctly")
	}
}

func TestBuildWatcherData(t *testing.T) {
	cfg := &config.Config{
		Permissions: config.PermissionsConfig{
			AllowedTools:    []string{"Read", "Write"},
			BlockedTools:    []string{"WebFetch"},
			AllowedPaths:    []string{"src/**"},
			BlockedPaths:    []string{".env*"},
			BashRules: config.BashRules{
				AllowedCommands: []string{"go test"},
				BlockedPatterns: []string{"rm -rf"},
			},
		},
		Validation: config.ValidationConfig{
			CommitFormat: config.CommitFormatConfig{Pattern: "^feat.*"},
			ValidatorDiagnostics: config.ValidatorDiagnosticsConfig{
				Enabled:  true,
				Commands: []string{"go test ./...", "go vet ./..."},
			},
		},
	}

	task := &tasks.Task{
		FileLocks: []string{"pkg/auth/"},
	}

	data := BuildWatcherData("worker-001", "worker", task, cfg, "/project/.blueflame")

	if data.AgentID != "worker-001" {
		t.Errorf("AgentID = %q", data.AgentID)
	}
	if data.Role != "worker" {
		t.Errorf("Role = %q", data.Role)
	}
	if len(data.FileLocks) != 1 || data.FileLocks[0] != "pkg/auth/" {
		t.Errorf("FileLocks = %v", data.FileLocks)
	}
	if len(data.DiagnosticCommands) != 2 {
		t.Errorf("DiagnosticCommands = %v", data.DiagnosticCommands)
	}
	if !strings.Contains(data.AuditLogPath, "worker-001.audit.jsonl") {
		t.Errorf("AuditLogPath = %q", data.AuditLogPath)
	}
}

func TestGenerateAgentSettings(t *testing.T) {
	dir := t.TempDir()
	wtPath := filepath.Join(dir, "worktree")
	os.MkdirAll(wtPath, 0o755)

	watcherPath := filepath.Join(dir, "watcher.sh")
	os.WriteFile(watcherPath, []byte("#!/bin/bash\necho ok"), 0o755)

	err := GenerateAgentSettings(wtPath, watcherPath)
	if err != nil {
		t.Fatalf("GenerateAgentSettings: %v", err)
	}

	settingsPath := filepath.Join(wtPath, ".claude", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "PreToolUse") {
		t.Error("settings missing PreToolUse hook")
	}
	if !strings.Contains(content, "watcher.sh") {
		t.Error("settings missing watcher script path")
	}
}
