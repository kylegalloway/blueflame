package agent

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/kylegalloway/blueflame/internal/config"
	"github.com/kylegalloway/blueflame/internal/tasks"
)

func TestLifecycleRegisterUnregister(t *testing.T) {
	dir := t.TempDir()
	lm := NewLifecycleManager(LifecycleConfig{
		PersistPath: filepath.Join(dir, "agents.json"),
	})

	// Start a real process to register
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	defer cmd.Process.Kill()

	task := &tasks.Task{ID: "task-1", Worktree: "/tmp/wt"}
	a := &Agent{
		ID:      "worker-test001",
		Cmd:     cmd,
		Task:    task,
		Role:    RoleWorker,
		Started: time.Now(),
		Budget:  config.BudgetSpec{Unit: config.USD, Value: 5.0},
	}

	if err := lm.Register(a); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if lm.RunningCount() != 1 {
		t.Errorf("RunningCount = %d, want 1", lm.RunningCount())
	}

	agents := lm.RunningAgents()
	if len(agents) != 1 {
		t.Fatalf("RunningAgents len = %d, want 1", len(agents))
	}
	if agents[0].ID != "worker-test001" {
		t.Errorf("agent ID = %q, want %q", agents[0].ID, "worker-test001")
	}
	if agents[0].PID != cmd.Process.Pid {
		t.Errorf("agent PID = %d, want %d", agents[0].PID, cmd.Process.Pid)
	}
	if agents[0].TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", agents[0].TaskID, "task-1")
	}

	// Unregister
	lm.Unregister("worker-test001", AgentResult{ExitCode: 0, CostUSD: 1.5, TokensUsed: 1000})

	if lm.RunningCount() != 0 {
		t.Errorf("RunningCount after unregister = %d, want 0", lm.RunningCount())
	}

	// Kill the process now so it doesn't linger
	cmd.Process.Kill()
	cmd.Wait()
}

func TestLifecyclePersistence(t *testing.T) {
	dir := t.TempDir()
	persistPath := filepath.Join(dir, "agents.json")

	lm := NewLifecycleManager(LifecycleConfig{
		PersistPath: persistPath,
	})

	// Start a process
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()

	a := &Agent{
		ID:      "worker-persist",
		Cmd:     cmd,
		Role:    RoleWorker,
		Started: time.Now(),
	}
	lm.Register(a)

	// Check persistence file exists and contains the agent
	data, err := os.ReadFile(persistPath)
	if err != nil {
		t.Fatalf("read agents.json: %v", err)
	}

	var entries []AgentEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("parse agents.json: %v", err)
	}
	if len(entries) != 1 || entries[0].ID != "worker-persist" {
		t.Errorf("persisted entries = %v, want 1 entry with ID worker-persist", entries)
	}

	// LoadStaleAgents from a fresh manager
	lm2 := NewLifecycleManager(LifecycleConfig{
		PersistPath: persistPath,
	})
	stale, err := lm2.LoadStaleAgents()
	if err != nil {
		t.Fatalf("LoadStaleAgents: %v", err)
	}
	if len(stale) != 1 || stale[0].ID != "worker-persist" {
		t.Errorf("stale agents = %v, want 1 with ID worker-persist", stale)
	}
}

func TestLifecycleLoadStaleAgentsNoFile(t *testing.T) {
	lm := NewLifecycleManager(LifecycleConfig{
		PersistPath: filepath.Join(t.TempDir(), "nonexistent.json"),
	})
	stale, err := lm.LoadStaleAgents()
	if err != nil {
		t.Fatalf("LoadStaleAgents: %v", err)
	}
	if len(stale) != 0 {
		t.Errorf("expected no stale agents, got %d", len(stale))
	}
}

func TestLifecycleRegisterNoProcess(t *testing.T) {
	lm := NewLifecycleManager(LifecycleConfig{})

	a := &Agent{ID: "no-proc", Cmd: nil}
	if err := lm.Register(a); err == nil {
		t.Error("expected error registering agent with nil Cmd")
	}

	a.Cmd = &exec.Cmd{} // Cmd with no Process
	if err := lm.Register(a); err == nil {
		t.Error("expected error registering agent with nil Process")
	}
}

