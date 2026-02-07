package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/kylegalloway/blueflame/internal/config"
	"github.com/kylegalloway/blueflame/internal/tasks"
)

// MockSpawner implements AgentSpawner with predetermined responses for testing.
type MockSpawner struct {
	// WorkerResults maps task IDs to predetermined results.
	WorkerResults map[string]MockResult
	// PlannerResult is the result the planner will produce.
	PlannerResult *MockResult
	// ValidatorResults maps task IDs to predetermined validation results.
	ValidatorResults map[string]MockResult
	// MergerResult is the result the merger will produce.
	MergerResult *MockResult
	// Delay is how long mock agents take to "run".
	Delay time.Duration
}

// MockResult defines what a mock agent will produce.
type MockResult struct {
	ExitCode int
	Output   string
	Err      error
}

func (m *MockSpawner) SpawnPlanner(ctx context.Context, description string, priorContext string, cfg *config.Config) (*Agent, error) {
	if m.PlannerResult != nil && m.PlannerResult.Err != nil {
		return nil, m.PlannerResult.Err
	}

	output := `{"result": "planned"}`
	if m.PlannerResult != nil && m.PlannerResult.Output != "" {
		output = m.PlannerResult.Output
	}

	return m.createMockAgent("planner-mock0001", RolePlanner, nil, output, cfg)
}

func (m *MockSpawner) SpawnWorker(ctx context.Context, task *tasks.Task, cfg *config.Config) (*Agent, error) {
	if result, ok := m.WorkerResults[task.ID]; ok && result.Err != nil {
		return nil, result.Err
	}

	output := `{"result": "done"}`
	if result, ok := m.WorkerResults[task.ID]; ok && result.Output != "" {
		output = result.Output
	}

	agentID := task.AgentID
	if agentID == "" {
		agentID = fmt.Sprintf("worker-mock%04d", time.Now().UnixNano()%10000)
	}

	return m.createMockAgent(agentID, RoleWorker, task, output, cfg)
}

func (m *MockSpawner) SpawnValidator(ctx context.Context, task *tasks.Task, diff string, auditSummary string, cfg *config.Config) (*Agent, error) {
	if result, ok := m.ValidatorResults[task.ID]; ok && result.Err != nil {
		return nil, result.Err
	}

	output := `{"status": "pass", "notes": "looks good"}`
	if result, ok := m.ValidatorResults[task.ID]; ok && result.Output != "" {
		output = result.Output
	}

	return m.createMockAgent(
		fmt.Sprintf("validator-mock%04d", time.Now().UnixNano()%10000),
		RoleValidator, task, output, cfg,
	)
}

func (m *MockSpawner) SpawnMerger(ctx context.Context, branches []BranchInfo, cfg *config.Config) (*Agent, error) {
	if m.MergerResult != nil && m.MergerResult.Err != nil {
		return nil, m.MergerResult.Err
	}

	output := `{"result": "merged"}`
	if m.MergerResult != nil && m.MergerResult.Output != "" {
		output = m.MergerResult.Output
	}

	return m.createMockAgent("merger-mock0001", RoleMerger, nil, output, cfg)
}

func (m *MockSpawner) createMockAgent(id, role string, task *tasks.Task, output string, cfg *config.Config) (*Agent, error) {
	// Determine exit code from MockResult
	exitCode := 0
	if task != nil {
		if result, ok := m.WorkerResults[task.ID]; ok {
			exitCode = result.ExitCode
		}
	}

	sleepMs := int(m.Delay.Milliseconds())
	var cmd *exec.Cmd
	if sleepMs > 0 {
		cmd = exec.Command("sleep", fmt.Sprintf("%.3f", m.Delay.Seconds()))
	} else if exitCode != 0 {
		cmd = exec.Command("bash", "-c", fmt.Sprintf("exit %d", exitCode))
	} else {
		cmd = exec.Command("true")
	}

	var stdout bytes.Buffer
	// Pre-fill stdout with the expected output since the mock command won't produce it
	stdout.WriteString(output)

	var stderr bytes.Buffer
	cmd.Stdout = &bytes.Buffer{} // command's stdout goes to /dev/null equivalent
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start mock agent: %w", err)
	}

	var budget config.BudgetSpec
	switch role {
	case RolePlanner:
		budget = cfg.Limits.TokenBudget.PlannerBudget()
	case RoleWorker:
		budget = cfg.Limits.TokenBudget.WorkerBudget()
	case RoleValidator:
		budget = cfg.Limits.TokenBudget.ValidatorBudget()
	case RoleMerger:
		budget = cfg.Limits.TokenBudget.MergerBudget()
	}

	return &Agent{
		ID:      id,
		Cmd:     cmd,
		Task:    task,
		Stdout:  &stdout,
		Stderr:  &stderr,
		Started: time.Now(),
		Role:    role,
		Budget:  budget,
	}, nil
}

// MockCollectResult collects the result from a mock agent, parsing the pre-filled stdout.
func MockCollectResult(agent *Agent) AgentResult {
	err := agent.Cmd.Wait()
	exitCode := 0
	if err != nil {
		exitCode = 1
	}

	var output ClaudeOutput
	json.Unmarshal(agent.Stdout.Bytes(), &output)

	result := AgentResult{
		AgentID:    agent.ID,
		ExitCode:   exitCode,
		Output:     output,
		RawStdout:  agent.Stdout.Bytes(),
		RawStderr:  agent.Stderr.Bytes(),
		CostUSD:    output.TotalCostUSD,
		TokensUsed: output.Usage.InputTokens + output.Usage.OutputTokens,
		Duration:   time.Since(agent.Started),
		Err:        err,
	}
	if agent.Task != nil {
		result.TaskID = agent.Task.ID
	}
	return result
}
