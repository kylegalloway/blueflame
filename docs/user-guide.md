# Blue Flame User Guide

Blue Flame is a wave-based multi-agent orchestration system that decomposes large development tasks into parallelizable sub-tasks, assigns each to an isolated Claude agent, validates the results, and merges approved changes into your codebase.

## Quick Start

1. Copy the example config and customize it for your project:

```bash
cp blueflame.yaml.example blueflame.yaml
# Edit project.name, project.repo, and project.base_branch
```

2. Run Blue Flame with a task description:

```bash
blueflame "Add JWT authentication to the API"
```

3. Blue Flame will:
   - Plan: decompose the task into sub-tasks
   - Develop: spawn parallel agents in isolated worktrees
   - Validate: review each agent's work
   - Merge: combine approved changes into your base branch

You approve at each gate before anything merges.

## CLI Reference

### Usage

```
blueflame --task 'description' [--config blueflame.yaml]
blueflame 'description'
blueflame cleanup [--config blueflame.yaml]
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--task` | | Task description for the planner |
| `--config` | `blueflame.yaml` | Path to config file |
| `--dry-run` | `false` | Show configuration and exit without spawning agents |
| `--decisions-file` | | Pre-scripted decisions file for CI/automation |
| `--version` | | Print version and exit |

The task can also be passed as a positional argument: `blueflame "my task"`.

### Subcommands

**`cleanup`** removes stale state from crashed or interrupted sessions:

```bash
blueflame cleanup --config blueflame.yaml
```

This removes orphaned worktrees, stale file locks, and recovery state files.

### Dry Run

Use `--dry-run` to preview the session configuration without spawning agents:

```bash
blueflame --dry-run --task "Add authentication"
```

This prints wave configuration, budget limits, per-agent budgets, permissions, and disk space status.

## Configuration

Blue Flame is configured through `blueflame.yaml`. See `blueflame.yaml.example` for a complete annotated example.

### Minimal Config

```yaml
schema_version: 1

project:
  name: "my-project"
  repo: "/path/to/repo"
```

Everything else uses sensible defaults.

### Project Settings

```yaml
project:
  name: "my-project"           # Display name (required)
  repo: "/path/to/repo"        # Absolute path to git repo (required)
  base_branch: "main"          # Branch agents fork from and merge into
  worktree_dir: ".trees"       # Directory for agent worktrees (relative to repo)
  tasks_file: ".blueflame/tasks.yaml"  # Task store location
```

### Concurrency

```yaml
concurrency:
  planning: 1       # Planner agents (typically 1)
  development: 4    # Parallel worker agents (1-8)
  validation: 2     # Parallel validator agents
  merge: 1          # Merger agents (typically 1)
  adaptive: true    # Reduce workers if system RAM is low
  adaptive_min_ram_per_agent_mb: 600  # Minimum MB per agent
```

When `adaptive` is enabled, Blue Flame checks available RAM at startup and reduces worker count if the system can't support the configured number at 600 MB each.

### Budget Limits

Set a session-wide budget to prevent runaway costs. Use either USD or token limits, not both:

```yaml
limits:
  max_session_cost_usd: 10.00   # Stop when session cost reaches this
  max_session_tokens: 0          # OR use token limit (set other to 0)
  max_wave_cycles: 5             # Maximum dev-validate-merge iterations
  max_retries: 2                 # Retry failed tasks up to N times
  agent_timeout: 300s            # Kill agents that run longer than this
```

Per-role budgets cap individual agent invocations. Use either `_usd` or `_tokens` per role, not both:

```yaml
limits:
  token_budget:
    planner_usd: 0.40
    worker_usd: 1.50
    validator_usd: 0.15
    merger_usd: 0.50
    warn_threshold: 0.8    # Warn at 80% of budget
```

### Models

Assign Claude models to each role:

```yaml
models:
  planner: "sonnet"      # Task decomposition
  worker: "sonnet"       # Code implementation
  validator: "haiku"     # Code review (cheaper model works well)
  merger: "sonnet"       # Branch merging
```

### Permissions

Control what agents can access:

