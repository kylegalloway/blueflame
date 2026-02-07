# Blue Flame: Wave-Based Multi-Agent Orchestration System

## Context

Building a multi-agent orchestration system inspired by Steve Yegge's Gastown but more straightforward. The mental model is **human as project lead/architect** directing a development team of AI agents. The system uses wave-based execution (plan → work → validate → merge), strict permission enforcement via watchers, git worktree isolation, and YAML-based task claiming. Optimized for low hardware specs and reduced token usage.

**Key dependencies**: Go (orchestrator), `claude` CLI (agents), Superpowers plugin (skills), git worktrees (isolation).

---

## Architecture Overview

```
Human (Project Lead)
    │
    ▼
┌─────────────────────────────────────┐
│  Blue Flame (Go binary)              │
│  - Reads blueflame.yaml config      │
│  - Manages waves & agent lifecycle  │
│  - Generates per-agent hooks        │
│  - Manages git worktrees & locks    │
│  - Presents changesets for review   │
└──────────┬──────────────────────────┘
           │ spawns claude CLI processes
           ▼
┌──────────────────────────────────────────────┐
│  Wave 1: PLANNING                            │
│  ┌─────────┐                                 │
│  │ Planner │ → produces task plan (YAML)     │
│  └─────────┘   → human approves/edits        │
├──────────────────────────────────────────────┤
│  Wave 2: DEVELOPMENT                         │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐     │
│  │ Worker 1 │ │ Worker 2 │ │ Worker N │     │
│  │ +Watcher │ │ +Watcher │ │ +Watcher │     │
│  │ worktree │ │ worktree │ │ worktree │     │
│  └──────────┘ └──────────┘ └──────────┘     │
│  (max 4 workers, each in isolated worktree)  │
├──────────────────────────────────────────────┤
│  Wave 3: VALIDATION                          │
│  ┌───────────┐ ┌───────────┐                 │
│  │Validator 1│ │Validator N│  (1 per worker) │
│  └───────────┘ └───────────┘                 │
├──────────────────────────────────────────────┤
│  Wave 4: MERGE                               │
│  ┌────────┐                                  │
│  │ Merger │ → coalesces into changeset(s)    │
│  └────────┘ → human reviews & approves       │
└──────────────────────────────────────────────┘
           │
           ▼
    Repeat (next wave cycle)
```

---

## Project Structure

```
blueflame/
├── cmd/
│   └── blueflame/
│       └── main.go              # CLI entrypoint
├── internal/
│   ├── config/
│   │   ├── config.go            # Parse & validate blueflame.yaml
│   │   └── config_test.go
│   ├── orchestrator/
│   │   ├── orchestrator.go      # Main wave loop
│   │   ├── orchestrator_test.go
│   │   ├── planner.go           # Wave 1: planning
│   │   ├── worker.go            # Wave 2: development
│   │   ├── validator.go         # Wave 3: validation
│   │   └── merger.go            # Wave 4: merge
│   ├── agent/
│   │   ├── agent.go             # Spawn & manage claude CLI processes
│   │   ├── agent_test.go
│   │   ├── lifecycle.go         # PID tracking, heartbeat, cleanup
│   │   └── hooks.go             # Generate per-agent watcher hook scripts
│   ├── tasks/
│   │   ├── tasks.go             # Read/write/claim tasks in YAML
│   │   └── tasks_test.go
│   ├── worktree/
│   │   ├── worktree.go          # Git worktree create/remove/list
│   │   └── worktree_test.go
│   ├── locks/
│   │   ├── locks.go             # File/directory advisory locking
│   │   └── locks_test.go
│   └── ui/
│       ├── prompt.go            # Simple CLI prompts for human review
│       └── diff.go              # Display diffs/changesets
├── templates/
│   ├── watcher.sh.tmpl          # Go template for watcher hook scripts
│   ├── planner-prompt.md.tmpl   # System prompt template for planner
│   ├── worker-prompt.md.tmpl    # System prompt template for workers
│   ├── validator-prompt.md.tmpl # System prompt template for validators
│   └── merger-prompt.md.tmpl    # System prompt template for merger
├── blueflame.yaml               # Example/default config
├── go.mod
├── go.sum
└── README.md
```

---

## Configuration: `blueflame.yaml`

The human (project lead) writes this before any session. It thoroughly defines what agents can and cannot do.

