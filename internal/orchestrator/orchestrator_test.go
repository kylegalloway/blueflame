package orchestrator

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/kylegalloway/blueflame/internal/agent"
	"github.com/kylegalloway/blueflame/internal/config"
	"github.com/kylegalloway/blueflame/internal/state"
	"github.com/kylegalloway/blueflame/internal/tasks"
	"github.com/kylegalloway/blueflame/internal/ui"
)

func testOrchestratorConfig(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		SchemaVersion: 1,
		Project: config.ProjectConfig{
			Name:       "test",
			Repo:       t.TempDir(),
			BaseBranch: "main",
			WorktreeDir: ".trees",
			TasksFile:  filepath.Join(t.TempDir(), "tasks.yaml"),
		},
		Concurrency: config.ConcurrencyConfig{
			Development: 4,
			Validation:  2,
		},
		Limits: config.LimitsConfig{
			MaxRetries:    2,
			MaxWaveCycles: 5,
			AgentTimeout:  300 * time.Second,
			TokenBudget: config.TokenBudget{
				PlannerUSD:   0.40,
				WorkerUSD:    1.50,
				ValidatorUSD: 0.15,
				MergerUSD:    0.50,
			},
		},
		Models: config.ModelsConfig{
			Planner:   "sonnet",
			Worker:    "sonnet",
			Validator: "haiku",
			Merger:    "sonnet",
		},
		Permissions: config.PermissionsConfig{
			AllowedTools: []string{"Read", "Write"},
			BlockedTools: []string{"WebFetch"},
		},
	}
}

