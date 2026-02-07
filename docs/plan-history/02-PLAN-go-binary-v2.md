# Blue Flame v2: Wave-Based Multi-Agent Orchestration (Claude Code Skill)

## Context

Building a multi-agent orchestration system inspired by Steve Yegge's Gastown but more straightforward. The mental model is **human as project lead/architect** directing a development team of AI agents. The system uses wave-based execution (plan → work → validate → merge), strict permission enforcement via watchers, git worktree isolation, and YAML-based task claiming.

**Key shift from v1 plan**: Blue Flame is a **Claude Code skill/plugin**, not a standalone Go binary. This means faster iteration, no compilation, direct integration with superpowers, and reliance on Claude Code's existing infrastructure (Task tool for subagents, hooks for watchers, Bash for system operations).

**Key dependencies**: `claude` CLI (runtime), Superpowers plugin (skills), git worktrees (isolation), Beads (persistent memory), shell scripts (mechanical operations).

---

## Architecture Overview

```
Human (Project Lead)
    │
    ▼
┌─────────────────────────────────────┐
│  Blue Flame (Claude Code Skill)      │
│  - Reads blueflame.yaml config      │
│  - Manages waves & agent lifecycle  │
│  - Calls shell helpers for mechops  │
│  - Dispatches agents via Task tool  │
│  - Presents changesets for review   │
│  - Archives results to Beads        │
└──────────┬──────────────────────────┘
           │ spawns via Task tool
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
│  (concurrency configurable per wave)         │
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
    Archive to Beads → Repeat (next wave cycle)
```

---

## Skill Structure

```
blueflame/
├── SKILL.md                        # Main orchestrator skill definition
├── templates/
│   ├── planner-prompt.md           # Prompt template for planner agents
│   ├── worker-prompt.md            # Prompt template for worker agents
│   ├── validator-prompt.md         # Prompt template for validator agents
│   └── merger-prompt.md            # Prompt template for merger agents
├── scripts/
│   ├── blueflame-init.sh           # Setup: create dirs, validate prereqs
│   ├── worktree-manage.sh          # Create/remove/list worktrees
│   ├── lock-manage.sh              # Acquire/release/check advisory locks
│   ├── watcher-generate.sh         # Generate per-agent watcher hook scripts
│   ├── token-tracker.sh            # Token budget tracking from audit logs
│   └── beads-archive.sh            # Archive session results to beads
├── hooks/
│   └── watcher.sh.tmpl             # Template for generated watcher scripts
└── blueflame.yaml.example          # Example configuration
```

---

## Configuration: `blueflame.yaml`

The human (project lead) writes this before any session. It defines what agents can and cannot do.

```yaml
# blueflame.yaml - Project lead's configuration

project:
  name: "my-project"
  repo: "/path/to/repo"
  base_branch: "main"
  worktree_dir: ".trees"

# Per-wave concurrency limits
concurrency:
  planning: 1                       # Always 1 planner
  development: 4                    # Max parallel workers
  validation: 2                     # Max parallel validators
  merge: 1                          # Always 1 merger

# Resource limits
limits:
  agent_timeout: 300s               # Max time per agent before kill
  heartbeat_interval: 30s           # Agent liveness check frequency
  max_retries: 2                    # Retries on agent failure
  token_budget:
    worker: 100000                  # Max tokens per worker agent
    validator: 30000                # Max tokens per validator
    planner: 50000                  # Max tokens per planner
    merger: 80000                   # Max tokens per merger
    warn_threshold: 0.8             # Warn at 80% of budget

# Model configuration (cost optimization)
models:
  planner: "sonnet"
  worker: "sonnet"
  validator: "haiku"
  merger: "sonnet"

# Permission rules - what agents CAN do
permissions:
  allowed_paths:
    - "src/**"
    - "tests/**"
    - "docs/**"
  blocked_paths:
    - ".env*"
    - "*.secret"
    - "blueflame.yaml"
    - ".claude/**"
    - "go.mod"
    - "go.sum"
  allowed_tools:
    - "Read"
    - "Write"
    - "Edit"
    - "Glob"
    - "Grep"
    - "Bash"
  blocked_tools:
    - "WebFetch"
    - "WebSearch"
    - "NotebookEdit"
  bash_rules:
    allowed_commands:
      - "go test"
      - "go build"
      - "go vet"
      - "make"
      - "git diff"
      - "git status"
      - "git log"
      - "git add"
      - "git commit"
    blocked_patterns:
      - "rm -rf"
      - "git push"
      - "git checkout main"
      - "curl|wget"
      - "sudo"
      - "chmod"
      - "docker"

# Output validation rules (Tier 1 - mechanical, enforced by watchers)
validation:
  commit_format:
    pattern: "^(feat|fix|refactor|test|docs|chore)\\(task-\\d+\\): .+"
    example: "feat(task-001): add JWT middleware"
  file_naming:
    style: "snake_case"              # snake_case | camelCase | PascalCase | kebab-case
    enforce_for:
      - "src/**/*.go"
      - "tests/**/*.go"
  require_tests:
    enabled: true
    source_patterns:                 # New source files matching these...
      - "src/**/*.go"
    test_patterns:                   # ...must have corresponding test files matching these
      - "tests/**/*_test.go"
      - "src/**/*_test.go"
  file_scope:
    enforce: true                    # Block changes outside task's declared file_locks

# Superpowers skills agents should use
superpowers:
  enabled: true
  skills:
    - "test-driven-development"
    - "systematic-debugging"
    - "requesting-code-review"

# Beads configuration (persistent memory)
beads:
  enabled: true
  archive_after_wave: true          # Archive results after each wave cycle
  include_failure_notes: true       # Store failure context for future sessions
  memory_decay: true                # Summarize old entries to save context
```