```yaml
# blueflame.yaml - Project lead's configuration for the dev team

project:
  name: "my-project"
  repo: "/path/to/repo"             # Target git repository
  base_branch: "main"               # Branch to create worktrees from
  worktree_dir: ".trees"            # Directory for agent worktrees

# Resource limits (optimized for low hardware)
limits:
  max_workers: 4                    # Max parallel workers (1-4)
  agent_timeout: 300s               # Max time per agent before kill
  heartbeat_interval: 30s           # Agent liveness check frequency
  max_retries: 2                    # Retries on agent failure

# Model configuration (cost optimization)
model:
  planner: "sonnet"                 # Cheaper model for planning
  worker: "sonnet"                  # Primary work model
  validator: "haiku"                # Cheapest for validation checks
  merger: "sonnet"                  # Needs to understand context

# Permission rules - what agents CAN do
permissions:
  allowed_paths:                    # Glob patterns of files agents may touch
    - "src/**"
    - "tests/**"
    - "docs/**"
  blocked_paths:                    # Files agents must NEVER touch
    - ".env*"
    - "*.secret"
    - "blueflame.yaml"
    - ".claude/**"
    - "go.mod"                      # Example: prevent dep changes
    - "go.sum"
  allowed_tools:                    # Claude Code tools agents may use
    - "Read"
    - "Write"
    - "Edit"
    - "Glob"
    - "Grep"
    - "Bash"
  blocked_tools:                    # Tools agents must NEVER use
    - "WebFetch"
    - "WebSearch"
    - "NotebookEdit"
  bash_rules:
    allowed_commands:               # Bash commands agents may run
      - "go test"
      - "go build"
      - "go vet"
      - "make"
      - "git diff"
      - "git status"
      - "git log"
      - "git add"
      - "git commit"
    blocked_patterns:               # Regex patterns to block
      - "rm -rf"
      - "git push"
      - "git checkout main"
      - "curl|wget"
      - "sudo"
      - "chmod"
      - "docker"

# Superpowers configuration
superpowers:
  enabled: true
  skills:                           # Which superpowers skills agents should use
    - "test-driven-development"
    - "systematic-debugging"
    - "requesting-code-review"
```

---

## Task File: `tasks.yaml`

Generated by the planner agent, claimed by workers via their unique ID.

```yaml
# tasks.yaml - Generated by planner, managed by Blue Flame
version: 1
session_id: "ses-20260206-143022"
wave: 2                             # Current wave number

tasks:
  - id: "task-001"
    title: "Add JWT middleware to API router"
    description: |
      Create JWT validation middleware in pkg/middleware/auth.go.
      Must validate tokens from Authorization header.
      Return 401 on invalid/expired tokens.
    status: "pending"               # pending | claimed | done | failed
    agent_id: null                  # Worker claims by writing their ID
    priority: 1                     # Lower = higher priority
    dependencies: []                # Task IDs that must complete first
    file_locks:                     # Files/dirs this task needs exclusive access to
      - "pkg/middleware/"
      - "internal/auth/"
    worktree: null                  # Populated when claimed
    branch: null                    # Populated when claimed
    result:
      status: null                  # pass | fail (set by validator)
      notes: ""

  - id: "task-002"
    title: "Add auth tests"
    status: "pending"
    agent_id: null
    priority: 2
    dependencies: ["task-001"]      # Blocked until task-001 completes
    file_locks:
      - "tests/auth/"
    # ...
```

---

## Wave Orchestration Flow

### Wave 1: Planning

1. Blue Flame spawns a single `claude` CLI process as the **Planner**
2. Planner receives: the human's task description + project context + blueflame.yaml constraints
3. Planner uses superpowers `/brainstorm` and `/write-plan` skills
4. Planner outputs a `tasks.yaml` with decomposed tasks, dependencies, file locks
5. Blue Flame presents the plan to the human via CLI prompt
6. Human approves, edits, or rejects → loop until approved

### Wave 2: Development (Workers)

1. Blue Flame reads `tasks.yaml`, identifies ready tasks (no unmet dependencies)
2. For each ready task (up to `max_workers`):
   a. Generate unique agent ID: `worker-<short-hash>` (e.g., `worker-a1b2c3`)
   b. Create git worktree: `git worktree add -b task-001 .trees/worker-a1b2c3 main`
   c. Acquire file locks for the task's `file_locks` entries
   d. Generate watcher hook script from config (see Watcher section)
   e. Create per-agent `.claude/` settings dir with watcher hooks
   f. Spawn `claude` CLI in the worktree directory with focused prompt
   g. Agent claims task by Blue Flame writing agent ID to `tasks.yaml`
3. Blue Flame monitors all workers:
   - Heartbeat checks via process liveness (every `heartbeat_interval`)
   - Timeout enforcement (kill after `agent_timeout`)
   - Stdout/stderr capture for logging
4. As each worker completes:
   - Worker commits changes to its worktree branch
   - Blue Flame marks task `done` or `failed` in `tasks.yaml`
   - File locks released
5. Once ALL workers complete → transition to Wave 3

### Wave 3: Validation

