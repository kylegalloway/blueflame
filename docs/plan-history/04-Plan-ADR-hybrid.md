# Blue Flame: Wave-Based Multi-Agent Orchestration System (Hybrid Plan)

## Context

Building a multi-agent orchestration system inspired by Steve Yegge's Gastown but more straightforward. The mental model is **human as project lead/architect** directing a development team of AI agents. The system uses wave-based execution (plan, work, validate, merge), strict permission enforcement via watchers, git worktree isolation, and YAML-based task claiming. Optimized for low hardware specs and reduced token usage.

This plan is the result of an Architecture Decision Record (ADR) that evaluated three candidate approaches and synthesized the strongest ideas from each into a single cohesive design.

### Design Lineage

| Decision | Source Plan | Rationale |
|----------|-------------|-----------|
| Claude Code Skill as orchestrator | Plan B | No compilation, direct Task tool dispatch, lower token overhead, faster iteration |
| Shell scripts for mechanical operations | Plan B | No dependencies, deterministic, keeps orchestrator context lean |
| Three-phase watcher enforcement model | Plan C | Pre-execution, runtime, and post-execution checks are more robust than hooks alone |
| Watcher hooks via Claude Code PreToolUse | Plan A, Plan B | Real-time blocking is necessary; hooks provide this natively |
| Post-execution filesystem diff validation | Plan C | Catches anything the hooks missed; defense in depth |
| OS-level runtime constraints (ulimit) | Plan C | Deterministic resource limits without token cost |
| Process groups for orphan prevention | Plan A | More reliable than PID-file-only tracking |
| Beads integration for persistent memory | Plan B | Cross-session context, memory decay, session independence |
| Token budget tracking and enforcement | Plan B | Essential for cost optimization |
| Two-tier validation (mechanical + semantic) | Plan B | Cheap deterministic checks first, expensive LLM checks second |
| Cohesion groups for task grouping | Plan C | Cleaner merge semantics, explicit grouping |
| Re-queue logic for rejected tasks | Plan B | Handles failures gracefully without human re-planning |
| Deterministic controller mindset | Plan C | The orchestrator should be predictable, not creative |
| Changeset chaining with human approval | Plan A, Plan B | Multiple changesets reviewed sequentially before next session |
| Per-wave configurable concurrency | Plan B | Flexibility per role (e.g., 4 workers, 2 validators, 1 merger) |
| Network isolation enforcement | Plan C | Agents should never access the network |

**Key dependencies**: `claude` CLI (agent runtime), Superpowers plugin (skills), git worktrees (isolation), Beads (persistent memory), shell scripts (mechanical operations).

---

## Architecture Overview

```
Human (Project Lead)
    |
    v
+-------------------------------------+
|  Blue Flame (Claude Code Skill)       |
|  - Reads blueflame.yaml config       |
|  - Manages waves & agent lifecycle   |
|  - Calls shell helpers for mechops   |
|  - Dispatches agents via Task tool   |
|  - Presents changesets for review    |
|  - Archives results to Beads         |
+----------+---------------------------+
           | spawns via Task tool
           v
+----------------------------------------------+
|  Wave 1: PLANNING                            |
|  +---------+                                 |
|  | Planner | -> produces task plan (YAML)    |
|  +---------+    -> human approves/edits      |
|----------------------------------------------+
|  Wave 2: DEVELOPMENT                         |
|  +----------+ +----------+ +----------+     |
|  | Worker 1 | | Worker 2 | | Worker N |     |
|  | +Watcher | | +Watcher | | +Watcher |     |
|  | worktree | | worktree | | worktree |     |
|  +----------+ +----------+ +----------+     |
|  (concurrency configurable, max 4)           |
|----------------------------------------------+
|  Wave 3: VALIDATION                          |
|  +-----------+ +-----------+                 |
|  |Validator 1| |Validator N|  (1 per worker) |
|  +-----------+ +-----------+                 |
|----------------------------------------------+
|  Wave 4: MERGE                               |
|  +--------+                                  |
|  | Merger | -> coalesces into changeset(s)   |
|  +--------+ -> human reviews & approves      |
+----------------------------------------------+
           |
           v
    Archive to Beads -> Repeat (next wave cycle)
```

---

## Skill Structure

