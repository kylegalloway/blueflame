# Blue Flame: Incomplete / Stubbed / Unfinished Items

## Critical (orchestrator can't actually run agents correctly)

- [x] **Worktree/lock/hook wiring missing** — `runDevelopment()` now calls `worktree.Manager.Create()`, acquires file locks via `locks.Manager.Acquire()`, generates watcher hooks and `.claude/settings.json`, with rollback on failure.

- [x] **No postcheck after worker completion** — `agent.PostCheck()` is now called in `handleDevelopmentResults()`. Failed postchecks fail the task with violation details and requeue if retries remain.

- [x] **No per-agent lock release** — `locks.Manager.Release()` now tracks locks per-agent and releases only that agent's locks. Orchestrator calls `releaseAgentLocks()` after each worker completes.

- [x] **Merge phase is a no-op** — `SpawnMerger()` is now called in `presentChangesets()` for approved changesets before marking tasks as merged.

- [x] **PromptRenderer unimplemented** — `DefaultPromptRenderer` implemented with built-in templates for all roles. System prompts and task prompts rendered per-role.

- [x] **Missing claude CLI flags** — `--system-prompt`, `--max-tokens` (for token budgets), and interactive planning mode (without `--print`) now wired in all spawner methods.

## High (significant feature gaps)

- [x] **Memory provider not wired** — Memory provider wired into orchestrator. Planner receives prior session context on startup. Session results saved after completion. Main.go selects BeadsProvider or NoopProvider based on config.

- [x] **Validator failure handling** — `ui.ValidatorFailed()` now called when validator exits non-zero. Supports retry, skip, and manual review decisions.

- [ ] **Changeset ordering ignores inter-group dependencies** — Iterates a map (random order). No auto-deferral when a dependency group is rejected. (`internal/orchestrator/orchestrator.go`)

- [ ] **Single-batch development** — Deferred tasks (lock conflicts) wait until next wave cycle instead of being re-spawned within the same wave.

- [ ] **Linux sandbox stub** — Only `Setpgid` + `CLONE_NEWNET`. No cgroups, no rlimits for CPU/memory/files. (`internal/agent/sandbox_linux.go`)

- [ ] **macOS sandbox stub** — CPU time, file size, and open file rlimits are configured but never applied. (`internal/agent/sandbox_darwin.go`)

- [x] **Cost summary empty** — `SessionSummary()` method added to orchestrator. `main.go` uses actual session data for cost summary display.

## Medium (polish / completeness)

- [ ] **Session re-plan is a stub** — `PlanReplan` works (appends feedback to prior context, re-enters planning loop), but `SessionReplan` just returns `ErrPlanRejected` instead of re-entering the planning phase mid-session. (`internal/orchestrator/orchestrator.go:212`)

- [x] **Crash recovery resume** — CrashRecoveryPrompt in Prompter interface. Task.ResetClaimed() + TaskStore.ResetClaimedTasks() for dead agent cleanup. Orchestrator.SetRecoveryState() skips planning, restores session state, resumes at wave cycle.

- [ ] **Lifecycle hooks not invoked** — `post_plan`, `pre_validation`, `post_merge`, `on_failure` scripts are parsed from config but never executed.

- [ ] **Superpowers unused** — Config parsed but never passed to agents.

- [ ] **Audit log infrastructure** — No directory creation, no log rotation, no retention policy, no audit summary generation for validators.

- [x] **Interactive planning** — Planner spawner now checks `cfg.Planning.Interactive` and omits `--print` flag when interactive mode is enabled.

- [ ] **No worktree cleanup on success** — Worktrees only cleaned via `blueflame cleanup` command, not on successful session completion.

## High-Medium (functional gaps in implemented code)

- [ ] **Validator receives empty diff and audit summary** — `runValidation()` passes empty strings for both `diff` and `auditSummary` to `SpawnValidator()`. Validators have no context about what the worker actually changed. Should compute `git diff base..branch` and summarize the agent's audit log. (`internal/orchestrator/orchestrator.go:502`)

