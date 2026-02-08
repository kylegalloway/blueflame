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
	Task               *tasks.Task
	Diff               string
	AuditSummary       string
	DiagnosticCommands []string
}

// MergerPromptData holds data for rendering merger prompts.
type MergerPromptData struct {
	Branches   []BranchInfo
	BaseBranch string
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

IMPORTANT: You MUST commit your changes using git before finishing:
1. git add the files you created or modified
2. git commit -m "feat(task-id): description of changes"

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

const mergerSystemPrompt = `You are a merge agent. Your job is to merge validated feature branches into the base branch using git.

Workflow:
1. Run "git checkout <base_branch>" to ensure you are on the target branch
2. For each feature branch, run "git merge <branch_name>"
3. If there are merge conflicts, resolve them and commit
4. After all merges, verify the build still passes

Do NOT create new branches. Merge directly into the base branch specified in the prompt.`

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
	if len(d.DiagnosticCommands) > 0 {
		fmt.Fprintf(&b, "\n\nDiagnostic commands to run:\n")
		for _, cmd := range d.DiagnosticCommands {
			fmt.Fprintf(&b, "- %s\n", cmd)
		}
	}
	return b.String()
}

func renderMergerPrompt(d MergerPromptData) string {
	var b strings.Builder
	baseBranch := d.BaseBranch
	if baseBranch == "" {
		baseBranch = "main"
	}
	fmt.Fprintf(&b, "Merge the following validated branches into %s:\n", baseBranch)
	for _, br := range d.Branches {
		fmt.Fprintf(&b, "- %s (task %s: %s)\n", br.Name, br.TaskID, br.TaskTitle)
	}
	fmt.Fprintf(&b, "\nSteps:\n")
	fmt.Fprintf(&b, "1. git checkout %s\n", baseBranch)
	for i, br := range d.Branches {
		fmt.Fprintf(&b, "%d. git merge %s\n", i+2, br.Name)
	}
	fmt.Fprintf(&b, "%d. Resolve any conflicts\n", len(d.Branches)+2)
	return b.String()
}