```yaml
permissions:
  allowed_paths:           # Glob patterns agents may modify
    - "src/**"
    - "tests/**"
  blocked_paths:           # Glob patterns agents must not touch
    - ".env*"
    - "*.secret"
    - "blueflame.yaml"
  allowed_tools:           # Claude tools agents can use
    - "Read"
    - "Write"
    - "Edit"
    - "Glob"
    - "Grep"
    - "Bash"
  blocked_tools:           # Tools agents cannot use
    - "WebFetch"
    - "WebSearch"
  bash_rules:
    allowed_commands:      # Command prefixes agents may run
      - "go test"
      - "go build"
      - "git diff"
      - "git status"
      - "git add"
      - "git commit"
    blocked_patterns:      # Regex patterns always blocked
      - "rm\\s+-rf"
      - "git\\s+push"
      - "sudo"
```

Permissions are enforced through a watcher hook script injected into each agent's `.claude/settings.json`. Every tool invocation is checked against these rules before execution.

### Validation

Configure what the validator checks:

```yaml
validation:
  commit_format:
    pattern: "^(feat|fix|refactor|test|docs|chore)\\(task-\\d+\\): .+"
    example: "feat(task-001): add JWT middleware"
  file_scope:
    enforce: true          # Restrict agents to their declared file_locks
  require_tests:
    enabled: true
    source_patterns:
      - "src/**/*.go"
    test_patterns:
      - "src/**/*_test.go"
  validator_diagnostics:
    enabled: true
    commands:               # Run these during validation
      - "go test ./..."
      - "go vet ./..."
    timeout: 120s
```

### Cross-Session Memory (Beads)

When enabled, Blue Flame saves session results and loads prior context for the planner. Failed tasks from previous sessions inform future planning:

```yaml
beads:
  enabled: true
  archive_after_wave: true
  include_failure_notes: true
  memory_decay: true
  decay_policy:
    summarize_after_sessions: 3
    preserve_failures_sessions: 5
```

## How It Works

### The Wave Cycle

Blue Flame operates in repeating wave cycles, each with four phases:

```
Plan  -->  Develop  -->  Validate  -->  Merge
  |                                      |
  |           (repeat if work remains)   |
  <--------------------------------------+
```

#### Phase 1: Planning

A planner agent decomposes your task description into independent, parallelizable sub-tasks. Each sub-task specifies:

- **Title and description**: what to implement
- **Priority**: execution order (higher = first)
- **File locks**: which files/directories this task owns exclusively
- **Dependencies**: tasks that must complete before this one starts
- **Cohesion group**: tasks that should be merged together

You review the plan and choose to approve, edit, re-plan, or abort.

#### Phase 2: Development

For each ready task (no pending dependencies, no lock conflicts):

1. A git worktree is created on a new branch forked from your base branch
2. File locks are acquired (flock-based, all-or-nothing)
3. A watcher hook script is generated and injected as a PreToolUse hook
4. A worker agent is spawned in the isolated worktree

Workers run in parallel up to your configured concurrency limit. Each worker:
- Operates in its own worktree (filesystem isolation)
- Holds exclusive locks on its declared files
- Is constrained by the watcher hook (tool/path/command restrictions)
- Has its own budget limit

After each worker completes:
- A **postcheck** validates that no files were modified outside the task's declared scope
- File locks are released for that agent specifically
- Failed tasks are retried (up to `max_retries`) or cascade failure to dependents

#### Phase 3: Validation

A validator agent reviews each completed task:
- Reads the diff between base and task branch
- Checks correctness, test coverage, style, and safety
- Runs diagnostic commands (tests, linters) if configured
- Outputs a structured verdict: **pass** or **fail**

If the validator itself fails (crashes, timeout), you're prompted to retry, skip, or manually review.

#### Phase 4: Merge

Validated tasks are grouped by cohesion group into changesets. For each changeset, you choose:

- **Approve**: a merger agent merges the branches into your base branch
- **Reject**: tasks are re-queued with your rejection reason for the next wave
- **Skip**: deferred to the next wave cycle

After merging, if tasks remain (re-queued, deferred, newly unblocked), you choose whether to continue with another wave cycle.

### Session Lifecycle

```
Startup
  ├── Load config
  ├── Check disk space
  ├── Clean stale state (orphaned worktrees, dead locks)
  └── Check for crash recovery state
        └── (Resume not yet implemented; starts fresh)

Wave Loop (up to max_wave_cycles)
  ├── Budget circuit breaker check
  ├── Development phase
  ├── Validation phase
  ├── Merge phase
  └── Session continuation decision

Shutdown
  ├── Save session to memory provider
  ├── Print cost summary
  └── Release all locks
```