```
blueflame/
+-- SKILL.md                         # Main orchestrator skill definition
+-- blueflame.yaml.example           # Example/default configuration
+-- templates/
|   +-- planner-prompt.md            # Prompt template for planner agents
|   +-- worker-prompt.md             # Prompt template for worker agents
|   +-- validator-prompt.md          # Prompt template for validator agents
|   +-- merger-prompt.md             # Prompt template for merger agents
+-- scripts/
|   +-- blueflame-init.sh            # Setup: create dirs, validate prereqs
|   +-- worktree-manage.sh           # Create/remove/list worktrees
|   +-- lock-manage.sh               # Acquire/release/check advisory locks
|   +-- watcher-generate.sh          # Generate per-agent watcher hook scripts
|   +-- watcher-postcheck.sh         # Post-execution filesystem diff validation
|   +-- token-tracker.sh             # Token budget tracking from audit logs
|   +-- lifecycle-manage.sh          # PID/process group tracking, orphan cleanup
|   +-- beads-archive.sh             # Archive session results to beads
|   +-- sandbox-setup.sh             # OS-level runtime constraints (ulimit, etc.)
+-- hooks/
|   +-- watcher.sh.tmpl              # Template for generated watcher scripts
+-- .blueflame/                      # Runtime state directory (auto-created)
    +-- agents.json                  # Active agent registry
    +-- state.yaml                   # Crash recovery state
    +-- locks/                       # Advisory lock files
    +-- hooks/                       # Generated per-agent watcher scripts
    +-- logs/                        # Per-agent audit logs (JSONL)
```

---

## Configuration: `blueflame.yaml`

The human (project lead) writes this before any session. It thoroughly defines what agents can and cannot do, resource limits, and session parameters.

```yaml
# blueflame.yaml - Project lead's configuration

project:
  name: "my-project"
  repo: "/path/to/repo"
  base_branch: "main"
  worktree_dir: ".trees"

# Per-wave concurrency limits
concurrency:
  planning: 1                        # Always 1 planner
  development: 4                     # Max parallel workers (1-4)
  validation: 2                      # Max parallel validators
  merge: 1                           # Always 1 merger

# Resource limits (optimized for low hardware)
limits:
  agent_timeout: 300s                # Max time per agent before kill
  heartbeat_interval: 30s            # Agent liveness check frequency
  max_retries: 2                     # Retries on agent failure
  token_budget:
    worker: 100000                   # Max tokens per worker agent
    validator: 30000                 # Max tokens per validator
    planner: 50000                   # Max tokens per planner
    merger: 80000                    # Max tokens per merger
    warn_threshold: 0.8              # Warn at 80% of budget

# OS-level resource constraints (applied per agent via ulimit/sandbox)
sandbox:
  max_cpu_seconds: 600               # CPU time limit
  max_memory_mb: 512                 # Memory limit
  max_file_size_mb: 50               # Max single file size
  max_open_files: 256                # Max open file descriptors
  allow_network: false               # Agents may NEVER access the network

# Model configuration (cost optimization)
models:
  planner: "sonnet"                  # Good at decomposition and planning
  worker: "sonnet"                   # Primary work model
  validator: "haiku"                 # Cheapest - sufficient for review
  merger: "sonnet"                   # Needs context understanding

# Permission rules - what agents CAN and CANNOT do
permissions:
  allowed_paths:                     # Glob patterns of files agents may touch
    - "src/**"
    - "tests/**"
    - "docs/**"
  blocked_paths:                     # Files agents must NEVER touch
    - ".env*"
    - "*.secret"
    - "*.key"
    - "blueflame.yaml"
    - ".claude/**"
    - ".blueflame/**"
    - "go.mod"
    - "go.sum"
    - "package-lock.json"
  allowed_tools:                     # Claude Code tools agents may use
    - "Read"
    - "Write"
    - "Edit"
    - "Glob"
    - "Grep"
    - "Bash"
  blocked_tools:                     # Tools agents must NEVER use
    - "WebFetch"
    - "WebSearch"
    - "NotebookEdit"
    - "Task"                         # Workers must not spawn sub-agents
  bash_rules:
    allowed_commands:                 # Bash commands agents may run
      - "go test"
      - "go build"
      - "go vet"
      - "make"
      - "git diff"
      - "git status"
      - "git log"
      - "git add"
      - "git commit"
      - "npm test"
      - "npm run lint"
    blocked_patterns:                # Regex patterns to block in bash
      - "rm -rf"
      - "git push"
      - "git checkout main"
      - "git merge"
      - "curl|wget"
      - "sudo"
      - "chmod"
      - "docker"
      - "nc |netcat|ncat"
      - "ssh|scp|sftp"
      - "pip install|npm install"

# Output validation rules (Tier 1 - mechanical, enforced by watchers)
validation:
  commit_format:
    pattern: "^(feat|fix|refactor|test|docs|chore)\\(task-\\d+\\): .+"
    example: "feat(task-001): add JWT middleware"
  file_naming:
    style: "snake_case"
    enforce_for:
      - "src/**/*.go"
      - "tests/**/*.go"
  require_tests:
    enabled: true
    source_patterns:
      - "src/**/*.go"
    test_patterns:
      - "tests/**/*_test.go"
      - "src/**/*_test.go"
  file_scope:
    enforce: true                    # Block changes outside task's declared file_locks

# Superpowers configuration
superpowers:
  enabled: true
  skills:
    - "test-driven-development"
    - "systematic-debugging"
    - "requesting-code-review"

# Beads configuration (persistent memory)
beads:
  enabled: true
  archive_after_wave: true           # Archive results after each wave cycle
  include_failure_notes: true        # Store failure context for future sessions
  memory_decay: true                 # Summarize old entries to save context
```

