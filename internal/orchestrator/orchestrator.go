package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kylegalloway/blueflame/internal/agent"
	"github.com/kylegalloway/blueflame/internal/config"
	"github.com/kylegalloway/blueflame/internal/state"
	"github.com/kylegalloway/blueflame/internal/tasks"
	"github.com/kylegalloway/blueflame/internal/ui"
)

var (
	ErrPlanRejected    = errors.New("plan rejected by user")
	ErrBudgetExceeded  = errors.New("session budget exceeded")
	ErrMaxWaveCycles   = errors.New("max wave cycles reached")
)

// Orchestrator manages the wave-based execution cycle.
type Orchestrator struct {
	config    *config.Config
	spawner   agent.AgentSpawner
	taskStore *tasks.TaskStore
	scheduler *Scheduler
	ui        ui.Prompter
	state     *state.OrchestratorState
	stateMgr  *state.Manager
	lifecycle *agent.LifecycleManager

	sessionCost   float64
	sessionTokens int
}

// New creates a new Orchestrator.
func New(cfg *config.Config, spawner agent.AgentSpawner, prompter ui.Prompter, taskStore *tasks.TaskStore, stateMgr *state.Manager) *Orchestrator {
	concurrency := agent.EffectiveConcurrency(&cfg.Concurrency)
	return &Orchestrator{
		config:    cfg,
		spawner:   spawner,
		taskStore: taskStore,
		scheduler: NewScheduler(concurrency),
		ui:        prompter,
		stateMgr:  stateMgr,
		state: &state.OrchestratorState{
			SessionID: fmt.Sprintf("ses-%s", time.Now().Format("20060102-150405")),
			StartTime: time.Now(),
		},
	}
}

// SetLifecycleManager sets the lifecycle manager for agent tracking.
func (o *Orchestrator) SetLifecycleManager(lm *agent.LifecycleManager) {
	o.lifecycle = lm
}

// Run executes the full wave orchestration loop.
func (o *Orchestrator) Run(ctx context.Context, taskDescription string) error {
	// Start lifecycle monitor if configured
	if o.lifecycle != nil {
		monitorCtx, monitorCancel := context.WithCancel(ctx)
		defer monitorCancel()
		go o.lifecycle.MonitorLoop(monitorCtx)
	}

	// Wave 1: Planning
	o.state.Phase = "planning"
	o.persistState()

	plan, err := o.runPlanning(ctx, taskDescription)
	if err != nil {
		return fmt.Errorf("planning: %w", err)
	}

	// Store planned tasks
	o.taskStore.SetFile(&tasks.TaskFile{
		SchemaVersion: 1,
		SessionID:     o.state.SessionID,
		WaveCycle:     1,
		Tasks:         plan,
	})
	if err := o.taskStore.Save(); err != nil {
		return fmt.Errorf("save tasks: %w", err)
	}

	// Present plan for approval
	decision := o.ui.PlanApproval(len(plan), o.estimateCost(len(plan)))
	switch decision {
	case ui.PlanApprove:
		// Continue
	case ui.PlanAbort:
		return ErrPlanRejected
	case ui.PlanReplan:
		// Re-plan (simplified: just fail for now, full re-plan in production)
		return ErrPlanRejected
	case ui.PlanEdit:
		// Human edits tasks.yaml, then reload
		if err := o.taskStore.Load(); err != nil {
			return fmt.Errorf("reload tasks after edit: %w", err)
		}
	}

	// Wave cycles (development -> validation -> merge -> repeat)
	for cycle := 1; cycle <= o.config.Limits.MaxWaveCycles; cycle++ {
		o.state.WaveCycle = cycle
		o.taskStore.File().WaveCycle = cycle

		// Check budget circuit breaker
		if err := o.checkBudgetCircuitBreaker(); err != nil {
			o.ui.Warn(err.Error())
			break
		}

		// Wave 2: Development
		o.state.Phase = "development"
		o.persistState()
		devResults := o.runDevelopment(ctx)
		o.handleDevelopmentResults(devResults)

		// Wave 3: Validation
		o.state.Phase = "validation"
		o.persistState()
		valResults := o.runValidation(ctx)
		o.handleValidationResults(valResults)

		// Wave 4: Merge
		o.state.Phase = "merge"
		o.persistState()
		changesets := o.collectChangesets()
		approved, requeued := o.presentChangesets(changesets)

		// Check if more work remains
		if !o.hasRemainingTasks() {
			o.ui.Info(fmt.Sprintf("All tasks complete. %d changesets approved.", approved))
			break
		}

		// Session continuation
		sessionState := o.buildSessionState(approved, requeued)
		sessionDecision := o.ui.SessionContinuation(sessionState)
		switch sessionDecision {
		case ui.SessionContinue:
			continue
		case ui.SessionReplan:
			return ErrPlanRejected // Simplified: would re-enter planning
		case ui.SessionStop:
			return nil
		}
	}

	// Clean up state file on successful completion
	if o.stateMgr != nil {
		o.stateMgr.Remove()
	}

	return nil
}

