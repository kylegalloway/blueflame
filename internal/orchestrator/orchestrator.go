package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/kylegalloway/blueflame/internal/agent"
	"github.com/kylegalloway/blueflame/internal/config"
	"github.com/kylegalloway/blueflame/internal/locks"
	"github.com/kylegalloway/blueflame/internal/memory"
	"github.com/kylegalloway/blueflame/internal/state"
	"github.com/kylegalloway/blueflame/internal/tasks"
	"github.com/kylegalloway/blueflame/internal/ui"
	"github.com/kylegalloway/blueflame/internal/worktree"
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
	worktrees *worktree.Manager
	locks     *locks.Manager
	memory      memory.Provider
	hooksDir    string
	watcherTmpl *template.Template

	sessionCost   float64
	sessionTokens int

	// agentLocks tracks which lock paths each agent holds, for per-agent release.
	agentLocks map[string][]string

	// recoveryState holds crash recovery state when resuming a previous session.
	recoveryState *state.OrchestratorState
}

// New creates a new Orchestrator.
func New(cfg *config.Config, spawner agent.AgentSpawner, prompter ui.Prompter, taskStore *tasks.TaskStore, stateMgr *state.Manager) *Orchestrator {
	concurrency := agent.EffectiveConcurrency(&cfg.Concurrency)
	return &Orchestrator{
		config:     cfg,
		spawner:    spawner,
		taskStore:  taskStore,
		scheduler:  NewScheduler(concurrency),
		ui:         prompter,
		stateMgr:   stateMgr,
		agentLocks: make(map[string][]string),
		state: &state.OrchestratorState{
			SessionID: fmt.Sprintf("ses-%s", time.Now().Format("20060102-150405")),
			StartTime: time.Now(),
		},
	}
}

// SetWorktreeManager sets the worktree manager for creating agent worktrees.
func (o *Orchestrator) SetWorktreeManager(wm *worktree.Manager) {
	o.worktrees = wm
}

// SetLockManager sets the lock manager for file lock acquisition.
func (o *Orchestrator) SetLockManager(lm *locks.Manager) {
	o.locks = lm
}

// SetHooksDir sets the base directory for generated hook scripts and the watcher template.
func (o *Orchestrator) SetHooksDir(dir string, tmpl *template.Template) {
	o.hooksDir = dir
	o.watcherTmpl = tmpl
}

// SetMemoryProvider sets the memory provider for cross-session context.
func (o *Orchestrator) SetMemoryProvider(mp memory.Provider) {
	o.memory = mp
}

// SetLifecycleManager sets the lifecycle manager for agent tracking.
func (o *Orchestrator) SetLifecycleManager(lm *agent.LifecycleManager) {
	o.lifecycle = lm
}

// SetRecoveryState sets crash recovery state. When set, Run() will skip
// planning and resume at the recovered wave cycle.
func (o *Orchestrator) SetRecoveryState(rs *state.OrchestratorState) {
	o.recoveryState = rs
}

