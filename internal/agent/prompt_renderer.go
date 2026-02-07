package agent

import (
	"fmt"
	"strings"

	"github.com/kylegalloway/blueflame/internal/tasks"
)

// PlannerPromptData holds data for rendering planner prompts.
type PlannerPromptData struct {
	Description  string
	PriorContext string
	ProjectName  string
	BaseBranch   string
}

// WorkerPromptData holds data for rendering worker prompts.
type WorkerPromptData struct {
	Task       *tasks.Task
	FileLocks  []string
	RetryNotes string
}

// ValidatorPromptData holds data for rendering validator prompts.
type ValidatorPromptData struct {
	Task         *tasks.Task
	Diff         string
	AuditSummary string
}

// MergerPromptData holds data for rendering merger prompts.
type MergerPromptData struct {
	Branches []BranchInfo
}

// DefaultPromptRenderer implements PromptRenderer with built-in templates.
type DefaultPromptRenderer struct{}

func (r *DefaultPromptRenderer) RenderPrompt(role string, data interface{}) (string, error) {
	switch role {
	case RolePlanner:
		d, ok := data.(PlannerPromptData)
		if !ok {
			return "", fmt.Errorf("invalid data type for planner prompt")
		}
		return renderPlannerPrompt(d), nil
	case RoleWorker:
		d, ok := data.(WorkerPromptData)
		if !ok {
			return "", fmt.Errorf("invalid data type for worker prompt")
		}
		return renderWorkerPrompt(d), nil
	case RoleValidator:
		d, ok := data.(ValidatorPromptData)
		if !ok {
			return "", fmt.Errorf("invalid data type for validator prompt")
		}
		return renderValidatorPrompt(d), nil
	case RoleMerger:
		d, ok := data.(MergerPromptData)
		if !ok {
			return "", fmt.Errorf("invalid data type for merger prompt")
		}
		return renderMergerPrompt(d), nil
	default:
		return "", fmt.Errorf("unknown role: %s", role)
	}
}

func (r *DefaultPromptRenderer) RenderSystemPrompt(role string, data interface{}) (string, error) {
	switch role {
	case RolePlanner:
		return plannerSystemPrompt, nil
	case RoleWorker:
		return workerSystemPrompt, nil
	case RoleValidator:
		return validatorSystemPrompt, nil
	case RoleMerger:
		return mergerSystemPrompt, nil
	default:
		return "", fmt.Errorf("unknown role: %s", role)
	}
}

const plannerSystemPrompt = `You are a planning agent for a multi-agent development system. Your job is to decompose a task description into independent, parallelizable sub-tasks.

Output a JSON object with a "tasks" array. Each task has:
- id: unique identifier (e.g. "task-001")
- title: short description
- description: detailed implementation instructions
- priority: integer (1 = highest)
- cohesion_group: group name for tasks that must be merged together (optional)
- dependencies: array of task IDs this task depends on
- file_locks: array of file/directory paths this task will modify

Minimize dependencies between tasks to maximize parallelism. Each task should be independently implementable and testable.`

const workerSystemPrompt = `You are a development agent. Implement the assigned task completely, including tests. Follow the project's existing patterns and conventions.

Constraints:
- Only modify files within your declared file_locks scope
- Create meaningful commits with clear messages
- Run tests to verify your changes work
- Do not modify files outside your assigned scope`

const validatorSystemPrompt = `You are a validation agent. Review the changes made by a development agent.

Output a JSON object with:
- status: "pass" or "fail"
- notes: explanation of your assessment
- issues: array of specific issues found (if any)

Check for:
- Correctness: Does the code do what the task requires?
- Tests: Are there adequate tests?
- Style: Does it follow project conventions?
- Safety: Are there security concerns?`

const mergerSystemPrompt = `You are a merge agent. Merge the validated branches into the base branch.

For each branch:
1. Check for merge conflicts
2. Resolve conflicts if possible
3. Verify tests pass after merge
4. Create a merge commit

If conflicts cannot be resolved automatically, report them.`

func renderPlannerPrompt(d PlannerPromptData) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Decompose the following task into parallelizable sub-tasks:\n\n%s", d.Description)
	if d.PriorContext != "" {
		fmt.Fprintf(&b, "\n\nContext from prior sessions:\n%s", d.PriorContext)
	}
	return b.String()
}

func renderWorkerPrompt(d WorkerPromptData) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Implement task %s: %s\n\n%s", d.Task.ID, d.Task.Title, d.Task.Description)
	if len(d.FileLocks) > 0 {
		fmt.Fprintf(&b, "\n\nYou may only modify files in: %s", strings.Join(d.FileLocks, ", "))
	}
	if d.RetryNotes != "" {
		fmt.Fprintf(&b, "\n\nPrevious attempt notes:\n%s", d.RetryNotes)
	}
	return b.String()
}

func renderValidatorPrompt(d ValidatorPromptData) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Validate task %s: %s\n\nDiff:\n%s", d.Task.ID, d.Task.Title, d.Diff)
	if d.AuditSummary != "" {
		fmt.Fprintf(&b, "\n\nAudit summary:\n%s", d.AuditSummary)
	}
	return b.String()
}

func renderMergerPrompt(d MergerPromptData) string {
	var b strings.Builder
	b.WriteString("Merge the following validated branches:\n")
	for _, br := range d.Branches {
		fmt.Fprintf(&b, "- %s (task %s: %s)\n", br.Name, br.TaskID, br.TaskTitle)
	}
	return b.String()
}
