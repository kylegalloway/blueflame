# Blue Flame: Incomplete / Stubbed / Unfinished Items

## Critical (orchestrator can't actually run agents correctly)

- [ ] **Worktree/lock/hook wiring missing** — `runDevelopment()` hardcodes `"/tmp/wt-"+task.ID` instead of calling `worktree.Manager.Create()`. Never acquires file locks, generates watcher hooks, or creates `.claude/settings.json`. No rollback on failure. (`internal/orchestrator/orchestrator.go:220`)

- [ ] **No postcheck after worker completion** — `agent.PostCheck()` exists but is never called. Workers complete with no filesystem diff validation. (`internal/orchestrator/orchestrator.go:249-279`)

- [ ] **No per-agent lock release** — Locks are never released per-agent after completion. Only `ReleaseAll()` on shutdown. (`internal/orchestrator/orchestrator.go:249-279`)

- [ ] **Merge phase is a no-op** — `SpawnMerger()` is never called. Changesets are "approved" without actually merging branches into base. (`internal/orchestrator/orchestrator.go:129-136`)

- [ ] **PromptRenderer unimplemented** — Interface declared on `ProductionSpawner` but has no implementation. Workers get hardcoded `"Implement task %s: %s"` strings. (`internal/agent/spawner.go:86-89, 111`)

- [ ] **Missing claude CLI flags** — No `--system-prompt`, `--json-schema`, `--settings`, or `--max-tokens` flags. Validators can't produce structured output. (`internal/agent/spawner.go:97-112, 138-150, 175-189, 214-231`)

## High (significant feature gaps)

- [ ] **Memory provider not wired** — `BeadsProvider` and `NoopProvider` exist but are never called. No session archival, no prior context for planner. (`internal/memory/`)

- [ ] **Validator failure handling** — No retry on failure, no human escalation via `ui.ValidatorFailed()`. Partial cohesion group warnings missing. (`internal/orchestrator/orchestrator.go:303-320`)

- [ ] **Changeset ordering ignores inter-group dependencies** — Iterates a map (random order). No auto-deferral when a dependency group is rejected. (`internal/orchestrator/orchestrator.go:329-353`)

- [ ] **Single-batch development** — Deferred tasks (lock conflicts) wait until next wave cycle instead of being re-spawned within the same wave. (`internal/orchestrator/orchestrator.go:105-150`)

- [ ] **Linux sandbox stub** — Only `Setpgid` + `CLONE_NEWNET`. No cgroups, no rlimits for CPU/memory/files. (`internal/agent/sandbox_linux.go:25-26`)

- [ ] **macOS sandbox stub** — CPU time, file size, and open file rlimits are configured but never applied. (`internal/agent/sandbox_darwin.go:31-33`)

- [ ] **Cost summary empty** — `main.go` builds `CostSummary` with all zero values. Orchestrator doesn't expose final totals. (`cmd/blueflame/main.go:172-178`)

## Medium (polish / completeness)

- [ ] **Re-plan is a stub** — Both `PlanReplan` and `SessionReplan` just return `ErrPlanRejected`. (`internal/orchestrator/orchestrator.go:96, 147`)

- [ ] **Crash recovery resume** — Detects recovery state but always starts fresh. No resume from saved wave/phase. (`cmd/blueflame/main.go:122-127`)

- [ ] **Lifecycle hooks not invoked** — `post_plan`, `pre_validation`, `post_merge`, `on_failure` scripts are parsed from config but never executed. (`internal/config/config.go:187-192`)

- [ ] **Superpowers unused** — Config parsed but never passed to agents. (`internal/config/config.go:169-172`)

- [ ] **Audit log infrastructure** — No directory creation, no log rotation, no retention policy, no audit summary generation for validators. Watcher template writes JSONL and stall detector reads modtimes, but nothing else.

- [ ] **Interactive planning** — `planning.interactive` config exists but planner always uses `--print`. (`internal/agent/spawner.go:138-150`)

- [ ] **No worktree cleanup on success** — Worktrees only cleaned via `blueflame cleanup` command, not on successful session completion. (`internal/orchestrator/orchestrator.go:152-157`)