// Run executes the full wave orchestration loop.
func (o *Orchestrator) Run(ctx context.Context, taskDescription string) error {
	// Start lifecycle monitor if configured
	if o.lifecycle != nil {
		monitorCtx, monitorCancel := context.WithCancel(ctx)
		defer monitorCancel()
		go o.lifecycle.MonitorLoop(monitorCtx)
	}

	startCycle := 1

	if o.recoveryState != nil {
		// Crash recovery: skip planning, restore state, reset claimed tasks
		o.state.SessionID = o.recoveryState.SessionID
		o.state.StartTime = o.recoveryState.StartTime
		o.sessionCost = o.recoveryState.SessionCost
		o.sessionTokens = o.recoveryState.SessionTokens
		o.state.SessionCost = o.recoveryState.SessionCost
		o.state.SessionTokens = o.recoveryState.SessionTokens
		startCycle = o.recoveryState.WaveCycle

		// Load tasks from disk (already persisted from previous session)
		if err := o.taskStore.Load(); err != nil {
			return fmt.Errorf("load tasks for recovery: %w", err)
		}

		// Reset any claimed tasks back to pending (their agents are dead)
		o.taskStore.ResetClaimedTasks()
		if err := o.taskStore.Save(); err != nil {
			return fmt.Errorf("save tasks after reset: %w", err)
		}

		o.ui.Info(fmt.Sprintf("Resuming session %s from wave cycle %d", o.state.SessionID, startCycle))
		o.ui.Info(fmt.Sprintf("  Accumulated cost: $%.2f (%d tokens)", o.sessionCost, o.sessionTokens))

		taskSummary := o.taskStore.Tasks()
		var pending, done, failed, merged int
		for _, t := range taskSummary {
			switch t.Status {
			case tasks.StatusPending:
				pending++
			case tasks.StatusDone:
				done++
			case tasks.StatusFailed:
				failed++
			case tasks.StatusMerged:
				merged++
			}
		}
		o.ui.Info(fmt.Sprintf("  Tasks: %d pending, %d done, %d failed, %d merged",
			pending, done, failed, merged))
	} else {
		// Normal path: run planning
		o.state.Phase = "planning"
		o.persistState()

		// Load prior session context for planner
		var priorContext string
		if o.memory != nil {
			if memCtx, loadErr := o.memory.Load(); loadErr == nil && memCtx.SessionCount > 0 {
				priorContext = fmt.Sprintf("Prior sessions: %d. Prior failures: %d.",
					memCtx.SessionCount, len(memCtx.PriorFailures))
				for _, f := range memCtx.PriorFailures {
					priorContext += fmt.Sprintf("\n- Task %s (%s): %s", f.ID, f.Title, f.FailureReason)
				}
			}
		}

		for {
			plan, err := o.runPlanning(ctx, taskDescription, priorContext)
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

			// Display the plan
			o.ui.Info(fmt.Sprintf("\nPlanned %d task(s), estimated cost: %s\n", len(plan), o.estimateCost(len(plan))))
			for i, t := range plan {
				deps := "none"
				if len(t.Dependencies) > 0 {
					deps = strings.Join(t.Dependencies, ", ")
				}
				locks := "none"
				if len(t.FileLocks) > 0 {
					locks = strings.Join(t.FileLocks, ", ")
				}
				o.ui.Info(fmt.Sprintf("  %d. [%s] %s (priority %d)", i+1, t.ID, t.Title, t.Priority))
				o.ui.Info(fmt.Sprintf("     %s", t.Description))
				o.ui.Info(fmt.Sprintf("     deps: %s | locks: %s", deps, locks))
			}

			// Present plan for approval
			decision, feedback := o.ui.PlanApproval(len(plan), o.estimateCost(len(plan)))
			switch decision {
			case ui.PlanApprove:
				// Continue to development
			case ui.PlanAbort:
				return ErrPlanRejected
			case ui.PlanReplan:
				o.ui.Info("Re-planning...")
				if feedback != "" {
					priorContext += fmt.Sprintf("\n\nUser feedback on previous plan: %s", feedback)
				}
				continue
			case ui.PlanEdit:
				// Human edits tasks.yaml, then reload
				if err := o.taskStore.Load(); err != nil {
					return fmt.Errorf("reload tasks after edit: %w", err)
				}
			}
			break
		}
	}

	// Wave cycles (development -> validation -> merge -> repeat)
	for cycle := startCycle; cycle <= o.config.Limits.MaxWaveCycles; cycle++ {
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
		if err := o.taskStore.Save(); err != nil {
			o.ui.Warn(fmt.Sprintf("save tasks after development: %v", err))
		}

		// Wave 3: Validation
		o.state.Phase = "validation"
		o.persistState()
		valResults := o.runValidation(ctx)
		o.handleValidationResults(valResults)
		if err := o.taskStore.Save(); err != nil {
			o.ui.Warn(fmt.Sprintf("save tasks after validation: %v", err))
		}

		// Wave 4: Merge
		o.state.Phase = "merge"
		o.persistState()
		changesets := o.collectChangesets()
		approved, requeued := o.presentChangesets(ctx, changesets)
		if err := o.taskStore.Save(); err != nil {
			o.ui.Warn(fmt.Sprintf("save tasks after merge: %v", err))
		}

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

	// Save session to memory provider
	if o.memory != nil {
		o.saveSessionMemory()
	}

	// Clean up state file on successful completion
	if o.stateMgr != nil {
		o.stateMgr.Remove()
	}

	return nil
}

func (o *Orchestrator) saveSessionMemory() {
	allTasks := o.taskStore.Tasks()
	result := memory.SessionResult{
		ID:           o.state.SessionID,
		TotalCostUSD: o.sessionCost,
		TotalTokens:  o.sessionTokens,
		WaveCycles:   o.state.WaveCycle,
	}
	for _, t := range allTasks {
		summary := memory.TaskSummary{
			ID:           t.ID,
			Title:        t.Title,
			ResultStatus: string(t.Status),
			RetryCount:   t.RetryCount,
		}
		if t.Result.Status != "" {
			summary.ValidatorNotes = t.Result.Notes
		}
		result.AllTasks = append(result.AllTasks, summary)
		switch t.Status {
		case tasks.StatusMerged, tasks.StatusDone:
			result.CompletedTasks = append(result.CompletedTasks, summary)
		case tasks.StatusFailed:
			summary.FailureReason = t.Result.Notes
			result.FailedTasks = append(result.FailedTasks, summary)
		}
	}
	if err := o.memory.Save(result); err != nil {
		o.ui.Warn(fmt.Sprintf("save session memory: %v", err))
	}
}

func (o *Orchestrator) runPlanning(ctx context.Context, description string, priorContext string) ([]tasks.Task, error) {
	plannerAgent, err := o.spawner.SpawnPlanner(ctx, description, priorContext, o.config)
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
	spawned := 0
	resultCh := make(chan agent.AgentResult, len(ready))

	for i := range ready {
		task := o.taskStore.FindTask(ready[i].ID)
		if task == nil {
			continue
		}

		agentID := fmt.Sprintf("worker-%08x", time.Now().UnixNano()&0xFFFFFFFF)
		branch := worktree.BranchName(task.ID)

		// Create worktree
		var wtPath string
		if o.worktrees != nil {
			var err error
			wtPath, _, err = o.worktrees.Create(agentID, task.ID)
			if err != nil {
				o.ui.Warn(fmt.Sprintf("worktree create for %s: %v", task.ID, err))
				continue
			}
		} else {
			wtPath = "/tmp/wt-" + task.ID
		}

		// Acquire file locks
		if o.locks != nil && len(task.FileLocks) > 0 {
			if err := o.locks.Acquire(agentID, task.FileLocks); err != nil {
				o.ui.Warn(fmt.Sprintf("lock conflict for %s: %v", task.ID, err))
				// Rollback worktree
				if o.worktrees != nil {
					o.worktrees.Remove(agentID)
				}
				continue
			}
			o.agentLocks[agentID] = task.FileLocks
		}

		// Generate watcher hooks and .claude/settings.json
		if o.hooksDir != "" {
			hookData := agent.BuildWatcherData(agentID, agent.RoleWorker, task, o.config, o.hooksDir)
			hookPath := fmt.Sprintf("%s/%s-watcher.sh", o.hooksDir, agentID)
			if err := agent.GenerateWatcherHookFromTemplate(o.watcherTmpl, hookData, hookPath); err != nil {
				// Non-fatal: log and continue without hooks
				o.ui.Warn(fmt.Sprintf("generate hooks for %s: %v", task.ID, err))
			} else {
				if err := agent.GenerateAgentSettings(wtPath, hookPath); err != nil {
					o.ui.Warn(fmt.Sprintf("generate settings for %s: %v", task.ID, err))
				}
			}
		}

		if err := task.Claim(agentID, wtPath, branch); err != nil {
			// Rollback
			if o.locks != nil {
				o.locks.Release(agentID)
				delete(o.agentLocks, agentID)
			}
			if o.worktrees != nil {
				o.worktrees.Remove(agentID)
			}
			continue
		}

		workerAgent, err := o.spawner.SpawnWorker(ctx, task, o.config)
		if err != nil {
			task.Status = tasks.StatusPending
			task.AgentID = ""
			// Rollback
			if o.locks != nil {
				o.locks.Release(agentID)
				delete(o.agentLocks, agentID)
			}
			if o.worktrees != nil {
				o.worktrees.Remove(agentID)
			}
			continue
		}

		// Register agent with lifecycle tracker
		if o.lifecycle != nil {
			o.lifecycle.Register(workerAgent)
		}

		spawned++
		go func(a *agent.Agent) {
			result := agent.CollectResult(a)
			if o.lifecycle != nil {
				o.lifecycle.Unregister(a.ID, result)
			}
			resultCh <- result
		}(workerAgent)
	}

	// Collect results
	for i := 0; i < spawned; i++ {
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

		// Release per-agent locks
		o.releaseAgentLocks(result.AgentID)

		if result.ExitCode == 0 {
			// Run postcheck to validate filesystem changes
			postResult, err := agent.PostCheck(task, o.config)
			if err != nil {
				o.ui.Warn(fmt.Sprintf("postcheck error for %s: %v", task.ID, err))
				task.Complete()
			} else if !postResult.Pass {
				var violations []string
				for _, v := range postResult.Violations {
					violations = append(violations, fmt.Sprintf("%s: %s", v.Type, v.Path))
				}
				task.Fail(fmt.Sprintf("postcheck violations: %v", violations))
				if task.RetryCount < o.config.Limits.MaxRetries {
					task.Requeue("postcheck failure", tasks.HistoryEntry{
						Attempt:    task.RetryCount + 1,
						AgentID:    result.AgentID,
						Timestamp:  time.Now(),
						Result:     "postcheck_failed",
						Notes:      fmt.Sprintf("violations: %v", violations),
						CostUSD:    result.CostUSD,
						TokensUsed: result.TokensUsed,
					})
				}
			} else {
				task.Complete()
			}
		} else {
			task.Fail(fmt.Sprintf("exit code %d", result.ExitCode))

			// Check retries
			if task.RetryCount < o.config.Limits.MaxRetries {
				task.Requeue("automatic retry", tasks.HistoryEntry{
					Attempt:    task.RetryCount + 1,
					AgentID:    result.AgentID,
					Timestamp:  time.Now(),
					Result:     "failed",
					Notes:      fmt.Sprintf("exit code %d", result.ExitCode),
					CostUSD:    result.CostUSD,
					TokensUsed: result.TokensUsed,
				})
			} else {
				// Cascade failure to dependents
				tasks.CascadeFailure(task.ID, o.taskStore.Tasks())
			}
		}
	}
}

// releaseAgentLocks releases file locks held by a specific agent.
func (o *Orchestrator) releaseAgentLocks(agentID string) {
	if o.locks == nil {
		return
	}
	if _, ok := o.agentLocks[agentID]; ok {
		o.locks.Release(agentID)
		delete(o.agentLocks, agentID)
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

		if result.ExitCode != 0 {
			decision := o.ui.ValidatorFailed(task.ID, fmt.Errorf("exit code %d", result.ExitCode))
			switch decision {
			case ui.ValidatorRetryTask:
				task.Requeue("validator retry", tasks.HistoryEntry{
					Attempt:   task.RetryCount + 1,
					AgentID:   result.AgentID,
					Timestamp: time.Now(),
					Result:    "validator_failed",
					Notes:     fmt.Sprintf("exit code %d, user chose retry", result.ExitCode),
				})
			case ui.ValidatorSkipTask:
				task.Fail("validator failed, skipped by user")
			case ui.ValidatorManualReview:
				task.SetValidationResult("manual_review", "awaiting manual review")
			}
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

func (o *Orchestrator) presentChangesets(ctx context.Context, changesets []Changeset) (approved, requeued int) {
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
			// Spawn merger to actually merge the branches
			var branches []agent.BranchInfo
			for _, taskID := range cs.TaskIDs {
				if task := o.taskStore.FindTask(taskID); task != nil {
					branches = append(branches, agent.BranchInfo{
						Name:      task.Branch,
						TaskID:    task.ID,
						TaskTitle: task.Title,
					})
				}
			}

			if len(branches) > 0 {
				mergerAgent, err := o.spawner.SpawnMerger(ctx, branches, o.config)
				if err != nil {
					o.ui.Warn(fmt.Sprintf("merge failed for group %s: %v", cs.CohesionGroup, err))
					continue
				}
				mergeResult := agent.CollectResult(mergerAgent)
				o.accumulateCost(mergeResult)

				if mergeResult.ExitCode != 0 {
					o.ui.Warn(fmt.Sprintf("merger exited %d for group %s", mergeResult.ExitCode, cs.CohesionGroup))
					continue
				}
			}

			approved++
			for _, taskID := range cs.TaskIDs {
				if task := o.taskStore.FindTask(taskID); task != nil {
					task.Status = tasks.StatusMerged
					// Clean up worktree, merge branch to base, then delete branch
					if o.worktrees != nil && task.AgentID != "" {
						if err := o.worktrees.Remove(task.AgentID); err != nil {
							o.ui.Warn(fmt.Sprintf("remove worktree for %s: %v", task.ID, err))
						}
						if err := o.worktrees.MergeBranch(task.ID); err != nil {
							o.ui.Warn(fmt.Sprintf("merge branch for %s: %v", task.ID, err))
						}
						if err := o.worktrees.RemoveBranch(task.ID); err != nil {
							o.ui.Warn(fmt.Sprintf("remove branch for %s: %v", task.ID, err))
						}
					}
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

// SessionSummary returns the accumulated session results for cost summary display.
func (o *Orchestrator) SessionSummary() ui.CostSummary {
	allTasks := o.taskStore.Tasks()
	var completed, failed, merged int
	for _, t := range allTasks {
		switch t.Status {
		case tasks.StatusDone:
			completed++
		case tasks.StatusFailed:
			failed++
		case tasks.StatusMerged:
			merged++
		}
	}
	return ui.CostSummary{
		SessionID:      o.state.SessionID,
		TotalCost:      o.sessionCost,
		TotalTokens:    o.sessionTokens,
		WaveCycles:     o.state.WaveCycle,
		TasksCompleted: completed,
		TasksFailed:    failed,
		TasksMerged:    merged,
	}
}

// HandleShutdown gracefully terminates all running agents and persists state.
func (o *Orchestrator) HandleShutdown() {
	if o.lifecycle != nil {
		o.lifecycle.GracefulShutdown(10 * time.Second)
	}
	o.persistState()
}