---

## Task File: `tasks.yaml`

Generated by the planner agent, claimed by workers via their unique ID. This is the runtime format. After each session, results are archived to Beads.

```yaml
# tasks.yaml - Generated by planner, managed by Blue Flame
version: 1
session_id: "ses-20260206-143022"
wave: 2

tasks:
  - id: "task-001"
    title: "Add JWT middleware to API router"
    description: |
      Create JWT validation middleware in pkg/middleware/auth.go.
      Must validate tokens from Authorization header.
      Return 401 on invalid/expired tokens.
    status: "pending"                # pending | claimed | done | failed | requeued
    agent_id: null                   # Worker claims by writing their ID here
    priority: 1                      # Lower = higher priority
    cohesion_group: "auth"           # Logical grouping for merge ordering
    dependencies: []                 # Task IDs that must complete first
    file_locks:                      # Files/dirs this task needs exclusive access to
      - "pkg/middleware/"
      - "internal/auth/"
    worktree: null                   # Populated when claimed
    branch: null                     # Populated when claimed
    result:
      status: null                   # pass | fail (set by validator)
      notes: ""
    history: []                      # Prior attempts (from re-queued tasks)

  - id: "task-002"
    title: "Add auth tests"
    status: "pending"
    agent_id: null
    priority: 2
    cohesion_group: "auth"
    dependencies: ["task-001"]
    file_locks:
      - "tests/auth/"
    worktree: null
    branch: null
    result:
      status: null
      notes: ""
    history: []
```

---

## Wave Orchestration Flow

### Wave 1: Planning

1. Orchestrator runs `blueflame-init.sh` to validate prerequisites:
   - Git repo exists and is clean
   - `claude` CLI is available
   - Beads is installed (if enabled)
   - Worktree directory is writable
   - Superpowers plugin is available (if enabled)
2. If Beads enabled, load prior session context (failure notes, patterns) via `beads-archive.sh load`
3. Spawn a single planner agent via Task tool with `planner-prompt.md`
4. Planner receives: human's task description + project context + blueflame.yaml constraints + prior session memory
5. Planner uses superpowers `/brainstorm` and `/write-plan` skills
6. Planner outputs `tasks.yaml` with decomposed tasks, dependencies, file locks, cohesion groups
7. Orchestrator presents the plan to the human for approval
8. Human approves, edits, or rejects. Loop until approved.

### Wave 2: Development (Workers)

1. Orchestrator reads `tasks.yaml`, identifies ready tasks (no unmet dependencies)
2. For each ready task (up to `concurrency.development`):
   a. Generate unique agent ID: `worker-<short-hash>` (e.g., `worker-a1b2c3`)
   b. Run `worktree-manage.sh create <agent-id> <base-branch>` for git worktree isolation
   c. Run `lock-manage.sh acquire <agent-id> <path1> <path2>...` for advisory file locks
   d. Run `sandbox-setup.sh <agent-id>` to configure OS-level resource limits
   e. Run `watcher-generate.sh <agent-id>` to create per-agent PreToolUse hooks from config
   f. Spawn worker via Task tool in the worktree directory with focused prompt from `worker-prompt.md`
   g. Write agent ID to `tasks.yaml` (claim the task)
   h. Run `lifecycle-manage.sh register <agent-id> <pid>` to track in process group
