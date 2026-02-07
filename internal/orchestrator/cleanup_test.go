package orchestrator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kylegalloway/blueflame/internal/agent"
	"github.com/kylegalloway/blueflame/internal/locks"
	"github.com/kylegalloway/blueflame/internal/state"
)

func TestCleanupStaleStateNoState(t *testing.T) {
	dir := t.TempDir()

	lm := agent.NewLifecycleManager(agent.LifecycleConfig{
		PersistPath: filepath.Join(dir, "agents.json"),
	})
	lockMgr := locks.NewManager(filepath.Join(dir, "locks"))
	stateMgr := state.NewManager(filepath.Join(dir, "state.json"))

	result, err := CleanupStaleState(lm, lockMgr, nil, stateMgr)
	if err != nil {
		t.Fatalf("CleanupStaleState: %v", err)
	}
	if result.OrphansKilled != 0 {
		t.Errorf("OrphansKilled = %d, want 0", result.OrphansKilled)
	}
	if result.RecoveryState != nil {
		t.Error("expected no recovery state")
	}
}

func TestCleanupWithRecoveryState(t *testing.T) {
	dir := t.TempDir()

	stateMgr := state.NewManager(filepath.Join(dir, "state.json"))
	stateMgr.Save(&state.OrchestratorState{
		SessionID: "ses-test",
		WaveCycle: 2,
		Phase:     "development",
	})

	result, err := CleanupStaleState(nil, nil, nil, stateMgr)
	if err != nil {
		t.Fatalf("CleanupStaleState: %v", err)
	}
	if result.RecoveryState == nil {
		t.Fatal("expected recovery state")
	}
	if result.RecoveryState.WaveCycle != 2 {
		t.Errorf("WaveCycle = %d, want 2", result.RecoveryState.WaveCycle)
	}
}

func TestCleanupWithStaleAgents(t *testing.T) {
	dir := t.TempDir()
	agentsPath := filepath.Join(dir, "agents.json")

	// Write a stale agents.json with a non-existent PID
	entries := []agent.AgentEntry{
		{
			ID:   "worker-stale",
			PID:  999999999, // non-existent PID
			PGID: 999999999,
			Role: "worker",
		},
	}
	data, _ := json.Marshal(entries)
	os.WriteFile(agentsPath, data, 0644)

	lm := agent.NewLifecycleManager(agent.LifecycleConfig{
		PersistPath: agentsPath,
	})

	result, err := CleanupStaleState(lm, nil, nil, nil)
	if err != nil {
		t.Fatalf("CleanupStaleState: %v", err)
	}
	// PID doesn't exist, so no orphans actually killed
	if result.OrphansKilled != 0 {
		t.Errorf("OrphansKilled = %d, want 0 (PID doesn't exist)", result.OrphansKilled)
	}
}

func TestCleanupWithNilManagers(t *testing.T) {
	// Should handle nil managers gracefully
	result, err := CleanupStaleState(nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("CleanupStaleState: %v", err)
	}
	if result.OrphansKilled != 0 {
		t.Errorf("OrphansKilled = %d, want 0", result.OrphansKilled)
	}
}

func TestFormatCleanupResult(t *testing.T) {
	tests := []struct {
		name   string
		result *CleanupResult
		want   string
	}{
		{
			name:   "nil result",
			result: nil,
			want:   "No cleanup needed",
		},
		{
			name:   "clean startup",
			result: &CleanupResult{},
			want:   "Clean startup",
		},
		{
			name:   "orphans killed",
			result: &CleanupResult{OrphansKilled: 2},
			want:   "Killed 2 orphan",
		},
		{
			name:   "recovery state",
			result: &CleanupResult{RecoveryState: &state.OrchestratorState{WaveCycle: 3, Phase: "validation"}},
			want:   "Recovery state available",
		},
		{
			name:   "stale worktrees",
			result: &CleanupResult{StaleWorktrees: []string{"/tmp/wt-1", "/tmp/wt-2"}},
			want:   "Found 2 stale worktree",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatCleanupResult(tt.result)
			if !strings.Contains(got, tt.want) {
				t.Errorf("FormatCleanupResult = %q, want to contain %q", got, tt.want)
			}
		})
	}
}
