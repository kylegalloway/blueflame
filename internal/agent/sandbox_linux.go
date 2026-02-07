//go:build linux

package agent

import (
	"os/exec"
	"syscall"

	"github.com/kylegalloway/blueflame/internal/config"
)

// applySandboxLimits applies platform-specific resource limits on Linux.
// Linux has full sandboxing support via cgroups v2, rlimits, and CLONE_NEWNET.
func applySandboxLimits(cmd *exec.Cmd, cfg config.SandboxConfig) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true

	// Network isolation via network namespace
	if !cfg.AllowNetwork {
		cmd.SysProcAttr.Cloneflags = syscall.CLONE_NEWNET
	}

	// Note: cgroups v2 memory limits and rlimits would be applied here.
	// Implementation deferred to Phase 3 full production hardening.
}