3. Orchestrator monitors all workers:
   - Liveness checks via PID and process group (every `heartbeat_interval`)
   - Timeout enforcement (kill after `agent_timeout`)
   - Token budget tracking via `token-tracker.sh`
   - At 80% budget: warning logged to audit trail
   - At 100% budget: agent terminated, task marked `failed`
4. As each worker completes:
   - Worker commits changes to its worktree branch (following `commit_format`)
   - Run `watcher-postcheck.sh <agent-id>` for post-execution filesystem diff validation
   - If postcheck fails: task marked `failed` with violation details
   - If postcheck passes: task marked `done` in `tasks.yaml`
   - Run `lock-manage.sh release <agent-id>` to free file locks
5. Once ALL workers complete, transition to Wave 3

### Wave 3: Validation

Two-tier validation. Tier 1 (mechanical) ran continuously during Wave 2 via watcher hooks and post-execution checks. Tier 2 (semantic) runs now.

1. For each completed task (status=done), up to `concurrency.validation` at a time:
   a. Spawn a Validator agent (haiku model, cheapest) in the task's worktree via Task tool
   b. Validator receives: task description + diff (`git diff main...<branch>`) + watcher audit log summary
   c. Validator checks:
      - **Task applicability**: Do the changes solve the stated problem? Are there unrelated changes?
      - **Correctness**: Does the implementation look right? Any logic errors?
      - **Regressions**: Do tests pass? Any broken behavior?
      - **Scope creep**: Changes stay within what was asked, no over-engineering
   d. Validator marks result as `pass` or `fail` with specific notes
2. Failed tasks: human is notified, can retry (re-queue) or skip
3. Once ALL validations complete, transition to Wave 4

### Wave 4: Merge

1. Spawn a single Merger agent via Task tool
2. Merger sees all validated branches and their diffs, grouped by `cohesion_group`
3. Merger creates one or more cohesive changesets:
   - Groups changes by cohesion group
   - Resolves any cross-task conflicts within groups
   - Creates clean commit messages following `commit_format`
   - Stops and reports if semantic conflict resolution is required (does not guess)
4. Orchestrator presents changesets to human (changeset chaining):
   ```
   Changeset 1/3: [auth] Add JWT middleware
     [12 files changed, +340, -12]
     (a)pprove / (r)eject / (v)iew diff / (s)kip? a

   Changeset 2/3: [auth] Add auth tests
     [4 files changed, +180, -0]
     (a)pprove / (r)eject / (v)iew diff / (s)kip? r
     -> Re-queued as task-002 for next wave cycle (rejection reason recorded)

   Changeset 3/3: [api] Update API docs
     [2 files changed, +45, -3]
     (a)pprove / (r)eject / (v)iew diff / (s)kip? a

   2 changesets approved, 1 re-queued. Merging...
   ```
5. Approved changesets merged to base branch
6. Rejected changesets: tasks marked `requeued` with rejection notes in `history` array
7. Run `beads-archive.sh save` to persist session results (tasks, outcomes, costs, failure notes)
8. Worktrees cleaned up via `worktree-manage.sh cleanup`
9. If re-queued tasks or remaining tasks in `tasks.yaml`, repeat from Wave 2

---

## Three-Phase Watcher Enforcement Model

The watcher system uses defense in depth: three distinct enforcement phases ensure no agent can violate its constraints, even if one layer is bypassed.

### Phase 1: Pre-Execution (Real-Time Hook Enforcement)

Enforced continuously during Wave 2 by generated shell scripts registered as Claude Code `PreToolUse` hooks. Fast, cheap, deterministic.

| Check | How |
|-------|-----|
| Tool allowlist | Is this tool in `allowed_tools`? Block if not. |
| Path restrictions | Is file in `allowed_paths` and not in `blocked_paths`? |
| Bash command filtering | Does command match `blocked_patterns`? Block network commands. |
| File scope | Are changes within the task's declared `file_locks`? |
| Commit message format | Does message match `commit_format.pattern`? |
| File naming convention | Do new files follow `file_naming.style`? |
| Test requirements | Does new source file have corresponding test? |
| Token budget | Estimated usage approaching `token_budget`? Warn at threshold, block at limit. |

All watcher decisions logged to `.blueflame/logs/<agent-id>.audit.jsonl`.