---

## Task File: `tasks.yaml`

Generated by the planner agent, claimed by workers via their unique ID. This is the runtime format - fast and simple. After each session, results are archived to Beads.

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
    status: "pending"               # pending | claimed | done | failed | requeued
    agent_id: null                  # Worker claims by writing their ID
    priority: 1
    dependencies: []
    file_locks:
      - "pkg/middleware/"
      - "internal/auth/"
    worktree: null
    branch: null
    result:
      status: null                  # pass | fail (set by validator)
      notes: ""
    history: []                     # Prior attempts (from re-queued tasks)

  - id: "task-002"
    title: "Add auth tests"
    status: "pending"
    agent_id: null
    priority: 2
    dependencies: ["task-001"]
    file_locks:
      - "tests/auth/"
    # ...
```

---

## Wave Orchestration Flow

### Wave 1: Planning

1. Orchestrator runs `blueflame-init.sh` to validate prereqs (git repo, beads installed, claude CLI, worktree dir)
2. If beads enabled, load prior session context (failure notes, patterns) via `beads-archive.sh load`
3. Spawn a single planner agent via Task tool with `planner-prompt.md`
4. Planner receives: human's task description + project context + blueflame.yaml constraints + prior session memory
5. Planner outputs a `tasks.yaml` with decomposed tasks, dependencies, file locks
6. Orchestrator presents the plan to the human for approval
7. Human approves, edits, or rejects → loop until approved

### Wave 2: Development (Workers)

1. Orchestrator reads `tasks.yaml`, identifies ready tasks (no unmet dependencies)
2. For each ready task (up to `concurrency.development`):
   a. Generate unique agent ID: `worker-<short-hash>` (e.g., `worker-a1b2c3`)
   b. Run `worktree-manage.sh create <agent-id> <base-branch>` for isolation
   c. Run `lock-manage.sh acquire <agent-id> <path1> <path2>...` for file locks
   d. Run `watcher-generate.sh <agent-id>` to create per-agent hooks from config
   e. Spawn worker via Task tool in the worktree directory with focused prompt
   f. Write agent ID to `tasks.yaml` (claim the task)
3. Orchestrator monitors all workers:
   - Liveness checks via PID files (every `heartbeat_interval`)
   - Timeout enforcement (kill after `agent_timeout`)
   - Token budget tracking via `token-tracker.sh`
4. As each worker completes:
   - Worker commits changes to its worktree branch
   - Orchestrator marks task `done` or `failed` in `tasks.yaml`
   - Run `lock-manage.sh release <agent-id>`
5. Once ALL workers complete → transition to Wave 3

### Wave 3: Validation

Two-tier validation. Tier 1 (mechanical) ran continuously during Wave 2 via watcher hooks. Tier 2 (semantic) runs now.

1. For each completed task (status=done), up to `concurrency.validation` at a time:
   a. Spawn a Validator agent (haiku - cheapest) in the task's worktree via Task tool
   b. Validator receives: task description + the diff (`git diff main...<branch>`)
   c. Validator checks:
      - **Task applicability**: Do the changes solve the stated problem? Are there unrelated changes?
      - **Correctness**: Does the implementation look right? Any logic errors?
      - **Regressions**: Do tests pass? Any broken behavior?
      - **Scope creep**: Changes stay within what was asked - no over-engineering
   d. Validator marks result as `pass` or `fail` with specific notes
2. Failed tasks: human is notified, can retry or skip
3. Once ALL validations complete → transition to Wave 4

### Wave 4: Merge

1. Spawn a single Merger agent via Task tool
2. Merger sees all validated branches and their diffs
3. Merger creates one or more cohesive changesets:
   - Groups related changes
   - Resolves any cross-task conflicts
   - Creates clean commit messages (following configured `commit_format`)
4. Orchestrator presents changesets to human (changeset chaining):
   ```
   Changeset 1/3: Add JWT middleware
     [12 files changed, +340, -12]
     (a)pprove / (r)eject / (v)iew diff / (s)kip? a

   Changeset 2/3: Add auth tests
     [4 files changed, +180, -0]
     (a)pprove / (r)eject / (v)iew diff / (s)kip? r
     → Re-queued as task-002 for next wave cycle

   Changeset 3/3: Update API docs
     [2 files changed, +45, -3]
     (a)pprove / (r)eject / (v)iew diff / (s)kip? a

   2 changesets approved, 1 re-queued. Merging...
   ```
5. Approved changesets merged to base branch
6. Rejected changesets → tasks marked `requeued` with rejection notes in `history`
7. Run `beads-archive.sh save` to persist session results
8. Worktrees cleaned up via `worktree-manage.sh cleanup`
9. If re-queued tasks or remaining tasks in `tasks.yaml` → repeat from Wave 2

---

## Two-Tier Validation System

### Tier 1: Mechanical (Watcher Hooks - Real-Time)

Enforced during Wave 2 by generated shell scripts registered as PreToolUse hooks. Fast, cheap, deterministic.

| Check | How |
|-------|-----|
| Tool allowlist | Is this tool in `allowed_tools`? Block if not. |
| Path restrictions | Is file in `allowed_paths` and not in `blocked_paths`? |
| Bash command filtering | Does command match `blocked_patterns`? |
| File scope | Are changes within the task's declared `file_locks`? |
| Commit message format | Does message match `commit_format.pattern`? |
| File naming | Do new files follow `file_naming.style`? |
| Test requirements | Does new source file have corresponding test? |
| Token budget | Estimated usage approaching `token_budget`? Warn at threshold, block at limit. |

All watcher decisions logged to `.blueflame/logs/<agent-id>.audit.jsonl`.

### Tier 2: Semantic (Validator Agents - Post-Work)

Enforced during Wave 3 by LLM-based validator agents. Catches what pattern matching cannot.

| Check | How |
|-------|-----|
| Task applicability | Does the diff address the task description? |
| Unrelated changes | Are there modifications that don't belong? |
| Correctness | Does the implementation logic make sense? |
| Regressions | Do existing tests still pass? |
| Scope creep | Did the agent over-engineer or add unrequested features? |

---

## Watcher Implementation

Each worker agent gets a generated shell script hook that enforces blueflame.yaml permissions.

**How it works:**
1. `watcher-generate.sh` reads blueflame.yaml and the task's file_locks
2. Generates a `watcher.sh` script in `.blueflame/hooks/<agent-id>/`
3. Creates a `.claude/settings.json` in the agent's worktree with:
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
4. On every tool call, Claude Code pipes JSON to the watcher on stdin
5. Watcher checks all Tier 1 rules
6. Exit 0 = allow, Exit 2 = block
7. All decisions appended to `.blueflame/logs/<agent-id>.audit.jsonl`

---

## Agent Lifecycle Management

Without Go process groups, lifecycle management uses PID files and shell helpers.

### Process Tracking
- Each agent tracked in `.blueflame/agents.json`: `{agentID, PID, worktree, taskID, startTime, lastHeartbeat, tokenUsage}`
- Updated by shell helpers as agents are spawned/completed

### Heartbeat & Liveness
- Orchestrator periodically runs `kill -0 <pid>` checks via Bash
- If process dead → mark task as `failed`, run `lock-manage.sh release`, clean worktree

### Graceful Shutdown
- On orchestrator interrupt:
  1. Kill all tracked agent PIDs
  2. Release all locks
  3. Persist state to `.blueflame/state.yaml` for recovery

### Orphan Prevention
- `blueflame-init.sh` checks `.blueflame/agents.json` for stale entries on startup
- Kills surviving orphan processes
- Cleans up stale worktrees and locks

### Timeout Enforcement
- Orchestrator tracks agent start times
- On timeout: kill PID, mark task `failed` (available for retry up to `max_retries`)

### Token Budget Enforcement
- `token-tracker.sh` estimates token usage from audit logs
- At 80% of budget: warning logged
- At 100% of budget: agent killed, task marked `failed`

---

## Beads Integration (Persistent Memory)

Tasks.yaml is the runtime format. Beads provides cross-session memory.

### After Each Wave Cycle

`beads-archive.sh save` runs and:
1. Creates a bead for each completed task with: description, result (pass/fail), validator notes, files changed, token usage
2. Creates a bead for each failed task with: failure reason, watcher audit trail, retry count
3. Creates a session summary bead: total tasks, pass/fail counts, cost estimate, duration

### Before Each Session

`beads-archive.sh load` provides the planner with:
1. Prior failure notes (what went wrong last time)
2. Patterns from past sessions (which task decompositions worked well)
3. Cost history (how much prior sessions cost)

### Memory Decay

Old closed beads are summarized to save context window space, per Beads' built-in memory decay feature.

---

## Agent Prompts (Minimal for Token Efficiency)

Each agent type gets a focused prompt. The orchestrator provides only the context each agent needs.

**Planner**: "You are a senior developer breaking down a feature request into isolated tasks. Each task must have clear file boundaries (for locking), explicit dependencies, and be completable in a single focused session. Output tasks in the YAML schema provided. Prior session context: [beads summary]. Use superpowers /brainstorm and /write-plan."

**Worker**: "You are a developer implementing task [ID]: [description]. Work ONLY in your worktree. Touch ONLY files listed in your task's file_locks. Your watcher will block out-of-scope changes. Follow TDD via superpowers. Commit with format: `type(task-ID): description`. Reference your task ID in all commits."

**Validator**: "You are a QA engineer reviewing task [ID]. The task asked for: [description]. The diff is below. Check: (1) Do the changes solve the stated problem? (2) Are there unrelated or out-of-scope changes? (3) Do tests pass? (4) Any regressions or logic errors? Output pass/fail with specific notes."

**Merger**: "You are a release engineer. Review these validated branches: [list with diffs]. Create clean, cohesive changesets. Resolve any conflicts. Group related changes. Write clear commit messages following the project's commit format."

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

Compared to Gastown's ~$3,000/month with 20-30 agents, this targets single-digit dollars per session.

---

## Implementation Phases

### Phase 1: Shell Helpers & Config

Files: `scripts/*`, `blueflame.yaml.example`
- `blueflame-init.sh`: prereq validation (git, beads, claude CLI, worktree dir)
- `worktree-manage.sh`: create/remove/list git worktrees
- `lock-manage.sh`: advisory lock acquire/release/check via lock files
- `watcher-generate.sh`: generate per-agent watcher hooks from blueflame.yaml
- Config YAML parsing (validated by init script)
- **Milestone**: Shell helpers work standalone, can create worktrees and locks

### Phase 2: Watcher Hooks

Files: `hooks/watcher.sh.tmpl`, `scripts/token-tracker.sh`
- Watcher template covering all Tier 1 checks
- Token tracking from audit logs
- Audit logging to JSONL
- **Milestone**: A manually spawned claude agent is constrained by generated watchers

### Phase 3: Orchestrator Skill (SKILL.md)

Files: `SKILL.md`, `templates/*`
- Main orchestrator skill with full wave protocol
- Agent prompt templates for all 4 roles
- Task tool dispatch with per-wave concurrency
- Human approval gates (plan approval, changeset chaining)
- Re-queue logic for rejected changesets
- **Milestone**: Full wave cycle works end-to-end via the skill

### Phase 4: Beads Integration

Files: `scripts/beads-archive.sh`
- Archive session results to beads after each wave cycle
- Load prior session context for planner
- Memory decay for old entries
- **Milestone**: Planner receives context from prior sessions

### Phase 5: Lifecycle Hardening

Files: updates to `scripts/*`, `SKILL.md`
- PID file tracking and orphan cleanup on startup
- Timeout enforcement with kill
- Crash recovery from `.blueflame/state.yaml`
- Token budget hard-stop enforcement
- **Milestone**: System recovers gracefully from crashes and runaway agents

---

## Verification Plan

1. **Helper script tests**: Each script tested in isolation (create worktree, acquire lock, generate watcher)
2. **Watcher test**: Spawn a claude agent with generated watcher, attempt blocked operations, verify blocks
3. **End-to-end test**: Run blueflame skill against a small test repo with a simple task, verify full wave cycle
4. **Lifecycle test**: Kill a worker mid-execution, verify orchestrator detects it, cleans up
5. **Lock test**: Two tasks with overlapping file_locks → verify sequential execution
6. **Re-queue test**: Reject a changeset → verify task is re-queued with notes for next cycle
7. **Beads test**: Run two sessions → verify planner in session 2 receives context from session 1
8. **Token budget test**: Set a low token budget → verify agent is warned then killed at threshold

---

## Existing Tools Leveraged

| Tool | Role | Why |
|------|------|-----|
| `claude` CLI | Agent runtime | Built-in Task tool, hook support, superpowers compatibility |
| Superpowers plugin | Agent skills | Planning, TDD, code review, debugging - battle-tested |
| Git worktrees | Worker isolation | File-level isolation, shared object DB, branch-per-task |
| Beads | Persistent memory | Git-backed, agent-friendly, memory decay, hash IDs prevent merge collisions |
| Lock files | File locking | Simple, advisory, no external deps |
| YAML | Config + tasks | Human-readable, simple, no external deps |
| Shell scripts | Mechanical ops | No compilation, minimal deps, keeps orchestrator tokens low |
