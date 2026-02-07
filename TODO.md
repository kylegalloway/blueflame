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

- [ ] **Re-plan is a stub** — Both `PlanReplan` and `SessionReplan` just return `ErrPlanRejected`.

- [ ] **Crash recovery resume** — Detects recovery state but always starts fresh. No resume from saved wave/phase.

- [ ] **Lifecycle hooks not invoked** — `post_plan`, `pre_validation`, `post_merge`, `on_failure` scripts are parsed from config but never executed.

- [ ] **Superpowers unused** — Config parsed but never passed to agents.

- [ ] **Audit log infrastructure** — No directory creation, no log rotation, no retention policy, no audit summary generation for validators.

- [x] **Interactive planning** — Planner spawner now checks `cfg.Planning.Interactive` and omits `--print` flag when interactive mode is enabled.

- [ ] **No worktree cleanup on success** — Worktrees only cleaned via `blueflame cleanup` command, not on successful session completion.