Generated hook config (placed in per-agent worktree `.claude/settings.json`):

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "type": "command",
        "command": ".blueflame/hooks/<agent-id>/watcher.sh",
        "timeout": 5000
      }
    ]
  }
}
```

### Phase 2: Runtime Constraints (OS-Level Sandbox)

Applied by `sandbox-setup.sh` before agent launch. These are hard limits that cannot be circumvented by the agent.

| Constraint | Mechanism |
|------------|-----------|
| CPU time | `ulimit -t <max_cpu_seconds>` |
| Memory | `ulimit -v <max_memory_mb * 1024>` |
| File size | `ulimit -f <max_file_size_mb * 1024>` |
| Open files | `ulimit -n <max_open_files>` |
| Network access | Block via `unshare --net` (Linux) or sandbox-exec (macOS) |
| Working directory | Agent confined to its worktree path |

These limits apply to the agent process and all its children.

### Phase 3: Post-Execution Filesystem Diff Validation

Run by `watcher-postcheck.sh` after each agent completes. This is the final safety net.

| Check | How |
|-------|-----|
| Filesystem diff | Compare worktree state before and after agent execution |
| Allowed paths only | Confirm only files within `allowed_paths` and `file_locks` were modified |
| No blocked path changes | Confirm no files in `blocked_paths` were touched |
| No unexpected files | Identify any new files created outside declared scope |
| Binary file check | Flag any unexpected binary files added |
| Sensitive content scan | Check for accidentally committed secrets, keys, tokens |

If any post-execution check fails, the task is marked `failed` with detailed violation notes. The agent's changes are discarded.

---

## Agent Lifecycle Management

### Process Tracking

- Each agent tracked in `.blueflame/agents.json`:
  ```json
  {
    "worker-a1b2c3": {
      "pid": 12345,
      "pgid": 12340,
      "worktree": ".trees/worker-a1b2c3",
      "taskID": "task-001",
      "startTime": "2026-02-06T14:30:22Z",
      "lastHeartbeat": "2026-02-06T14:31:52Z",
      "tokenUsage": 23400,
      "status": "running"
    }
  }
  ```
- Process group ID (pgid) tracked alongside PID for reliable cleanup

### Heartbeat and Liveness

- Orchestrator periodically runs `kill -0 <pid>` checks via Bash
- If process dead: mark task as `failed`, release locks, clean worktree
- Heartbeat interval configurable via `limits.heartbeat_interval`

### Graceful Shutdown

On orchestrator interrupt (SIGINT/SIGTERM):
1. Send SIGTERM to all tracked agent process groups
2. Wait 10 seconds for graceful exit
3. Send SIGKILL to any survivors via process group
4. Release all locks via `lock-manage.sh release-all`
5. Persist state to `.blueflame/state.yaml` for crash recovery
6. Do NOT clean worktrees (preserve for debugging)

### Orphan Prevention

- `lifecycle-manage.sh` uses process groups where supported
- On startup, `blueflame-init.sh` checks `.blueflame/agents.json` for stale entries
- Kills surviving orphan processes via saved PID/PGID
- Cleans up stale worktrees and locks from previous crashed sessions
- Stale lock detection via PID liveness: if the PID that holds a lock is dead, the lock is stale

### Timeout Enforcement

- Orchestrator tracks agent start times in agents.json
- On timeout: send SIGTERM to process group, wait 5s, then SIGKILL
- Task marked `failed`, available for retry up to `max_retries`
- Timeout violations logged to audit trail

### Token Budget Enforcement

- `token-tracker.sh` estimates token usage from Claude Code audit logs
- At 80% of budget: warning logged to `.blueflame/logs/<agent-id>.audit.jsonl`
- At 100% of budget: agent killed, task marked `failed` with "token_budget_exceeded" reason
- Budget per-agent, not per-session, to prevent one agent from consuming all resources

---

## File and Directory Locking

### Lock Mechanism

- Simple file-based advisory locks in `.blueflame/locks/`
- Lock file naming convention: path separators replaced with underscores
  - `pkg/middleware/` -> `.blueflame/locks/pkg_middleware.lock`
- Lock file contents: `<agent-id> <pid> <timestamp>`
- Locking managed by `lock-manage.sh` (acquire, release, check, release-all)

### Lock Acquisition

- Each task declares `file_locks` (paths it needs exclusive access to)
- Before spawning a worker, orchestrator acquires locks via `lock-manage.sh acquire`
- If locks conflict between ready tasks, conflicting tasks run in sequence, not parallel
- Lock conflict detected by checking if lock file exists and holding PID is alive

### Lock Release

- Locks released when worker completes (success or failure)
- On crash: `blueflame-init.sh` startup cleanup also releases stale locks
- Stale = lock file exists but PID is dead

### Conflict Prevention

- Planner is instructed to decompose tasks such that `file_locks` don't overlap where possible
- Orchestrator sorts ready tasks by priority, then checks lock availability
- Tasks with conflicting locks are deferred to the next batch within the same wave

---

## Beads Integration (Persistent Memory)

Tasks.yaml is the runtime format. Beads provides cross-session memory and session independence.

### After Each Wave Cycle

`beads-archive.sh save` runs and:
1. Creates a bead for each completed task: description, result (pass/fail), validator notes, files changed, token usage
2. Creates a bead for each failed task: failure reason, watcher audit trail, retry count, rejection notes
3. Creates a session summary bead: total tasks, pass/fail counts, cost estimate, duration, re-queued tasks

### Before Each Session

`beads-archive.sh load` provides the planner with:
1. Prior failure notes (what went wrong last time, why tasks were re-queued)
2. Patterns from past sessions (which task decompositions worked well)
3. Cost history (how much prior sessions cost)
4. Re-queued task context (full history of prior attempts)

### Memory Decay

Old closed beads are summarized to save context window space, per Beads' built-in memory decay feature. This is critical for keeping token usage low across many sessions.

---

## Agent System Prompts (Minimal for Token Efficiency)

Each agent type gets a focused, minimal prompt. The orchestrator provides only the context each agent needs.

**Planner**: "You are a senior developer breaking down a feature request into isolated tasks. Each task must have clear file boundaries (for locking), explicit dependencies, a cohesion group label, and be completable in a single focused session. Output tasks in the YAML schema provided. Prior session context: [beads summary]. Use superpowers /brainstorm and /write-plan."

**Worker**: "You are a developer implementing task [ID]: [description]. Work ONLY in your worktree. Touch ONLY files listed in your task's file_locks. Your watcher will block out-of-scope changes. Follow TDD via superpowers. Commit with format: `type(task-ID): description`. You have no network access. Reference your task ID in all commits."

**Validator**: "You are a QA engineer reviewing task [ID]. The task asked for: [description]. The diff is below. Watcher audit summary: [summary]. Check: (1) Do the changes solve the stated problem? (2) Are there unrelated or out-of-scope changes? (3) Do tests pass? (4) Any regressions or logic errors? Output pass/fail with specific notes."

**Merger**: "You are a release engineer. Review these validated branches grouped by cohesion: [list with diffs and groups]. Create clean, cohesive changesets per cohesion group. Resolve any conflicts. Write clear commit messages following the commit format. If a semantic conflict cannot be safely resolved, stop and report it rather than guessing."

---

## Approvals and Human Gates

### Plan Approval (After Wave 1)

Orchestrator presents the generated plan:
```
=== BLUE FLAME: Plan Review ===

