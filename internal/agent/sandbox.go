package agent

import (
	"fmt"
	"os/exec"
	"strings"
)

// shellQuote wraps a string in single quotes, escaping internal single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// wrapWithLimits rewrites cmd to run inside "bash -c 'ulimit ... && exec original-cmd'"
// while preserving the working directory, environment, and SysProcAttr.
func wrapWithLimits(cmd *exec.Cmd, limits []string) {
	if len(limits) == 0 {
		return
	}

	// Build the original command line
	var original strings.Builder
	for i, arg := range cmd.Args {
		if i > 0 {
			original.WriteString(" ")
		}
		original.WriteString(shellQuote(arg))
	}

	// Build the ulimit prefix
	ulimitPrefix := strings.Join(limits, " && ")
	script := fmt.Sprintf("%s && exec %s", ulimitPrefix, original.String())

	// Preserve state from the original cmd
	dir := cmd.Dir
	env := cmd.Env
	stdout := cmd.Stdout
	stderr := cmd.Stderr
	sysProcAttr := cmd.SysProcAttr

	// Replace the command
	*cmd = *exec.Command("bash", "-c", script)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.SysProcAttr = sysProcAttr
}
