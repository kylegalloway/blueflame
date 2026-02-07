package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/kylegalloway/blueflame/internal/config"
	"github.com/kylegalloway/blueflame/internal/tasks"
)

// Role constants.
const (
	RolePlanner   = "planner"
	RoleWorker    = "worker"
	RoleValidator = "validator"
	RoleMerger    = "merger"
)

// Agent represents a running or completed claude CLI process.
type Agent struct {
	ID       string
	Cmd      *exec.Cmd
	Task     *tasks.Task
	Stdout   *bytes.Buffer
	Stderr   *bytes.Buffer
	Started  time.Time
	Role     string
	Budget   config.BudgetSpec
}

// BranchInfo describes a validated branch for the merger.
type BranchInfo struct {
	Name         string
	TaskID       string
	TaskTitle    string
	FilesChanged int
}

// ClaudeOutput represents the JSON output from claude --print --output-format json.
type ClaudeOutput struct {
	Result       string  `json:"result"`
	CostUSD      float64 `json:"cost_usd"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	Duration     float64 `json:"duration_seconds"`
}

// AgentResult holds the outcome of an agent execution.
type AgentResult struct {
	AgentID    string
	TaskID     string
	ExitCode   int
	Output     ClaudeOutput
	RawStdout  []byte
	RawStderr  []byte
	CostUSD    float64
	TokensUsed int
	Duration   time.Duration
	Err        error
}

// AgentSpawner is the interface for spawning claude CLI agents.
type AgentSpawner interface {
	SpawnPlanner(ctx context.Context, description string, priorContext string, cfg *config.Config) (*Agent, error)
	SpawnWorker(ctx context.Context, task *tasks.Task, cfg *config.Config) (*Agent, error)
	SpawnValidator(ctx context.Context, task *tasks.Task, diff string, auditSummary string, cfg *config.Config) (*Agent, error)
	SpawnMerger(ctx context.Context, branches []BranchInfo, cfg *config.Config) (*Agent, error)
}

// ProductionSpawner implements AgentSpawner using real claude CLI invocations.
type ProductionSpawner struct {
	// PromptRenderer renders prompt templates.
	PromptRenderer PromptRenderer
	// HooksDir is the base directory for generated hook scripts.
	HooksDir string
}

// PromptRenderer renders prompt templates for different agent roles.
type PromptRenderer interface {
	RenderPrompt(role string, data interface{}) (string, error)
	RenderSystemPrompt(role string, data interface{}) (string, error)
}

func (s *ProductionSpawner) SpawnWorker(ctx context.Context, task *tasks.Task, cfg *config.Config) (*Agent, error) {
	agentID := task.AgentID
	if agentID == "" {
		return nil, fmt.Errorf("task %s has no agent_id", task.ID)
	}

	args := []string{
		"--print",
		"--model", cfg.Models.Worker,
		"--allowed-tools", strings.Join(cfg.Permissions.AllowedTools, ","),
		"--disallowed-tools", strings.Join(cfg.Permissions.BlockedTools, ","),
		"--output-format", "json",
	}

	budget := cfg.Limits.TokenBudget.WorkerBudget()
	if budget.Unit == config.USD && budget.Value > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", budget.Value))
	}

	// The prompt would be rendered from template in production
	args = append(args, fmt.Sprintf("Implement task %s: %s", task.ID, task.Title))

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = task.Worktree
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start worker: %w", err)
	}

	return &Agent{
		ID:      agentID,
		Cmd:     cmd,
		Task:    task,
		Stdout:  &stdout,
		Stderr:  &stderr,
		Started: time.Now(),
		Role:    RoleWorker,
		Budget:  budget,
	}, nil
}

func (s *ProductionSpawner) SpawnPlanner(ctx context.Context, description string, priorContext string, cfg *config.Config) (*Agent, error) {
	args := []string{
		"--print",
		"--model", cfg.Models.Planner,
		"--output-format", "json",
	}

	budget := cfg.Limits.TokenBudget.PlannerBudget()
	if budget.Unit == config.USD && budget.Value > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", budget.Value))
	}

	args = append(args, description)

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = cfg.Project.Repo
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start planner: %w", err)
	}

	return &Agent{
		ID:      fmt.Sprintf("planner-%08x", time.Now().UnixNano()&0xFFFFFFFF),
		Cmd:     cmd,
		Stdout:  &stdout,
		Stderr:  &stderr,
		Started: time.Now(),
		Role:    RolePlanner,
		Budget:  budget,
	}, nil
}

func (s *ProductionSpawner) SpawnValidator(ctx context.Context, task *tasks.Task, diff string, auditSummary string, cfg *config.Config) (*Agent, error) {
	args := []string{
		"--print",
		"--model", cfg.Models.Validator,
		"--allowed-tools", "Read,Glob,Grep,Bash",
		"--disallowed-tools", "Write,Edit,WebFetch,WebSearch,NotebookEdit,Task",
		"--output-format", "json",
	}

	budget := cfg.Limits.TokenBudget.ValidatorBudget()
	if budget.Unit == config.USD && budget.Value > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", budget.Value))
	}

	args = append(args, fmt.Sprintf("Validate task %s: %s\n\nDiff:\n%s", task.ID, task.Title, diff))

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = task.Worktree
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start validator: %w", err)
	}

	return &Agent{
		ID:      fmt.Sprintf("validator-%08x", time.Now().UnixNano()&0xFFFFFFFF),
		Cmd:     cmd,
		Task:    task,
		Stdout:  &stdout,
		Stderr:  &stderr,
		Started: time.Now(),
		Role:    RoleValidator,
		Budget:  budget,
	}, nil
}

func (s *ProductionSpawner) SpawnMerger(ctx context.Context, branches []BranchInfo, cfg *config.Config) (*Agent, error) {
	args := []string{
		"--print",
		"--model", cfg.Models.Merger,
		"--output-format", "json",
	}

	budget := cfg.Limits.TokenBudget.MergerBudget()
	if budget.Unit == config.USD && budget.Value > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", budget.Value))
	}

	var desc strings.Builder
	desc.WriteString("Merge the following validated branches:\n")
	for _, b := range branches {
		fmt.Fprintf(&desc, "- %s (task %s: %s)\n", b.Name, b.TaskID, b.TaskTitle)
	}
	args = append(args, desc.String())

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = cfg.Project.Repo
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start merger: %w", err)
	}

	return &Agent{
		ID:      fmt.Sprintf("merger-%08x", time.Now().UnixNano()&0xFFFFFFFF),
		Cmd:     cmd,
		Stdout:  &stdout,
		Stderr:  &stderr,
		Started: time.Now(),
		Role:    RoleMerger,
		Budget:  budget,
	}, nil
}

// CollectResult waits for an agent to complete and returns its result.
func CollectResult(agent *Agent) AgentResult {
	err := agent.Cmd.Wait()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}

	var output ClaudeOutput
	json.Unmarshal(agent.Stdout.Bytes(), &output)

	result := AgentResult{
		AgentID:   agent.ID,
		ExitCode:  exitCode,
		Output:    output,
		RawStdout: agent.Stdout.Bytes(),
		RawStderr: agent.Stderr.Bytes(),
		CostUSD:   output.CostUSD,
		TokensUsed: output.InputTokens + output.OutputTokens,
		Duration:  time.Since(agent.Started),
		Err:       err,
	}
	if agent.Task != nil {
		result.TaskID = agent.Task.ID
	}
	return result
}