Session: ses-20260206-143022
Tasks: 3
Estimated cost: ~$0.78

  task-001 [auth] Add JWT middleware to API router
    Locks: pkg/middleware/, internal/auth/
    Dependencies: none

  task-002 [auth] Add auth tests
    Locks: tests/auth/
    Dependencies: task-001

  task-003 [api] Update API route documentation
    Locks: docs/api/
    Dependencies: none

(a)pprove / (e)dit / (r)eject?
```

### Changeset Approval (After Wave 4)

Changesets presented sequentially. Human can approve, reject, view diff, or skip each one. Rejected changesets are re-queued with history. All decisions are final before the next wave begins.

### Session Continuation

After all changesets are processed:
```
Wave cycle complete.
  Approved: 2 changesets (merged to main)
  Re-queued: 1 task (task-002, will retry next wave)
  Remaining: 0 tasks

(c)ontinue to next wave / (s)top?
```

This enables chaining: the human can review and approve multiple wave cycles without restarting the system.

---

## Cost Profile

For a typical session with 3 worker tasks:

| Agent | Model | Sessions | Est. Cost |
|-------|-------|----------|-----------|
| Orchestrator | (parent context) | 1 | minimal (shell helpers do mechops) |
| Planner | sonnet | 1 | ~$0.15 |
| Workers | sonnet | 3 | ~$0.45 |
| Validators | haiku | 3 | ~$0.03 |
| Merger | sonnet | 1 | ~$0.15 |
| **Total** | | **9** | **~$0.78** |

Key cost-saving measures:
- Validators use haiku (cheapest model)
- Watcher hooks are deterministic shell scripts (zero token cost)
- Post-execution checks are deterministic (zero token cost)
- Token budgets prevent runaway consumption
- Beads memory decay keeps cross-session context lean
- Focused prompts minimize per-agent context size
- Superpowers skills handle common patterns efficiently

---

## Implementation Phases

### Phase 1: Shell Helpers and Configuration (Foundation)

**Files**: `scripts/blueflame-init.sh`, `scripts/worktree-manage.sh`, `scripts/lock-manage.sh`, `blueflame.yaml.example`

**Deliverables**:
- `blueflame-init.sh`: validate prerequisites (git, claude CLI, beads, superpowers), create `.blueflame/` runtime directory structure
- `worktree-manage.sh`: create/remove/list git worktrees with `create <agent-id> <base-branch>`, `remove <agent-id>`, `cleanup` (remove all), `list` subcommands
- `lock-manage.sh`: acquire/release/check advisory locks with `acquire <agent-id> <paths...>`, `release <agent-id>`, `release-all`, `check <path>` subcommands
- `blueflame.yaml.example`: complete example configuration with all options documented
- YAML validation: init script validates blueflame.yaml schema

**Milestone**: Shell helpers work standalone. Can create worktrees, acquire/release locks, validate configuration.

### Phase 2: Watcher System (Three-Phase Enforcement)

**Files**: `hooks/watcher.sh.tmpl`, `scripts/watcher-generate.sh`, `scripts/watcher-postcheck.sh`, `scripts/sandbox-setup.sh`, `scripts/token-tracker.sh`

**Deliverables**:
- `watcher.sh.tmpl`: template covering all Tier 1 checks (tool allowlist, path restrictions, bash filtering, file scope, commit format, naming, tests, token budget)
- `watcher-generate.sh`: reads blueflame.yaml + task file_locks, generates per-agent watcher script, creates per-agent `.claude/settings.json` with hook registration
- `sandbox-setup.sh`: applies OS-level resource limits (ulimit) and network isolation before agent launch
- `watcher-postcheck.sh`: post-execution filesystem diff, path verification, sensitive content scan
- `token-tracker.sh`: parse Claude Code audit logs, estimate token usage, compare against budget
- Audit logging to `.blueflame/logs/<agent-id>.audit.jsonl`

**Milestone**: A manually spawned claude agent is constrained by generated watchers. Post-execution checks catch violations. Resource limits are enforced.

### Phase 3: Orchestrator Skill (Core Wave Logic)

**Files**: `SKILL.md`, `templates/planner-prompt.md`, `templates/worker-prompt.md`, `templates/validator-prompt.md`, `templates/merger-prompt.md`

**Deliverables**:
- `SKILL.md`: main orchestrator skill definition with full wave protocol
- Agent prompt templates for all 4 roles (planner, worker, validator, merger)
- Task tool dispatch with per-wave concurrency limits
- Human approval gates: plan approval after Wave 1, changeset chaining after Wave 4
- Task claiming: orchestrator writes agent ID to tasks.yaml
- Wave transitions: detect all-complete, handle failures, transition to next wave
- Re-queue logic: rejected changesets create `requeued` tasks with history
- Session continuation: prompt for next wave or stop after each cycle

**Milestone**: Full wave cycle works end-to-end via the skill. Human can approve plans, review changesets, and chain sessions.

### Phase 4: Lifecycle Hardening and Beads Integration

**Files**: `scripts/lifecycle-manage.sh`, `scripts/beads-archive.sh`, updates to `SKILL.md` and other scripts

**Deliverables**:
- `lifecycle-manage.sh`: PID/process group tracking, register/unregister agents, orphan detection, stale process cleanup
- Process group management: wrap agent launch to create process groups for clean teardown
- Timeout enforcement: orchestrator monitors start times, kills on timeout via process group
- Crash recovery: persist state to `.blueflame/state.yaml`, restore on restart
- `beads-archive.sh`: `save` (archive session results to beads), `load` (retrieve prior session context for planner)
- Memory decay integration for old bead entries
- Token budget hard-stop: kill agents that exceed their token budget
- Startup cleanup: `blueflame-init.sh` detects and cleans stale agents, locks, worktrees from prior crashed sessions

**Milestone**: System recovers gracefully from crashes. Planner receives context from prior sessions. Runaway agents are killed. No orphan or zombie processes persist.

### Phase 5: Polish and Optimization

**Deliverables**:
- Progress display during waves (which agents are running, time remaining, token usage)
- Cost summary after each session (actual vs. estimated)
- Cohesion group display in changeset review
- Dry-run mode: show what would happen without actually spawning agents
- Configuration validation: warn about common misconfigurations
- Detailed error messages for all failure modes
- Documentation: usage guide, troubleshooting, configuration reference

**Milestone**: Production-ready orchestrator. Pleasant human experience. Clear diagnostics.

---

## Verification Plan

1. **Helper script tests**: Each script tested in isolation
   - Create/remove worktree
   - Acquire/release/conflict-detect locks
   - Generate watcher from sample config
   - Apply sandbox limits

2. **Watcher test (Phase 1 - hooks)**: Spawn a claude agent with generated watcher, attempt blocked operations:
   - Write to blocked path -> blocked
   - Use blocked tool -> blocked
   - Run blocked bash command -> blocked
   - Modify file outside file_locks scope -> blocked
   - All blocks logged to audit JSONL

3. **Watcher test (Phase 3 - postcheck)**: After agent completes, run postcheck:
   - Agent that only modified allowed files -> pass
   - Agent that somehow modified blocked file -> fail, task rejected

4. **Sandbox test**: Verify OS-level limits are applied:
   - Agent cannot make network requests
   - Agent respects memory/CPU limits

5. **End-to-end test**: Run full blueflame skill against a small test repo with a simple task (e.g., "add a hello world function and test"), verify complete wave cycle

6. **Lifecycle test**: Kill a worker mid-execution, verify:
   - Orchestrator detects death via heartbeat
   - Locks released
   - Worktree cleaned up
   - Task marked `failed`
   - Retry works up to max_retries

7. **Lock test**: Two tasks with overlapping file_locks -> verify sequential execution within wave

8. **Re-queue test**: Reject a changeset -> verify task is re-queued with notes, appears in next wave

9. **Beads test**: Run two sessions -> verify planner in session 2 receives context from session 1

10. **Token budget test**: Set a low token budget -> verify agent is warned at 80%, killed at 100%

11. **Orphan test**: Kill the orchestrator mid-wave, restart -> verify orphan cleanup, state recovery

12. **Changeset chaining test**: Multiple workers complete -> verify merger produces reviewable changesets, human can approve/reject individually, rejected tasks are re-queued

---

## Existing Tools Leveraged

| Tool | Role | Why |
|------|------|-----|
| `claude` CLI | Agent runtime | Built-in Task tool, hook support, superpowers compatibility |
| Superpowers plugin | Agent skills | Planning, TDD, code review, debugging - battle-tested, reduces token waste |
| Git worktrees | Worker isolation | File-level isolation, shared object DB, branch-per-task, no full clone |
| Beads | Persistent memory | Git-backed, agent-friendly, memory decay, hash IDs prevent merge collisions |
| Lock files | File locking | Simple, advisory, no external deps, stale detection via PID |
| YAML | Config + tasks | Human-readable, simple, editable, no external deps |
| Shell scripts | Mechanical ops | No compilation, minimal deps, deterministic, keeps orchestrator tokens at zero for mechops |
| `ulimit` / OS sandbox | Resource limits | Deterministic, cannot be circumvented by agent, zero token cost |
| Process groups | Orphan prevention | Reliable cleanup of agent and all child processes |

---

## What This Plan Intentionally Omits

- **Go binary compilation**: The orchestrator is a Claude Code skill, not a compiled binary. This eliminates build steps, reduces iteration time, and leverages the existing Claude Code infrastructure directly.
- **Python agent runtime**: Agents run via `claude` CLI, not a custom Python runtime. This avoids a second language dependency and leverages Claude Code's native tool ecosystem.
- **Autonomous agent spawning**: Workers cannot spawn sub-agents (Task tool is blocked for workers). Only the orchestrator dispatches agents.
- **Network access for agents**: All agent network access is blocked at both the watcher level and the OS sandbox level. Defense in depth.
- **Persistent agents between waves**: Agents are ephemeral. They exist only for the duration of their wave. No agent state leaks between waves. Session memory is handled by Beads, not by agent persistence.