func (o *Orchestrator) runPlanning(ctx context.Context, description string) ([]tasks.Task, error) {
	plannerAgent, err := o.spawner.SpawnPlanner(ctx, description, "", o.config)
	if err != nil {
		return nil, fmt.Errorf("spawn planner: %w", err)
	}

	result := agent.CollectResult(plannerAgent)
	o.accumulateCost(result)

	if result.ExitCode != 0 {
		return nil, fmt.Errorf("planner failed with exit code %d", result.ExitCode)
	}

	planOutput, err := agent.ParsePlannerOutput(result.RawStdout)
	if err != nil {
		return nil, fmt.Errorf("parse planner output: %w", err)
	}

	// Convert planner tasks to task store tasks
	var storeTasks []tasks.Task
	for _, pt := range planOutput.Tasks {
		storeTasks = append(storeTasks, tasks.Task{
			ID:            pt.ID,
			Title:         pt.Title,
			Description:   pt.Description,
			Status:        tasks.StatusPending,
			Priority:      pt.Priority,
			CohesionGroup: pt.CohesionGroup,
			Dependencies:  pt.Dependencies,
			FileLocks:     pt.FileLocks,
		})
	}

	// Validate dependencies
	if err := tasks.ValidateDependencies(storeTasks); err != nil {
		return nil, fmt.Errorf("invalid task dependencies: %w", err)
	}

	return storeTasks, nil
}

func (o *Orchestrator) runDevelopment(ctx context.Context) []agent.AgentResult {
	allTasks := o.taskStore.Tasks()
	ready := o.scheduler.ReadyTasks(allTasks)

	if len(ready) == 0 {
		return nil
	}

	var results []agent.AgentResult
	resultCh := make(chan agent.AgentResult, len(ready))

	for i := range ready {
		task := o.taskStore.FindTask(ready[i].ID)
		if task == nil {
			continue
		}

		agentID := fmt.Sprintf("worker-%08x", time.Now().UnixNano()&0xFFFFFFFF)
		if err := task.Claim(agentID, "/tmp/wt-"+task.ID, "blueflame/"+task.ID); err != nil {
			continue
		}

		workerAgent, err := o.spawner.SpawnWorker(ctx, task, o.config)
		if err != nil {
			task.Status = tasks.StatusPending // Unclaim on spawn failure
			task.AgentID = ""
			continue
		}

		// Register agent with lifecycle tracker
		if o.lifecycle != nil {
			o.lifecycle.Register(workerAgent)
		}

		go func(a *agent.Agent) {
			result := agent.CollectResult(a)
			if o.lifecycle != nil {
				o.lifecycle.Unregister(a.ID, result)
			}
			resultCh <- result
		}(workerAgent)
	}

	// Collect results
	for i := 0; i < cap(resultCh); i++ {
		select {
		case result := <-resultCh:
			results = append(results, result)
		case <-ctx.Done():
			return results
		}
	}

	return results
}

func (o *Orchestrator) handleDevelopmentResults(results []agent.AgentResult) {
	for _, result := range results {
		o.accumulateCost(result)

		task := o.taskStore.FindTask(result.TaskID)
		if task == nil {
			continue
		}

		if result.ExitCode == 0 {
			task.Complete()
		} else {
			task.Fail(fmt.Sprintf("exit code %d", result.ExitCode))

			// Check retries
			if task.RetryCount < o.config.Limits.MaxRetries {
				task.Requeue("automatic retry", tasks.HistoryEntry{
					Attempt:   task.RetryCount + 1,
					AgentID:   result.AgentID,
					Timestamp: time.Now(),
					Result:    "failed",
					Notes:     fmt.Sprintf("exit code %d", result.ExitCode),
					CostUSD:   result.CostUSD,
					TokensUsed: result.TokensUsed,
				})
			} else {
				// Cascade failure to dependents
				tasks.CascadeFailure(task.ID, o.taskStore.Tasks())
			}
		}
	}
}

func (o *Orchestrator) runValidation(ctx context.Context) []agent.AgentResult {
	allTasks := o.taskStore.Tasks()
	var results []agent.AgentResult

	for i := range allTasks {
		task := &allTasks[i]
		if task.Status != tasks.StatusDone {
			continue
		}

		valAgent, err := o.spawner.SpawnValidator(ctx, task, "", "", o.config)
		if err != nil {
			continue
		}

		result := agent.CollectResult(valAgent)
		results = append(results, result)
	}

	return results
}