1. For each completed task (status=done):
   a. Spawn a **Validator** agent in the task's worktree
   b. Validator reviews the diff (`git diff main...<branch>`)
   c. Validator runs tests if applicable
   d. Validator checks: does the work match the task description? Any regressions?
   e. Validator marks result as `pass` or `fail` with notes
2. Failed tasks: human is notified, can retry or skip
3. Once ALL validations complete → transition to Wave 4

### Wave 4: Merge

1. Spawn a single **Merger** agent
2. Merger sees all validated branches and their diffs
3. Merger creates one or more cohesive changesets:
   - Groups related changes
   - Resolves any cross-task conflicts
   - Creates clean commit messages
4. Blue Flame presents changesets to human:
   - Shows diff summary for each changeset
   - Human approves/rejects each one (changeset chaining)
5. Approved changesets merged to base branch
6. Worktrees cleaned up
7. If more tasks remain in `tasks.yaml` → repeat from Wave 2

### Changeset Chaining

After Wave 4, if multiple changesets are produced:
```
Changeset 1/3: Add JWT middleware
  [12 files changed, +340, -12]
  (a)pprove / (r)eject / (v)iew diff / (s)kip? a

Changeset 2/3: Add auth tests
  [4 files changed, +180, -0]
  (a)pprove / (r)eject / (v)iew diff / (s)kip? a

Changeset 3/3: Update API docs
  [2 files changed, +45, -3]
  (a)pprove / (r)eject / (v)iew diff / (s)kip? v
  [shows full diff]
  (a)pprove / (r)eject? a

All changesets approved. Merging to main...
```

---

## Watcher Implementation

Each worker agent gets a generated shell script hook that enforces `blueflame.yaml` permissions. The Go binary generates these from `templates/watcher.sh.tmpl`.

**How it works:**
1. Blue Flame reads `blueflame.yaml` permissions
2. For each worker, generates a `watcher.sh` script in the agent's temp `.claude/` dir
3. The script is registered as a `PreToolUse` hook in the agent's `.claude/settings.json`
4. On every tool call, Claude Code pipes JSON to the watcher on stdin
5. Watcher checks:
   - Is this tool in `allowed_tools`? If not → exit 2 (block)
   - If Bash: does command match `blocked_patterns`? → exit 2
   - If Edit/Write: is the file path in `allowed_paths` and not in `blocked_paths`? → exit 2
6. Watcher logs all decisions to `logs/<agent-id>.audit.jsonl`

**Generated hook config** (placed in per-agent settings):
```json
{
  "hooks": {
    "PreToolUse": [
      {
        "type": "command",
        "command": "/tmp/blueflame/<agent-id>/watcher.sh",
        "timeout": 5000
      }
    ]
  }
}
```

**Watcher script behavior** (simplified):
```bash
#!/bin/bash
# Auto-generated by Blue Flame. Do not edit.
INPUT=$(cat)  # JSON from Claude Code
TOOL=$(echo "$INPUT" | jq -r '.tool_name')
# Check tool allowlist, path restrictions, bash patterns...
# Exit 0 = allow, Exit 2 = block
```

---

## Agent Lifecycle Management

### Process Tracking
- Each agent tracked in a `registry` map: `agentID → {PID, worktree, taskID, startTime, lastHeartbeat}`
- Registry persisted to `.blueflame/agents.json` for crash recovery

### Heartbeat & Liveness
- Goroutine per agent checks `/proc/<pid>/stat` (or `kill -0`) every `heartbeat_interval`
- If process dead → mark task as `failed`, release locks, clean worktree

### Graceful Shutdown
- On SIGINT/SIGTERM to Blue Flame:
  1. Send SIGTERM to all agent processes
  2. Wait 10s for graceful exit
  3. Send SIGKILL to survivors
  4. Clean up worktrees and locks
  5. Persist state to `.blueflame/` for recovery

### Orphan Prevention
- Blue Flame uses process groups (`Setpgid: true` in `exec.Cmd.SysProcAttr`)
- On startup, checks `.blueflame/agents.json` for stale entries from previous crashes
- Kills any surviving orphan processes
- Cleans up stale worktrees and locks

### Timeout Enforcement
- Context with deadline per agent (`context.WithTimeout`)
- On timeout: SIGTERM → 5s grace → SIGKILL
- Task marked `failed`, available for retry

---

## File/Directory Locking

### Lock Acquisition
- Each task declares `file_locks` (paths it needs exclusive access to)
- Before spawning a worker, Blue Flame acquires advisory locks via `flock`
- Lock files stored in `.blueflame/locks/` (e.g., `.blueflame/locks/pkg_middleware.lock`)
- Path-based: lock granularity is per-directory or per-file as declared in task

### Conflict Prevention
- Planner is instructed to decompose tasks such that file_locks don't overlap
- If locks conflict between ready tasks → tasks run in sequence, not parallel
- Blue Flame checks for lock conflicts before spawning each worker

