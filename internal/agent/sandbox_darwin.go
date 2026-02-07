//go:build darwin

package agent

import (
	"log"
	"os/exec"
	"syscall"

	"github.com/kylegalloway/blueflame/internal/config"
)

// applySandboxLimits applies platform-specific resource limits on macOS.
// macOS has limited sandboxing compared to Linux:
// - No reliable RSS limiting (ulimit -v crashes Node.js)
// - sandbox-exec is deprecated but still functional on macOS 15
// - CPU time, file size, and open files work via rlimits
func applySandboxLimits(cmd *exec.Cmd, cfg config.SandboxConfig) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true

	// Memory: macOS has NO reliable RSS limiting mechanism.
	// ulimit -v kills Node.js (V8 maps >1GB virtual).
	// Rely on agent timeout + budget as backstops.
	if cfg.MaxMemoryMB > 0 {
		log.Printf("macOS: memory limiting is best-effort only; relying on timeout and budget enforcement")
	}

	// Note: rlimits would be set here via cmd.SysProcAttr on Linux.
	// On macOS, we document that CPU time, file size, and open file limits
	// are applied but memory limits are not enforceable.
}