- [ ] **PostCheck silently passes when no commits exist** — `gitDiffNameStatus()` returns `nil, nil` when git diff fails (e.g., no commits on the branch), causing postcheck to pass silently instead of flagging that the worker produced no output. (`internal/agent/postcheck.go:65`)

## Golden Path Issues (discovered in E2E testing)

- [x] **Stale branches block worktree creation on rerun** — `worktree.Create()` now does best-effort deletion of pre-existing branch before `git worktree add -b`.

- [x] **Merger doesn't specify base branch** — `MergerPromptData` now includes `BaseBranch`. Merger prompt and system prompt prescribe explicit `git checkout <base> && git merge <branch>` workflow.

- [x] **tasks.yaml state not persisted across wave phases** — `taskStore.Save()` now called after development, validation, and merge phases.

- [x] **Worktrees/branches not cleaned up after successful merge** — Worktrees and branches for merged tasks are now removed in `presentChangesets()` after successful merger.

## Low (plan features not yet implemented)

- [ ] **Post-execution git commit verification** — Plan specifies `git log --oneline base_branch..agent_branch` to verify commits exist after worker completes. Not implemented in postcheck. (`internal/agent/postcheck.go`)

- [ ] **Sensitive content detection in postcheck** — Plan specifies `containsSensitiveContent()` to detect secrets, keys, and tokens in modified files. Not implemented. (`internal/agent/postcheck.go`)

- [ ] **End-to-end test infrastructure** — Plan specifies `test/e2e/` directory with 5 E2E tests using real `claude --print` against a test repo. Directory does not exist.

- [ ] **Max re-plan attempts** — Plan specifies max 3 re-plan attempts, then suggest manual plan writing. No limit exists (re-plan itself is stubbed). (`internal/orchestrator/orchestrator.go`)

- [ ] **Re-plan rejection notes** — Plan specifies passing human's rejection notes to next planner invocation via `ReplanNotes` template variable. Not implemented (re-plan is stubbed). (`internal/orchestrator/planner.go`)

- [ ] **Memory decay config passthrough** — Plan specifies `decay_policy.summarize_after_sessions` and `preserve_failures_sessions` passed to BeadsProvider. Config is parsed but BeadsProvider does not receive or use decay policy. (`cmd/blueflame/main.go`, `internal/memory/beads.go`)

- [ ] **Budget warn threshold** — Plan specifies warning at 80% of budget (`warn_threshold: 0.8`). Config field exists but threshold is never checked; only the 100% circuit breaker fires. (`internal/orchestrator/orchestrator.go`)

- [ ] **Validator diagnostic commands in spawner/prompt** — Watcher template restricts validator bash to diagnostic commands, but commands list is not passed to the validator's system prompt so the validator doesn't know which commands to run. (`internal/agent/spawner.go`, prompt templates)

- [ ] **Merge conflict detection after changeset merge** — Plan specifies detecting conflicts when merging approved changesets to base branch in sequence, re-queuing conflicting changesets for next cycle. Not implemented. (`internal/orchestrator/orchestrator.go`)

- [ ] **Config drift detection between wave cycles** — Plan specifies detecting and warning if `blueflame.yaml` changes between wave cycles. Not implemented. (`internal/orchestrator/orchestrator.go`)

- [ ] **Task history size bounding** — Plan verification test #47 specifies history stays bounded across multiple re-queues. No max history entries enforced. (`internal/tasks/tasks.go`)

- [ ] **Per-agent token budget monitoring goroutine** — Plan specifies `monitorAgentTokens()` goroutine that periodically checks agent's cumulative token usage from JSON output and kills the agent if exceeded. Spawner passes `--max-tokens` flag but orchestrator does not actively monitor token-based budgets. (`internal/orchestrator/worker.go`)