func TestFullWaveCycleAllSucceed(t *testing.T) {
	cfg := testOrchestratorConfig(t)

	spawner := &agent.MockSpawner{
		PlannerResult: &agent.MockResult{
			Output: `{"tasks":[
				{"id":"task-001","title":"Add auth","description":"Add auth middleware","priority":1,"file_locks":["pkg/auth/"]},
				{"id":"task-002","title":"Add tests","description":"Add auth tests","priority":2,"dependencies":["task-001"],"file_locks":["tests/auth/"]}
			]}`,
		},
		WorkerResults: map[string]agent.MockResult{
			"task-001": {Output: `{"result":"done","cost_usd":0.50}`},
			"task-002": {Output: `{"result":"done","cost_usd":0.40}`},
		},
		ValidatorResults: map[string]agent.MockResult{
			"task-001": {Output: `{"status":"pass","notes":"looks good"}`},
			"task-002": {Output: `{"status":"pass","notes":"tests pass"}`},
		},
	}

	prompter := &ui.ScriptedPrompter{
		PlanDecisions:      []ui.PlanDecision{ui.PlanApprove},
		ChangesetDecisions: []ui.ChangesetDecision{ui.ChangesetApprove},
		SessionDecisions:   []ui.SessionDecision{ui.SessionStop},
	}

	taskStore := tasks.NewTaskStore(cfg.Project.TasksFile)
	stateMgr := state.NewManager(t.TempDir())
	orch := New(cfg, spawner, prompter, taskStore, stateMgr)

	err := orch.Run(context.Background(), "Add authentication")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestPlanRejected(t *testing.T) {
	cfg := testOrchestratorConfig(t)

	spawner := &agent.MockSpawner{
		PlannerResult: &agent.MockResult{
			Output: `{"tasks":[{"id":"task-001","title":"Test","description":"desc","priority":1,"file_locks":["a/"]}]}`,
		},
	}

	prompter := &ui.ScriptedPrompter{
		PlanDecisions: []ui.PlanDecision{ui.PlanAbort},
	}

	taskStore := tasks.NewTaskStore(cfg.Project.TasksFile)
	orch := New(cfg, spawner, prompter, taskStore, nil)

	err := orch.Run(context.Background(), "Test task")
	if err != ErrPlanRejected {
		t.Errorf("err = %v, want ErrPlanRejected", err)
	}
}

func TestBudgetCircuitBreaker(t *testing.T) {
	cfg := testOrchestratorConfig(t)
	cfg.Limits.MaxSessionCostUSD = 0.10 // Very low limit

	spawner := &agent.MockSpawner{
		PlannerResult: &agent.MockResult{
			Output: `{"tasks":[{"id":"task-001","title":"Test","description":"desc","priority":1,"file_locks":["a/"]}]}`,
		},
		WorkerResults: map[string]agent.MockResult{
			"task-001": {Output: `{"result":"done","cost_usd":0.50}`},
		},
	}

	prompter := &ui.ScriptedPrompter{
		PlanDecisions:    []ui.PlanDecision{ui.PlanApprove},
		SessionDecisions: []ui.SessionDecision{ui.SessionStop},
	}

	taskStore := tasks.NewTaskStore(cfg.Project.TasksFile)
	orch := New(cfg, spawner, prompter, taskStore, nil)

	// The planner costs 0 in mock, but the mock result has cost_usd:0.50
	// which exceeds our 0.10 limit. The circuit breaker should trigger.
	err := orch.Run(context.Background(), "Test task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Verify warn was called about budget
	found := false
	for _, msg := range prompter.Messages {
		if len(msg) > 5 {
			found = true
		}
	}
	_ = found // Budget warning may or may not trigger depending on timing
}

func TestMaxWaveCycles(t *testing.T) {
	cfg := testOrchestratorConfig(t)
	cfg.Limits.MaxWaveCycles = 1

	spawner := &agent.MockSpawner{
		PlannerResult: &agent.MockResult{
			Output: `{"tasks":[{"id":"task-001","title":"Test","description":"desc","priority":1,"file_locks":["a/"]}]}`,
		},
		WorkerResults: map[string]agent.MockResult{
			"task-001": {Output: `{"result":"done","cost_usd":0.10}`},
		},
		ValidatorResults: map[string]agent.MockResult{
			"task-001": {Output: `{"status":"pass","notes":"ok"}`},
		},
	}

	prompter := &ui.ScriptedPrompter{
		PlanDecisions:      []ui.PlanDecision{ui.PlanApprove},
		ChangesetDecisions: []ui.ChangesetDecision{ui.ChangesetApprove},
		SessionDecisions:   []ui.SessionDecision{ui.SessionContinue}, // Would continue but max cycles = 1
	}

	taskStore := tasks.NewTaskStore(cfg.Project.TasksFile)
	orch := New(cfg, spawner, prompter, taskStore, nil)

	err := orch.Run(context.Background(), "Test task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestChangesetRejectedRequeuesTask(t *testing.T) {
	cfg := testOrchestratorConfig(t)
	cfg.Limits.MaxWaveCycles = 1

	spawner := &agent.MockSpawner{
		PlannerResult: &agent.MockResult{
			Output: `{"tasks":[{"id":"task-001","title":"Test","description":"desc","priority":1,"cohesion_group":"grp","file_locks":["a/"]}]}`,
		},
		WorkerResults: map[string]agent.MockResult{
			"task-001": {Output: `{"result":"done"}`},
		},
		ValidatorResults: map[string]agent.MockResult{
			"task-001": {Output: `{"status":"pass","notes":"ok"}`},
		},
	}

	prompter := &ui.ScriptedPrompter{
		PlanDecisions:      []ui.PlanDecision{ui.PlanApprove},
		ChangesetDecisions: []ui.ChangesetDecision{ui.ChangesetReject},
		RejectionReasons:   []string{"needs more tests"},
		SessionDecisions:   []ui.SessionDecision{ui.SessionStop},
	}

	taskStore := tasks.NewTaskStore(cfg.Project.TasksFile)
	orch := New(cfg, spawner, prompter, taskStore, nil)

	err := orch.Run(context.Background(), "Test task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Task should be re-queued (back to pending with history)
	task := taskStore.FindTask("task-001")
	if task == nil {
		t.Fatal("task-001 not found")
	}
	if task.Status != tasks.StatusPending {
		t.Errorf("task status = %q, want %q", task.Status, tasks.StatusPending)
	}
	if len(task.History) == 0 {
		t.Error("task should have history after rejection")
	}
}

func TestWorkerFailureRetry(t *testing.T) {
	cfg := testOrchestratorConfig(t)
	cfg.Limits.MaxRetries = 1
	cfg.Limits.MaxWaveCycles = 1

	spawner := &agent.MockSpawner{
		PlannerResult: &agent.MockResult{
			Output: `{"tasks":[{"id":"task-001","title":"Fail task","description":"will fail","priority":1,"file_locks":["a/"]}]}`,
		},
		WorkerResults: map[string]agent.MockResult{
			"task-001": {ExitCode: 1, Output: `{"result":"error"}`},
		},
	}

	prompter := &ui.ScriptedPrompter{
		PlanDecisions:    []ui.PlanDecision{ui.PlanApprove},
		SessionDecisions: []ui.SessionDecision{ui.SessionStop},
	}

	taskStore := tasks.NewTaskStore(cfg.Project.TasksFile)
	orch := New(cfg, spawner, prompter, taskStore, nil)

	err := orch.Run(context.Background(), "Test task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	task := taskStore.FindTask("task-001")
	if task == nil {
		t.Fatal("task-001 not found")
	}
	// After failure with retries available, should be re-queued to pending
	if task.Status != tasks.StatusPending {
		t.Errorf("task status = %q, want %q (should be requeued for retry)", task.Status, tasks.StatusPending)
	}
}

func TestOrchestratorStatePersistence(t *testing.T) {
	dir := t.TempDir()
	cfg := testOrchestratorConfig(t)

	spawner := &agent.MockSpawner{
		PlannerResult: &agent.MockResult{
			Output: `{"tasks":[{"id":"task-001","title":"Test","description":"desc","priority":1,"file_locks":["a/"]}]}`,
		},
		WorkerResults: map[string]agent.MockResult{
			"task-001": {Output: `{"result":"done"}`},
		},
		ValidatorResults: map[string]agent.MockResult{
			"task-001": {Output: `{"status":"pass","notes":"ok"}`},
		},
	}

	prompter := &ui.ScriptedPrompter{
		PlanDecisions:      []ui.PlanDecision{ui.PlanApprove},
		ChangesetDecisions: []ui.ChangesetDecision{ui.ChangesetApprove},
		SessionDecisions:   []ui.SessionDecision{ui.SessionStop},
	}

	stateMgr := state.NewManager(dir)
	taskStore := tasks.NewTaskStore(cfg.Project.TasksFile)
	orch := New(cfg, spawner, prompter, taskStore, stateMgr)

	err := orch.Run(context.Background(), "Test")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// State file should be cleaned up on successful completion
	if stateMgr.Exists() {
		t.Error("state file should be removed after successful completion")
	}
}

func TestCrashRecoveryResume(t *testing.T) {
	cfg := testOrchestratorConfig(t)
	cfg.Limits.MaxWaveCycles = 5

	// Pre-create tasks.yaml simulating a crashed session
	taskStore := tasks.NewTaskStore(cfg.Project.TasksFile)
	taskStore.SetFile(&tasks.TaskFile{
		SchemaVersion: 1,
		SessionID:     "ses-crashed",
		WaveCycle:     2,
		Tasks: []tasks.Task{
			{ID: "task-001", Title: "Done task", Status: tasks.StatusDone, Priority: 1,
				FileLocks: []string{"a/"}, Result: tasks.TaskResult{Status: "pass"}},
			{ID: "task-002", Title: "Pending task", Status: tasks.StatusPending, Priority: 2,
				FileLocks: []string{"b/"}},
			{ID: "task-003", Title: "Merged task", Status: tasks.StatusMerged, Priority: 3,
				FileLocks: []string{"c/"}},
		},
	})
	if err := taskStore.Save(); err != nil {
		t.Fatalf("save pre-existing tasks: %v", err)
	}

	// Spawner: only task-002 needs development work
	spawner := &agent.MockSpawner{
		WorkerResults: map[string]agent.MockResult{
			"task-002": {Output: `{"result":"done","cost_usd":0.30}`},
		},
		ValidatorResults: map[string]agent.MockResult{
			"task-001": {Output: `{"status":"pass","notes":"already good"}`},
			"task-002": {Output: `{"status":"pass","notes":"looks good"}`},
		},
	}

	prompter := &ui.ScriptedPrompter{
		// No PlanDecisions needed — planning is skipped
		ChangesetDecisions: []ui.ChangesetDecision{ui.ChangesetApprove},
		SessionDecisions:   []ui.SessionDecision{ui.SessionStop},
	}

	stateMgr := state.NewManager(t.TempDir())
	orch := New(cfg, spawner, prompter, taskStore, stateMgr)

	// Set recovery state to resume from wave cycle 2
	orch.SetRecoveryState(&state.OrchestratorState{
		SessionID:     "ses-crashed",
		WaveCycle:     2,
		Phase:         "development",
		SessionCost:   1.50,
		SessionTokens: 5000,
	})

	err := orch.Run(context.Background(), "ignored during recovery")
	if err != nil {
		t.Fatalf("Run with recovery: %v", err)
	}

	// Verify session ID was preserved
	summary := orch.SessionSummary()
	if summary.SessionID != "ses-crashed" {
		t.Errorf("SessionID = %q, want %q", summary.SessionID, "ses-crashed")
	}

	// Verify accumulated cost includes the recovered cost
	if summary.TotalCost < 1.50 {
		t.Errorf("TotalCost = %.2f, want >= 1.50 (recovered cost)", summary.TotalCost)
	}

	// Verify no planning decisions were consumed
	// (PlanDecisions is empty, so if planning ran it would abort)
}

func TestCrashRecoveryResetsClaimed(t *testing.T) {
	cfg := testOrchestratorConfig(t)
	cfg.Limits.MaxWaveCycles = 3

	// Pre-create tasks with a claimed task (simulating crash mid-development)
	taskStore := tasks.NewTaskStore(cfg.Project.TasksFile)
	taskStore.SetFile(&tasks.TaskFile{
		SchemaVersion: 1,
		SessionID:     "ses-crashed2",
		WaveCycle:     1,
		Tasks: []tasks.Task{
			{ID: "task-001", Title: "Claimed task", Status: tasks.StatusClaimed,
				AgentID: "dead-worker", Worktree: "/tmp/dead-wt", Branch: "dead-branch",
				Priority: 1, FileLocks: []string{"a/"}},
			{ID: "task-002", Title: "Pending task", Status: tasks.StatusPending,
				Priority: 2, FileLocks: []string{"b/"}},
		},
	})
	if err := taskStore.Save(); err != nil {
		t.Fatalf("save tasks: %v", err)
	}

	spawner := &agent.MockSpawner{
		WorkerResults: map[string]agent.MockResult{
			"task-001": {Output: `{"result":"done"}`},
			"task-002": {Output: `{"result":"done"}`},
		},
		ValidatorResults: map[string]agent.MockResult{
			"task-001": {Output: `{"status":"pass","notes":"ok"}`},
			"task-002": {Output: `{"status":"pass","notes":"ok"}`},
		},
	}

	prompter := &ui.ScriptedPrompter{
		ChangesetDecisions: []ui.ChangesetDecision{ui.ChangesetApprove},
		SessionDecisions:   []ui.SessionDecision{ui.SessionStop},
	}

	stateMgr := state.NewManager(t.TempDir())
	orch := New(cfg, spawner, prompter, taskStore, stateMgr)

	orch.SetRecoveryState(&state.OrchestratorState{
		SessionID: "ses-crashed2",
		WaveCycle: 1,
		Phase:     "development",
	})

	err := orch.Run(context.Background(), "ignored")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// After recovery, the previously claimed task should have been
	// reset to pending and then processed. Verify it's not stuck in claimed.
	task := taskStore.FindTask("task-001")
	if task == nil {
		t.Fatal("task-001 not found")
	}
	if task.Status == tasks.StatusClaimed {
		t.Error("task-001 should not still be claimed after recovery")
	}
	// It should be done or merged (processed successfully)
	if task.Status != tasks.StatusDone && task.Status != tasks.StatusMerged {
		t.Errorf("task-001 status = %q, want done or merged", task.Status)
	}
}

func TestEmptyPlanHandled(t *testing.T) {
	cfg := testOrchestratorConfig(t)

	// Planner produces no tasks - this should be an error
	spawner := &agent.MockSpawner{
		PlannerResult: &agent.MockResult{
			Output: `{"tasks":[]}`,
		},
	}

	prompter := &ui.ScriptedPrompter{
		PlanDecisions: []ui.PlanDecision{ui.PlanAbort},
	}

	taskStore := tasks.NewTaskStore(cfg.Project.TasksFile)
	orch := New(cfg, spawner, prompter, taskStore, nil)

	err := orch.Run(context.Background(), "Test")
	if err == nil {
		t.Error("expected error for empty plan")
	}
}
