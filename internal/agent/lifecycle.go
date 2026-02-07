package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/kylegalloway/blueflame/internal/config"
)

// AgentEntry represents a tracked agent process.
type AgentEntry struct {
	ID           string            `json:"id"`
	PID          int               `json:"pid"`
	PGID         int               `json:"pgid"`
	Role         string            `json:"role"`
	WorktreePath string            `json:"worktree"`
	TaskID       string            `json:"task_id"`
	StartTime    time.Time         `json:"start_time"`
	Status       string            `json:"status"` // "running", "completed", "failed", "killed"
	CostUSD      float64           `json:"cost_usd"`
	TokensUsed   int               `json:"tokens_used"`
	Budget       config.BudgetSpec `json:"budget"`
}

// LifecycleManager tracks running agent processes with heartbeat monitoring.
type LifecycleManager struct {
	mu                sync.Mutex
	agents            map[string]*AgentEntry
	persistPath       string
	heartbeatInterval time.Duration
	agentTimeout      time.Duration
	stallThreshold    time.Duration
	auditDir          string

	// onAgentDeath is called when an agent is detected as dead.
	// Used for testing; nil means log-only.
	onAgentDeath func(entry *AgentEntry)
}

// LifecycleConfig holds configuration for the lifecycle manager.
type LifecycleConfig struct {
	PersistPath       string
	HeartbeatInterval time.Duration
	AgentTimeout      time.Duration
	StallThreshold    time.Duration
	AuditDir          string
}

// NewLifecycleManager creates a new lifecycle manager.
func NewLifecycleManager(cfg LifecycleConfig) *LifecycleManager {
	if cfg.HeartbeatInterval == 0 {
		cfg.HeartbeatInterval = 30 * time.Second
	}
	if cfg.AgentTimeout == 0 {
		cfg.AgentTimeout = 300 * time.Second
	}
	if cfg.StallThreshold == 0 {
		cfg.StallThreshold = 120 * time.Second
	}
	return &LifecycleManager{
		agents:            make(map[string]*AgentEntry),
		persistPath:       cfg.PersistPath,
		heartbeatInterval: cfg.HeartbeatInterval,
		agentTimeout:      cfg.AgentTimeout,
		stallThreshold:    cfg.StallThreshold,
		auditDir:          cfg.AuditDir,
	}
}

// Register adds an agent to the lifecycle tracker.
func (lm *LifecycleManager) Register(a *Agent) error {
	if a.Cmd == nil || a.Cmd.Process == nil {
		return fmt.Errorf("agent %s has no running process", a.ID)
	}

	pid := a.Cmd.Process.Pid
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		pgid = pid // fallback to pid if pgid lookup fails
	}

	entry := &AgentEntry{
		ID:        a.ID,
		PID:       pid,
		PGID:      pgid,
		Role:      a.Role,
		TaskID:    "",
		StartTime: a.Started,
		Status:    "running",
		Budget:    a.Budget,
	}
	if a.Task != nil {
		entry.TaskID = a.Task.ID
		entry.WorktreePath = a.Task.Worktree
	}

	lm.mu.Lock()
	lm.agents[a.ID] = entry
	lm.mu.Unlock()

	lm.persist()
	return nil
}

// Unregister marks an agent as completed and removes it from tracking.
func (lm *LifecycleManager) Unregister(agentID string, result AgentResult) {
	lm.mu.Lock()
	entry, ok := lm.agents[agentID]
	if ok {
		if result.ExitCode == 0 {
			entry.Status = "completed"
		} else {
			entry.Status = "failed"
		}
		entry.CostUSD = result.CostUSD
		entry.TokensUsed = result.TokensUsed
		delete(lm.agents, agentID)
	}
	lm.mu.Unlock()

	lm.persist()
}

// RunningAgents returns a snapshot of all currently tracked agents.
func (lm *LifecycleManager) RunningAgents() []AgentEntry {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	entries := make([]AgentEntry, 0, len(lm.agents))
	for _, e := range lm.agents {
		entries = append(entries, *e)
	}
	return entries
}

// RunningCount returns the number of currently tracked agents.
func (lm *LifecycleManager) RunningCount() int {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	return len(lm.agents)
}

// MonitorLoop runs the heartbeat/liveness check loop until ctx is cancelled.
func (lm *LifecycleManager) MonitorLoop(ctx context.Context) {
	ticker := time.NewTicker(lm.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			lm.checkAgents()
		case <-ctx.Done():
			return
		}
	}
}

func (lm *LifecycleManager) checkAgents() {
	lm.mu.Lock()
	var dead []*AgentEntry
	var timedOut []*AgentEntry
	var stalled []*AgentEntry

	for _, entry := range lm.agents {
		// Liveness check: signal 0 tests if process exists
		if err := syscall.Kill(entry.PID, 0); err != nil {
			dead = append(dead, entry)
			continue
		}
		// Timeout check
		if time.Since(entry.StartTime) > lm.agentTimeout {
			timedOut = append(timedOut, entry)
			continue
		}
		// Stall detection via audit log last-modified time
		if lm.isStalled(entry) {
			stalled = append(stalled, entry)
		}
	}
	lm.mu.Unlock()

	// Handle dead agents
	for _, entry := range dead {
		lm.handleAgentDeath(entry)
	}

	// Kill timed out agents
	for _, entry := range timedOut {
		log.Printf("Agent %s timed out after %v, killing", entry.ID, lm.agentTimeout)
		lm.KillAgent(entry.ID, "timeout")
	}

	// Warn about stalled agents (don't kill, just log)
	for _, entry := range stalled {
		log.Printf("Agent %s appears stalled (no activity for %v)", entry.ID, lm.stallThreshold)
	}
}