### Lock Release
- Locks released when worker completes (success or failure)
- On crash: Blue Flame's orphan cleanup also releases stale locks

---

## Agent System Prompts

Each agent type gets a focused, minimal prompt to reduce token usage.

**Planner**: "You are a senior developer breaking down a feature request into isolated tasks. Each task must have clear file boundaries (for locking), explicit dependencies, and be completable in a single focused session. Output tasks in the YAML schema provided. Use superpowers /brainstorm and /write-plan."

**Worker**: "You are a developer implementing task [ID]: [description]. Work ONLY in your worktree. Touch ONLY files listed in your task. Follow TDD via superpowers. Commit your changes when done with a clear commit message referencing your task ID."

**Validator**: "You are a QA engineer reviewing task [ID]. Check: (1) Does the diff match the task description? (2) Do tests pass? (3) Any regressions or code quality issues? Output pass/fail with specific notes."

**Merger**: "You are a release engineer. Review these validated branches: [list]. Create clean, cohesive changesets. Resolve any conflicts. Group related changes. Write clear commit messages."

---

## Implementation Phases (Build Order)

### Phase 1: Core Foundation (MVP)
Files: `cmd/blueflame/main.go`, `internal/config/`, `internal/tasks/`, `internal/agent/agent.go`
- Parse `blueflame.yaml`
- Parse/write `tasks.yaml`
- Spawn a single claude CLI process with custom prompt
- Basic agent lifecycle (start, wait, timeout, kill)
- **Milestone**: Can run a planner agent and produce a task YAML

### Phase 2: Worktrees & Locking
Files: `internal/worktree/`, `internal/locks/`
- Create/remove git worktrees per agent
- Advisory file locking via flock
- Lock conflict detection
- **Milestone**: Workers run in isolated worktrees with file locks

### Phase 3: Watchers
Files: `internal/agent/hooks.go`, `templates/watcher.sh.tmpl`
- Generate watcher hook scripts from config
- Per-agent `.claude/settings.json` with hooks
- Audit logging
- **Milestone**: Agents are constrained by config permissions

### Phase 4: Wave Orchestration
Files: `internal/orchestrator/`
- Full wave loop: plan → work → validate → merge
- Parallel worker management (goroutines + channels)
- Wave transitions with human approval gates
- **Milestone**: Full wave cycle works end-to-end

### Phase 5: Human Experience & Polish
Files: `internal/ui/`, `internal/agent/lifecycle.go`
- Changeset presentation and chaining
- Crash recovery from `.blueflame/` state
- Orphan detection and cleanup on startup
- Progress display during waves
- **Milestone**: Production-ready orchestrator

---

## Verification Plan

1. **Unit tests**: Each `internal/` package gets `_test.go` files covering core logic
2. **Integration test**: Run Blue Flame against a small test repo with a simple task (e.g., "add a hello world function and test"), verify full wave cycle
3. **Watcher test**: Attempt a blocked operation (e.g., edit `.env`), verify watcher blocks it
4. **Lifecycle test**: Kill a worker mid-execution, verify Blue Flame detects it, cleans up worktree/locks
5. **Lock test**: Two tasks with overlapping file_locks → verify they run sequentially
6. **Changeset test**: Multiple workers complete → verify merger produces reviewable changesets

---

## Key Files to Create

| File | Purpose |
|------|---------|
| `cmd/blueflame/main.go` | CLI entrypoint, flag parsing, orchestrator init |
| `internal/config/config.go` | YAML config parsing, validation, defaults |
| `internal/tasks/tasks.go` | YAML task file read/write/claim operations |
| `internal/agent/agent.go` | Claude CLI process spawn, communication |
| `internal/agent/lifecycle.go` | PID tracking, heartbeat, timeout, cleanup |
| `internal/agent/hooks.go` | Generate watcher scripts from config |
| `internal/worktree/worktree.go` | Git worktree create/remove/list |
| `internal/locks/locks.go` | Advisory file locking via flock |
| `internal/orchestrator/orchestrator.go` | Wave loop, transitions, human gates |
| `internal/ui/prompt.go` | CLI prompts for plan approval, changeset review |
| `templates/watcher.sh.tmpl` | Go template for watcher hook shell scripts |
| `blueflame.yaml` | Example configuration |

## Existing Tools Leveraged

| Tool | Role | Why |
|------|------|-----|
| `claude` CLI | Agent runtime | Built-in tools, hook support, superpowers compatibility |
| Superpowers plugin | Agent skills | Planning, TDD, code review, debugging - already battle-tested |
| Git worktrees | Worker isolation | File-level isolation, shared object DB, branch-per-task |
| `flock` (syscall) | File locking | Lightweight, advisory, built into OS |
| YAML | Config + tasks | Human-readable, simple, no external deps |
