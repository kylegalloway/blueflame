package agent

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/kylegalloway/blueflame/internal/config"
)

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "'hello'"},
		{"hello world", "'hello world'"},
		{"it's", "'it'\"'\"'s'"},
		{"", "''"},
	}
	for _, tt := range tests {
		got := shellQuote(tt.input)
		if got != tt.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestWrapWithLimits(t *testing.T) {
	cmd := exec.Command("claude", "--print", "--model", "sonnet")
	cmd.Dir = "/tmp/test"

	limits := []string{"ulimit -t 300", "ulimit -n 1024"}
	wrapWithLimits(cmd, limits)

	if cmd.Args[0] != "bash" {
		t.Errorf("Args[0] = %q, want bash", cmd.Args[0])
	}
	if cmd.Args[1] != "-c" {
		t.Errorf("Args[1] = %q, want -c", cmd.Args[1])
	}
	script := cmd.Args[2]
	if !strings.Contains(script, "ulimit -t 300") {
		t.Errorf("script missing ulimit -t: %s", script)
	}
	if !strings.Contains(script, "ulimit -n 1024") {
		t.Errorf("script missing ulimit -n: %s", script)
	}
	if !strings.Contains(script, "exec") {
		t.Errorf("script missing exec: %s", script)
	}
	if !strings.Contains(script, "'claude'") {
		t.Errorf("script missing original command: %s", script)
	}
	if cmd.Dir != "/tmp/test" {
		t.Errorf("Dir = %q, want /tmp/test", cmd.Dir)
	}
}

func TestWrapWithLimitsEmpty(t *testing.T) {
	cmd := exec.Command("claude", "--print")
	original := cmd.Args[0]
	wrapWithLimits(cmd, nil)
	if cmd.Args[0] != original {
		t.Errorf("empty limits should not modify cmd, got Args[0]=%q", cmd.Args[0])
	}
}

func TestApplySandboxLimitsNoLimits(t *testing.T) {
	cmd := exec.Command("claude", "--print")
	applySandboxLimits(cmd, config.SandboxConfig{})

	// With no limits configured, command should still be "claude" (no bash wrapper)
	if cmd.Args[0] != "claude" {
		t.Errorf("no-limits should keep original command, got Args[0]=%q", cmd.Args[0])
	}
	// But SysProcAttr.Setpgid should be set
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Error("Setpgid should be true even with no limits")
	}
}

func TestApplySandboxLimitsWithRlimits(t *testing.T) {
	cmd := exec.Command("claude", "--print")
	applySandboxLimits(cmd, config.SandboxConfig{
		MaxCPUSeconds: 300,
		MaxOpenFiles:  1024,
		MaxFileSizeMB: 100,
	})

	// Should be wrapped in bash
	if cmd.Args[0] != "bash" {
		t.Errorf("with limits should wrap in bash, got Args[0]=%q", cmd.Args[0])
	}
	script := cmd.Args[2]
	if !strings.Contains(script, "ulimit -t 300") {
		t.Errorf("missing CPU limit in script: %s", script)
	}
	if !strings.Contains(script, "ulimit -n 1024") {
		t.Errorf("missing open files limit in script: %s", script)
	}
	if !strings.Contains(script, "ulimit -f") {
		t.Errorf("missing file size limit in script: %s", script)
	}
}
