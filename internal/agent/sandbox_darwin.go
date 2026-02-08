//go:build darwin

package agent

import (
	"fmt"
	"log"
	"os/exec"
	"syscall"

	"github.com/kylegalloway/blueflame/internal/config"
)

// applySandboxLimits applies platform-specific resource limits on macOS.
// macOS has limited sandboxing compared to Linux:
// - No reliable RSS limiting (ulimit -v crashes Node.js)
// - CPU time, file size, and open files work via ulimit wrapper
func applySandboxLimits(cmd *exec.Cmd, cfg config.SandboxConfig) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true

	var limits []string

	if cfg.MaxCPUSeconds > 0 {
		limits = append(limits, fmt.Sprintf("ulimit -t %d", cfg.MaxCPUSeconds))
	}
	if cfg.MaxFileSizeMB > 0 {
		// ulimit -f uses 512-byte blocks
		blocks := cfg.MaxFileSizeMB * 2048
		limits = append(limits, fmt.Sprintf("ulimit -f %d", blocks))
	}
	if cfg.MaxOpenFiles > 0 {
		limits = append(limits, fmt.Sprintf("ulimit -n %d", cfg.MaxOpenFiles))
	}

	// Memory: macOS has NO reliable RSS limiting mechanism.
	if cfg.MaxMemoryMB > 0 {
		log.Printf("macOS: memory limiting is best-effort only; relying on timeout and budget enforcement")
	}

	wrapWithLimits(cmd, limits)
}
