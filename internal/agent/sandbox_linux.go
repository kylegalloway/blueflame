//go:build linux

package agent

import (
	"fmt"
	"os/exec"
	"syscall"

	"github.com/kylegalloway/blueflame/internal/config"
)

// applySandboxLimits applies platform-specific resource limits on Linux.
// Linux has full sandboxing support via rlimits, CLONE_NEWNET, and ulimit wrapper.
func applySandboxLimits(cmd *exec.Cmd, cfg config.SandboxConfig) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true

	// Network isolation via network namespace
	if !cfg.AllowNetwork {
		cmd.SysProcAttr.Cloneflags = syscall.CLONE_NEWNET
	}

	var limits []string

	if cfg.MaxCPUSeconds > 0 {
		limits = append(limits, fmt.Sprintf("ulimit -t %d", cfg.MaxCPUSeconds))
	}
	if cfg.MaxFileSizeMB > 0 {
		blocks := cfg.MaxFileSizeMB * 2048
		limits = append(limits, fmt.Sprintf("ulimit -f %d", blocks))
	}
	if cfg.MaxOpenFiles > 0 {
		limits = append(limits, fmt.Sprintf("ulimit -n %d", cfg.MaxOpenFiles))
	}
	if cfg.MaxMemoryMB > 0 {
		// RLIMIT_AS via ulimit -v (kilobytes)
		kb := cfg.MaxMemoryMB * 1024
		limits = append(limits, fmt.Sprintf("ulimit -v %d", kb))
	}

	wrapWithLimits(cmd, limits)
}
