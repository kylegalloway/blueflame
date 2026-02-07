package agent

import (
	"context"
	"testing"
	"time"

	"github.com/kylegalloway/blueflame/internal/config"
	"github.com/kylegalloway/blueflame/internal/tasks"
)

func testConfig() *config.Config {
	return &config.Config{
		Project: config.ProjectConfig{
			Name:       "test",
			Repo:       "/tmp",
			BaseBranch: "main",
		},
		Models: config.ModelsConfig{
			Planner:   "sonnet",
			Worker:    "sonnet",
			Validator: "haiku",
			Merger:    "sonnet",
		},
		Permissions: config.PermissionsConfig{
			AllowedTools: []string{"Read", "Write", "Edit"},
			BlockedTools: []string{"WebFetch", "Task"},
		},
		Limits: config.LimitsConfig{
			TokenBudget: config.TokenBudget{
				PlannerUSD:   0.40,
				WorkerUSD:    1.50,
				ValidatorUSD: 0.15,
				MergerUSD:    0.50,
			},
		},
	}
}

func TestMockSpawnerWorker(t *testing.T) {
	spawner := &MockSpawner{
		WorkerResults: map[string]MockResult{
			"task-001": {Output: `{"result":"done","cost_usd":0.50,"input_tokens":1000,"output_tokens":500}`},
		},
	}

	task := &tasks.Task{
		ID:      "task-001",
		AgentID: "worker-test0001",
		Title:   "Test task",
	}

	agent, err := spawner.SpawnWorker(context.Background(), task, testConfig())
	if err != nil {
		t.Fatalf("SpawnWorker: %v", err)
	}

	if agent.Role != RoleWorker {
		t.Errorf("Role = %q, want %q", agent.Role, RoleWorker)
	}
	if agent.ID != "worker-test0001" {
		t.Errorf("ID = %q, want %q", agent.ID, "worker-test0001")
	}

	result := MockCollectResult(agent)
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
	if result.Output.CostUSD != 0.50 {
		t.Errorf("CostUSD = %f, want 0.50", result.Output.CostUSD)
	}
	if result.TokensUsed != 1500 {
		t.Errorf("TokensUsed = %d, want 1500", result.TokensUsed)
	}
}

func TestMockSpawnerPlanner(t *testing.T) {
	spawner := &MockSpawner{
		PlannerResult: &MockResult{
			Output: `{"tasks":[{"id":"task-001","title":"Test"}]}`,
		},
	}

	agent, err := spawner.SpawnPlanner(context.Background(), "test desc", "", testConfig())
	if err != nil {
		t.Fatalf("SpawnPlanner: %v", err)
	}

	if agent.Role != RolePlanner {
		t.Errorf("Role = %q, want %q", agent.Role, RolePlanner)
	}

	result := MockCollectResult(agent)
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
}

func TestMockSpawnerValidator(t *testing.T) {
	spawner := &MockSpawner{
		ValidatorResults: map[string]MockResult{
			"task-001": {Output: `{"status":"pass","notes":"LGTM"}`},
		},
	}

	task := &tasks.Task{ID: "task-001", Title: "Test"}
	agent, err := spawner.SpawnValidator(context.Background(), task, "diff here", "", testConfig())
	if err != nil {
		t.Fatalf("SpawnValidator: %v", err)
	}

	if agent.Role != RoleValidator {
		t.Errorf("Role = %q, want %q", agent.Role, RoleValidator)
	}

	result := MockCollectResult(agent)
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
}

func TestMockSpawnerMerger(t *testing.T) {
	spawner := &MockSpawner{
		MergerResult: &MockResult{Output: `{"result":"merged"}`},
	}

	branches := []BranchInfo{
		{Name: "blueflame/task-001", TaskID: "task-001", TaskTitle: "Test"},
	}
	agent, err := spawner.SpawnMerger(context.Background(), branches, testConfig())
	if err != nil {
		t.Fatalf("SpawnMerger: %v", err)
	}

	if agent.Role != RoleMerger {
		t.Errorf("Role = %q, want %q", agent.Role, RoleMerger)
	}

	result := MockCollectResult(agent)
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
}

func TestMockSpawnerWithDelay(t *testing.T) {
	spawner := &MockSpawner{
		Delay: 100 * time.Millisecond,
	}

	task := &tasks.Task{ID: "task-001", AgentID: "worker-delay", Title: "Test"}
	agent, err := spawner.SpawnWorker(context.Background(), task, testConfig())
	if err != nil {
		t.Fatalf("SpawnWorker: %v", err)
	}

	start := time.Now()
	result := MockCollectResult(agent)
	elapsed := time.Since(start)

	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
	if elapsed < 50*time.Millisecond {
		t.Errorf("elapsed = %v, expected at least 50ms delay", elapsed)
	}
}

func TestMockSpawnerSpawnError(t *testing.T) {
	spawner := &MockSpawner{
		WorkerResults: map[string]MockResult{
			"task-001": {Err: context.Canceled},
		},
	}

	task := &tasks.Task{ID: "task-001", AgentID: "worker-fail", Title: "Test"}
	_, err := spawner.SpawnWorker(context.Background(), task, testConfig())
	if err == nil {
		t.Error("expected spawn error")
	}
}

func TestAgentSpawnerInterface(t *testing.T) {
	// Verify both types implement the interface
	var _ AgentSpawner = &ProductionSpawner{}
	var _ AgentSpawner = &MockSpawner{}
}
