package orchestrator

import (
	"fmt"
	"log"
	"syscall"

	"github.com/kylegalloway/blueflame/internal/agent"
	"github.com/kylegalloway/blueflame/internal/locks"
	"github.com/kylegalloway/blueflame/internal/state"
	"github.com/kylegalloway/blueflame/internal/worktree"
)

// CleanupResult reports what was cleaned up during startup.
type CleanupResult struct {
	OrphansKilled     int
	StaleLocksCleaned int
	StaleWorktrees    []string
	RecoveryState     *state.OrchestratorState
}

// CleanupStaleState performs startup cleanup: kills orphan processes,
// cleans stale locks, detects stale worktrees, and checks for crash recovery state.
func CleanupStaleState(
	lifecycle *agent.LifecycleManager,
	lockMgr *locks.Manager,
	wtMgr *worktree.Manager,
	stateMgr *state.Manager,
) (*CleanupResult, error) {
	result := &CleanupResult{}

	// 1. Kill orphan agent processes from previous session
	if lifecycle != nil {
		staleAgents, err := lifecycle.LoadStaleAgents()
		if err != nil {
			log.Printf("Warning: could not load stale agents: %v", err)
		} else {
			for _, a := range staleAgents {
				if agent.ProcessAlive(a.PID) {
					// Kill via process group
					_ = syscall.Kill(-a.PGID, syscall.SIGKILL)
					_ = syscall.Kill(a.PID, syscall.SIGKILL)
					log.Printf("Killed orphan agent %s (PID %d)", a.ID, a.PID)
					result.OrphansKilled++
				}
			}
		}
	}

	// 2. Clean stale locks (lock holder PID is dead)
	if lockMgr != nil {
		if err := lockMgr.CleanStale(); err != nil {
			log.Printf("Warning: stale lock cleanup: %v", err)
		}
	}

	// 3. Find stale worktrees
	if wtMgr != nil {
		stale, err := wtMgr.FindStale()
		if err != nil {
			log.Printf("Warning: stale worktree detection: %v", err)
		} else if len(stale) > 0 {
			result.StaleWorktrees = stale
			log.Printf("Found %d stale worktrees from previous session", len(stale))
		}
	}

	// 4. Check for crash recovery state
	if stateMgr != nil && stateMgr.Exists() {
		recoveryState, err := stateMgr.Load()
		if err != nil {
			log.Printf("Warning: could not load recovery state: %v", err)
		} else {
			result.RecoveryState = recoveryState
			log.Printf("Found recovery state from wave cycle %d, phase %q",
				recoveryState.WaveCycle, recoveryState.Phase)
		}
	}

	return result, nil
}

// FormatCleanupResult returns a human-readable summary of cleanup actions.
func FormatCleanupResult(r *CleanupResult) string {
	if r == nil {
		return "No cleanup needed"
	}

	msg := ""
	if r.OrphansKilled > 0 {
		msg += fmt.Sprintf("Killed %d orphan agent(s). ", r.OrphansKilled)
	}
	if len(r.StaleWorktrees) > 0 {
		msg += fmt.Sprintf("Found %d stale worktree(s). ", len(r.StaleWorktrees))
	}
	if r.RecoveryState != nil {
		msg += fmt.Sprintf("Recovery state available (wave %d, phase %q).",
			r.RecoveryState.WaveCycle, r.RecoveryState.Phase)
	}
	if msg == "" {
		msg = "Clean startup, no stale state found."
	}
	return msg
}
