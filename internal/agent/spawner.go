package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
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
	Type         string       `json:"type"`
	Subtype      string       `json:"subtype"`
	Result       string       `json:"result"`
	IsError      bool         `json:"is_error"`
	TotalCostUSD float64      `json:"total_cost_usd"`
	DurationMS   int          `json:"duration_ms"`
	NumTurns     int          `json:"num_turns"`
	Usage        ClaudeUsage  `json:"usage"`
	SessionID    string       `json:"session_id"`
}

// ClaudeUsage represents token usage from claude CLI output.
type ClaudeUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
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

	allowedTools := append([]string{}, cfg.Permissions.AllowedTools...)
	// Wire superpowers skills as additional allowed tools
	if cfg.Superpowers.Enabled && len(cfg.Superpowers.Skills) > 0 {
		allowedTools = append(allowedTools, cfg.Superpowers.Skills...)
	}

	args := []string{
		"--print",
		"--model", cfg.Models.Worker,
		"--allowed-tools", strings.Join(allowedTools, ","),
		"--disallowed-tools", strings.Join(cfg.Permissions.BlockedTools, ","),
		"--output-format", "json",
	}

	budget := cfg.Limits.TokenBudget.WorkerBudget()
	if budget.Unit == config.USD && budget.Value > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", budget.Value))
	} else if budget.Unit == config.Tokens && budget.Value > 0 {
		args = append(args, "--max-tokens", fmt.Sprintf("%.0f", budget.Value))
	}

	// Render system prompt
	if s.PromptRenderer != nil {
		sysPrompt, err := s.PromptRenderer.RenderSystemPrompt(RoleWorker, nil)
		if err == nil && sysPrompt != "" {
			args = append(args, "--system-prompt", sysPrompt)
		}
	}

	// Render task prompt
	prompt := fmt.Sprintf("Implement task %s: %s", task.ID, task.Title)
	if s.PromptRenderer != nil {
		var retryNotes string
		if len(task.History) > 0 {
			last := task.History[len(task.History)-1]
			retryNotes = last.Notes
		}
		rendered, err := s.PromptRenderer.RenderPrompt(RoleWorker, WorkerPromptData{
			Task:       task,
			FileLocks:  task.FileLocks,
			RetryNotes: retryNotes,
		})
		if err == nil {
			prompt = rendered
		}
	}
	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = task.Worktree

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	applySandboxLimits(cmd, cfg.Sandbox)

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

	if cfg.Planning.Interactive {
		// Remove --print for interactive mode
		args = args[1:]
	}

	budget := cfg.Limits.TokenBudget.PlannerBudget()
	if budget.Unit == config.USD && budget.Value > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", budget.Value))
	} else if budget.Unit == config.Tokens && budget.Value > 0 {
		args = append(args, "--max-tokens", fmt.Sprintf("%.0f", budget.Value))
	}

	// Render system prompt
	if s.PromptRenderer != nil {
		sysPrompt, err := s.PromptRenderer.RenderSystemPrompt(RolePlanner, nil)
		if err == nil && sysPrompt != "" {
			args = append(args, "--system-prompt", sysPrompt)
		}
	}

	// Render task prompt
	prompt := description
	if s.PromptRenderer != nil {
		rendered, err := s.PromptRenderer.RenderPrompt(RolePlanner, PlannerPromptData{
			Description:  description,
			PriorContext: priorContext,
			ProjectName:  cfg.Project.Name,
			BaseBranch:   cfg.Project.BaseBranch,
		})
		if err == nil {
			prompt = rendered
		}
	}
	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = cfg.Project.Repo

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	applySandboxLimits(cmd, cfg.Sandbox)

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
	} else if budget.Unit == config.Tokens && budget.Value > 0 {
		args = append(args, "--max-tokens", fmt.Sprintf("%.0f", budget.Value))
	}

	// Render system prompt
	if s.PromptRenderer != nil {
		sysPrompt, err := s.PromptRenderer.RenderSystemPrompt(RoleValidator, nil)
		if err == nil && sysPrompt != "" {
			args = append(args, "--system-prompt", sysPrompt)
		}
	}

	// Render task prompt
	prompt := fmt.Sprintf("Validate task %s: %s\n\nDiff:\n%s", task.ID, task.Title, diff)
	if s.PromptRenderer != nil {
		var diagCmds []string
		if cfg.Validation.ValidatorDiagnostics.Enabled {
			diagCmds = cfg.Validation.ValidatorDiagnostics.Commands
		}
		rendered, err := s.PromptRenderer.RenderPrompt(RoleValidator, ValidatorPromptData{
			Task:               task,
			Diff:               diff,
			AuditSummary:       auditSummary,
			DiagnosticCommands: diagCmds,
		})
		if err == nil {
			prompt = rendered
		}
	}
	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = task.Worktree

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	applySandboxLimits(cmd, cfg.Sandbox)

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
		"--allowed-tools", "Bash,Read,Glob,Grep",
		"--disallowed-tools", "Write,Edit,WebFetch,WebSearch,NotebookEdit,Task",
		"--output-format", "json",
	}

	budget := cfg.Limits.TokenBudget.MergerBudget()
	if budget.Unit == config.USD && budget.Value > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", budget.Value))
	} else if budget.Unit == config.Tokens && budget.Value > 0 {
		args = append(args, "--max-tokens", fmt.Sprintf("%.0f", budget.Value))
	}

	// Render system prompt
	if s.PromptRenderer != nil {
		sysPrompt, err := s.PromptRenderer.RenderSystemPrompt(RoleMerger, nil)
		if err == nil && sysPrompt != "" {
			args = append(args, "--system-prompt", sysPrompt)
		}
	}

	// Render task prompt
	baseBranch := cfg.Project.BaseBranch
	if baseBranch == "" {
		baseBranch = "main"
	}
	var desc strings.Builder
	fmt.Fprintf(&desc, "Merge the following validated branches into %s:\n", baseBranch)
	for _, b := range branches {
		fmt.Fprintf(&desc, "- %s (task %s: %s)\n", b.Name, b.TaskID, b.TaskTitle)
	}
	prompt := desc.String()
	if s.PromptRenderer != nil {
		rendered, err := s.PromptRenderer.RenderPrompt(RoleMerger, MergerPromptData{
			Branches:   branches,
			BaseBranch: baseBranch,
		})
		if err == nil {
			prompt = rendered
		}
	}
	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = cfg.Project.Repo

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	applySandboxLimits(cmd, cfg.Sandbox)

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
		CostUSD:   output.TotalCostUSD,
		TokensUsed: output.Usage.InputTokens + output.Usage.OutputTokens,
		Duration:  time.Since(agent.Started),
		Err:       err,
	}
	if agent.Task != nil {
		result.TaskID = agent.Task.ID
	}
	return result
}