// isStalled checks if an agent's audit log hasn't been modified recently.
// Must be called with lm.mu held.
func (lm *LifecycleManager) isStalled(entry *AgentEntry) bool {
	if lm.auditDir == "" {
		return false
	}
	auditPath := filepath.Join(lm.auditDir, entry.ID+".jsonl")
	info, err := os.Stat(auditPath)
	if err != nil {
		return false // no audit log = can't determine stall
	}
	return time.Since(info.ModTime()) > lm.stallThreshold
}

func (lm *LifecycleManager) handleAgentDeath(entry *AgentEntry) {
	lm.mu.Lock()
	entry.Status = "failed"
	delete(lm.agents, entry.ID)
	lm.mu.Unlock()

	log.Printf("Agent %s (PID %d) died unexpectedly", entry.ID, entry.PID)
	lm.persist()

	if lm.onAgentDeath != nil {
		lm.onAgentDeath(entry)
	}
}

// KillAgent sends SIGTERM to an agent's process group, waits briefly, then SIGKILL.
func (lm *LifecycleManager) KillAgent(agentID string, reason string) error {
	lm.mu.Lock()
	entry, ok := lm.agents[agentID]
	if !ok {
		lm.mu.Unlock()
		return fmt.Errorf("agent %s not found", agentID)
	}
	pgid := entry.PGID
	entry.Status = "killed"
	delete(lm.agents, agentID)
	lm.mu.Unlock()

	pid := entry.PID
	log.Printf("Killing agent %s (PID %d, PGID %d): %s", agentID, pid, pgid, reason)

	// SIGTERM to process group and individual PID
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	_ = syscall.Kill(pid, syscall.SIGTERM)

	// Give process 5 seconds to exit gracefully
	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			if err := syscall.Kill(pid, 0); err != nil {
				close(done)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		// Force kill
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_ = syscall.Kill(pid, syscall.SIGKILL)
		log.Printf("Sent SIGKILL to agent %s (PID %d)", agentID, pid)
	}

	lm.persist()
	return nil
}

// GracefulShutdown terminates all running agents: SIGTERM, wait, SIGKILL.
func (lm *LifecycleManager) GracefulShutdown(timeout time.Duration) {
	agents := lm.RunningAgents()
	if len(agents) == 0 {
		return
	}

	log.Printf("Graceful shutdown: terminating %d agents", len(agents))

	// Phase 1: SIGTERM to all process groups and individual PIDs
	for _, a := range agents {
		_ = syscall.Kill(-a.PGID, syscall.SIGTERM)
		_ = syscall.Kill(a.PID, syscall.SIGTERM)
	}

	// Phase 2: Wait for graceful exit
	deadline := time.After(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			goto forceKill
		case <-ticker.C:
			allDead := true
			for _, a := range agents {
				if err := syscall.Kill(a.PID, 0); err == nil {
					allDead = false
					break
				}
			}
			if allDead {
				log.Printf("All agents exited gracefully")
				lm.clearAll()
				return
			}
		}
	}

forceKill:
	// Phase 3: SIGKILL survivors
	for _, a := range agents {
		if err := syscall.Kill(a.PID, 0); err == nil {
			_ = syscall.Kill(-a.PGID, syscall.SIGKILL)
			_ = syscall.Kill(a.PID, syscall.SIGKILL)
			log.Printf("Sent SIGKILL to agent %s (PID %d)", a.ID, a.PID)
		}
	}

	lm.clearAll()
}

func (lm *LifecycleManager) clearAll() {
	lm.mu.Lock()
	lm.agents = make(map[string]*AgentEntry)
	lm.mu.Unlock()
	lm.persist()
}

// LoadStaleAgents reads the persisted agents.json from a previous session.
func (lm *LifecycleManager) LoadStaleAgents() ([]AgentEntry, error) {
	if lm.persistPath == "" {
		return nil, nil
	}
	data, err := os.ReadFile(lm.persistPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read agents.json: %w", err)
	}

	var entries []AgentEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse agents.json: %w", err)
	}
	return entries, nil
}

// ProcessAlive checks if a given PID is still running.
func ProcessAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// persist writes the current agent registry to disk.
func (lm *LifecycleManager) persist() {
	if lm.persistPath == "" {
		return
	}

	lm.mu.Lock()
	entries := make([]AgentEntry, 0, len(lm.agents))
	for _, e := range lm.agents {
		entries = append(entries, *e)
	}
	lm.mu.Unlock()

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal agents.json: %v", err)
		return
	}

	// Atomic write: temp file then rename
	dir := filepath.Dir(lm.persistPath)
	tmp, err := os.CreateTemp(dir, "agents-*.json")
	if err != nil {
		log.Printf("Failed to create temp file for agents.json: %v", err)
		return
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return
	}
	tmp.Close()

	if err := os.Rename(tmpName, lm.persistPath); err != nil {
		os.Remove(tmpName)
		log.Printf("Failed to persist agents.json: %v", err)
	}
}