### Task State Machine

```
pending ──> claimed ──> done ──> merged
   ^           |          |
   |           v          v
   +──── requeued    failed/blocked
```

- **pending**: waiting to be picked up
- **claimed**: assigned to an agent, work in progress
- **done**: agent completed, awaiting validation
- **merged**: approved and merged into base branch
- **failed**: agent failed, exceeded retries, or postcheck violation
- **blocked**: a dependency task failed

### File Locking

Blue Flame uses OS-level advisory locks (flock) to prevent concurrent agents from modifying the same files. Locks are:

- **All-or-nothing**: if any lock in a task's set can't be acquired, none are
- **Per-agent**: released when that specific agent completes, not at shutdown
- **Conflict-aware**: the scheduler defers tasks whose locks conflict with running agents

### Postcheck

After a worker completes, Blue Flame validates that it stayed within bounds:

- Files modified must be within `permissions.allowed_paths`
- Files modified must not be in `permissions.blocked_paths`
- If `validation.file_scope.enforce` is true, files must be within the task's declared `file_locks`

Violations fail the task, which can be retried.

## Automation

### Decisions File

For CI pipelines or automated testing, provide pre-scripted decisions:

```bash
blueflame --decisions-file decisions.txt --task "Run migration"
```

The decisions file contains one decision per line:

```
# Plan phase
approve

# Changeset phase
changeset-approve

# Session continuation
stop
```

Available decisions:
- `approve` / `plan-approve` / `plan-edit` / `plan-replan` / `plan-abort`
- `changeset-approve` / `changeset-reject` / `changeset-skip`
- `continue` / `stop` / `replan`

### State Files

Blue Flame stores internal state in `.blueflame/` within your repo:

```
.blueflame/
  ├── tasks.yaml       # Current task store
  ├── state.json       # Orchestrator state (crash recovery)
  ├── agents.json      # Running agent registry
  ├── locks/           # flock files
  ├── hooks/           # Generated watcher scripts
  └── audit/           # Agent action logs (JSONL)
```

Add `.blueflame/` to your `.gitignore`.

## Cost Management

### Estimating Costs

A rough guide based on typical Claude pricing:
- Planner: $0.20-0.50 per invocation
- Worker: $0.50-3.00 per task (depends on complexity)
- Validator: $0.05-0.20 per task
- Merger: $0.10-0.50 per merge

A 5-task session typically costs $3-15.

### Controlling Costs

1. **Session limit**: set `max_session_cost_usd` to cap total spend
2. **Per-agent budgets**: set `worker_usd` / `validator_usd` to cap individual agents
3. **Wave cycles**: set `max_wave_cycles: 1` for single-pass execution
4. **Retries**: set `max_retries: 0` to disable automatic retries
5. **Model selection**: use `haiku` for validators (cheaper, works well for review)

### Session Summary

At the end of each session, Blue Flame prints a summary:

```
=== Session Summary ===
Session:    ses-20260207-160430
Duration:   3m42s
Waves:      2

Tasks:
  Completed: 4
  Merged:    3
  Failed:    1

Cost:
  Total:     $4.2300
  Limit:     $10.00 (42.3% used)
  Tokens:    12450
=======================
```

## Troubleshooting

### Stale State After Crash

If Blue Flame crashes or is killed, run cleanup before the next session:

```bash
blueflame cleanup
```

This removes orphaned worktrees, releases dead locks, and clears recovery state.

### Lock Conflicts

If tasks are deferred due to lock conflicts, they'll be scheduled in the next wave cycle once the conflicting agent completes. To minimize conflicts:

- Keep `file_locks` as narrow as possible
- Use directory locks (`src/auth/`) rather than broad globs
- Avoid overlapping lock scopes between tasks

### Agent Timeouts

If agents consistently time out, increase `limits.agent_timeout` or reduce task scope. The lifecycle manager monitors heartbeats and kills stalled agents after `2 * heartbeat_interval` with no activity.

### Disk Space

Blue Flame checks for at least 500 MB of free disk space at startup. Each worktree is a shallow copy of your repo, so plan for `repo_size * concurrency.development` of additional disk usage.