func TestLifecycleMonitorDetectsDeadProcess(t *testing.T) {
	dir := t.TempDir()
	deathCh := make(chan string, 1)

	lm := NewLifecycleManager(LifecycleConfig{
		PersistPath:       filepath.Join(dir, "agents.json"),
		HeartbeatInterval: 50 * time.Millisecond,
	})
	lm.onAgentDeath = func(entry *AgentEntry) {
		deathCh <- entry.ID
	}

	// Start a process that exits immediately
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	cmd.Wait() // Wait for it to die

	// Force register with known PID (process is already dead)
	lm.mu.Lock()
	lm.agents["worker-dying"] = &AgentEntry{
		ID:        "worker-dying",
		PID:       cmd.Process.Pid,
		PGID:      cmd.Process.Pid,
		Role:      RoleWorker,
		StartTime: time.Now(),
		Status:    "running",
	}
	lm.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	go lm.MonitorLoop(ctx)

	select {
	case id := <-deathCh:
		if id != "worker-dying" {
			t.Errorf("death callback got ID %q, want %q", id, "worker-dying")
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for death detection")
	}

	if lm.RunningCount() != 0 {
		t.Errorf("RunningCount after death = %d, want 0", lm.RunningCount())
	}
}

func TestLifecycleGracefulShutdown(t *testing.T) {
	dir := t.TempDir()
	lm := NewLifecycleManager(LifecycleConfig{
		PersistPath: filepath.Join(dir, "agents.json"),
	})

	// Start two processes with their own process groups
	cmd1 := exec.Command("sleep", "60")
	cmd1.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd2 := exec.Command("sleep", "60")
	cmd2.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd1.Start()
	cmd2.Start()

	lm.mu.Lock()
	lm.agents["w1"] = &AgentEntry{ID: "w1", PID: cmd1.Process.Pid, PGID: cmd1.Process.Pid, Status: "running"}
	lm.agents["w2"] = &AgentEntry{ID: "w2", PID: cmd2.Process.Pid, PGID: cmd2.Process.Pid, Status: "running"}
	lm.mu.Unlock()

	lm.GracefulShutdown(5 * time.Second)

	if lm.RunningCount() != 0 {
		t.Errorf("RunningCount after shutdown = %d, want 0", lm.RunningCount())
	}

	// Reap the killed child processes so they don't become zombies.
	// cmd.Wait() should return an error since the processes were signaled.
	err1 := cmd1.Wait()
	err2 := cmd2.Wait()
	if err1 == nil {
		t.Error("cmd1 exited cleanly, expected signal death")
	}
	if err2 == nil {
		t.Error("cmd2 exited cleanly, expected signal death")
	}
}

func TestLifecycleKillAgent(t *testing.T) {
	dir := t.TempDir()
	lm := NewLifecycleManager(LifecycleConfig{
		PersistPath: filepath.Join(dir, "agents.json"),
	})

	cmd := exec.Command("sleep", "60")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Start()
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()

	lm.mu.Lock()
	lm.agents["w-kill"] = &AgentEntry{
		ID:   "w-kill",
		PID:  cmd.Process.Pid,
		PGID: cmd.Process.Pid,
	}
	lm.mu.Unlock()

	if err := lm.KillAgent("w-kill", "test"); err != nil {
		t.Fatalf("KillAgent: %v", err)
	}

	if lm.RunningCount() != 0 {
		t.Errorf("RunningCount after kill = %d, want 0", lm.RunningCount())
	}

	// Non-existent agent
	if err := lm.KillAgent("nonexistent", "test"); err == nil {
		t.Error("expected error killing nonexistent agent")
	}
}

func TestLifecycleStallDetection(t *testing.T) {
	dir := t.TempDir()
	auditDir := filepath.Join(dir, "audit")
	os.MkdirAll(auditDir, 0755)

	lm := NewLifecycleManager(LifecycleConfig{
		StallThreshold: 1 * time.Millisecond, // Very short for testing
		AuditDir:       auditDir,
	})

	// Create an old audit log
	auditFile := filepath.Join(auditDir, "worker-stall.jsonl")
	os.WriteFile(auditFile, []byte(`{"event":"tool_call"}`), 0644)

	// Backdate the file
	oldTime := time.Now().Add(-1 * time.Hour)
	os.Chtimes(auditFile, oldTime, oldTime)

	entry := &AgentEntry{
		ID:  "worker-stall",
		PID: os.Getpid(), // Use our own PID so it's "alive"
	}

	if !lm.isStalled(entry) {
		t.Error("expected stall detection for old audit log")
	}

	// With a very large threshold, should not be stalled
	lm.stallThreshold = 24 * time.Hour
	if lm.isStalled(entry) {
		t.Error("audit log within threshold should not be stalled")
	}
}

func TestProcessAlive(t *testing.T) {
	// Our own PID should be alive
	if !ProcessAlive(os.Getpid()) {
		t.Error("expected own process to be alive")
	}

	// PID 0 should not be "alive" from our perspective (it's the kernel)
	// Use a very unlikely PID
	if ProcessAlive(999999999) {
		t.Error("expected non-existent PID to not be alive")
	}
}

func TestLifecycleNoPersistPath(t *testing.T) {
	lm := NewLifecycleManager(LifecycleConfig{})

	// LoadStaleAgents with no persist path should return nil
	stale, err := lm.LoadStaleAgents()
	if err != nil {
		t.Fatalf("LoadStaleAgents: %v", err)
	}
	if stale != nil {
		t.Errorf("expected nil stale agents, got %v", stale)
	}

	// persist should be a no-op (no panic)
	lm.persist()
}