func (o *Orchestrator) handleValidationResults(results []agent.AgentResult) {
	for _, result := range results {
		o.accumulateCost(result)

		task := o.taskStore.FindTask(result.TaskID)
		if task == nil {
			continue
		}

		valOutput, err := agent.ParseValidatorOutput(result.RawStdout)
		if err != nil {
			task.SetValidationResult("fail", "validator output parse error: "+err.Error())
			continue
		}

		task.SetValidationResult(valOutput.Status, valOutput.Notes)
	}
}

// Changeset represents a group of validated task branches.
type Changeset struct {
	CohesionGroup string
	TaskIDs       []string
	Description   string
}

func (o *Orchestrator) collectChangesets() []Changeset {
	allTasks := o.taskStore.Tasks()
	groups := make(map[string]*Changeset)

	for _, task := range allTasks {
		if task.Status != tasks.StatusDone || task.Result.Status != "pass" {
			continue
		}
		group := task.CohesionGroup
		if group == "" {
			group = "default"
		}
		if groups[group] == nil {
			groups[group] = &Changeset{CohesionGroup: group}
		}
		groups[group].TaskIDs = append(groups[group].TaskIDs, task.ID)
		groups[group].Description += task.Title + "; "
	}

	var result []Changeset
	for _, cs := range groups {
		result = append(result, *cs)
	}
	return result
}

func (o *Orchestrator) presentChangesets(changesets []Changeset) (approved, requeued int) {
	for i, cs := range changesets {
		info := ui.ChangesetInfo{
			Index:         i + 1,
			Total:         len(changesets),
			CohesionGroup: cs.CohesionGroup,
			Description:   cs.Description,
			TaskIDs:       cs.TaskIDs,
		}

		decision, reason := o.ui.ChangesetReview(info)
		switch decision {
		case ui.ChangesetApprove:
			approved++
			for _, taskID := range cs.TaskIDs {
				if task := o.taskStore.FindTask(taskID); task != nil {
					task.Status = tasks.StatusMerged
				}
			}
		case ui.ChangesetReject:
			requeued += len(cs.TaskIDs)
			for _, taskID := range cs.TaskIDs {
				if task := o.taskStore.FindTask(taskID); task != nil {
					task.Requeue("changeset rejected", tasks.HistoryEntry{
						Attempt:         task.RetryCount + 1,
						Timestamp:       time.Now(),
						Result:          "rejected",
						RejectionReason: reason,
					})
				}
			}
		case ui.ChangesetSkip:
			// Deferred to next wave cycle
		}
	}
	return
}

func (o *Orchestrator) hasRemainingTasks() bool {
	for _, task := range o.taskStore.Tasks() {
		if task.Status == tasks.StatusPending || task.Status == tasks.StatusDone {
			return true
		}
	}
	return false
}

func (o *Orchestrator) checkBudgetCircuitBreaker() error {
	if limit := o.config.Limits.MaxSessionCostUSD; limit > 0 {
		if o.sessionCost >= limit {
			return fmt.Errorf("session cost $%.2f exceeds limit $%.2f", o.sessionCost, limit)
		}
	}
	if limit := o.config.Limits.MaxSessionTokens; limit > 0 {
		if o.sessionTokens >= limit {
			return fmt.Errorf("session tokens %d exceeds limit %d", o.sessionTokens, limit)
		}
	}
	return nil
}

func (o *Orchestrator) accumulateCost(result agent.AgentResult) {
	o.sessionCost += result.CostUSD
	o.sessionTokens += result.TokensUsed
	o.state.SessionCost = o.sessionCost
	o.state.SessionTokens = o.sessionTokens
}

func (o *Orchestrator) persistState() {
	if o.stateMgr != nil {
		o.stateMgr.Save(o.state)
	}
}

func (o *Orchestrator) estimateCost(taskCount int) string {
	low := float64(taskCount) * 0.50
	high := float64(taskCount) * 3.00
	return fmt.Sprintf("$%.2f - $%.2f", low, high)
}

func (o *Orchestrator) buildSessionState(approved, requeued int) ui.SessionState {
	var requeuedTasks []string
	var blocked int
	for _, task := range o.taskStore.Tasks() {
		if task.Status == tasks.StatusPending && task.RetryCount > 0 {
			requeuedTasks = append(requeuedTasks, task.ID)
		}
		if task.Status == tasks.StatusBlocked {
			blocked++
		}
	}

	return ui.SessionState{
		WaveCycle:     o.state.WaveCycle,
		Approved:      approved,
		Requeued:      requeued,
		Blocked:       blocked,
		TotalCost:     o.sessionCost,
		CostLimit:     o.config.Limits.MaxSessionCostUSD,
		TokensUsed:    o.sessionTokens,
		TokenLimit:    o.config.Limits.MaxSessionTokens,
		RequeuedTasks: requeuedTasks,
	}
}

// HandleShutdown gracefully terminates all running agents and persists state.
func (o *Orchestrator) HandleShutdown() {
	if o.lifecycle != nil {
		o.lifecycle.GracefulShutdown(10 * time.Second)
	}
	o.persistState()
}
