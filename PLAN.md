# Blue Flame: Wave-Based Multi-Agent Orchestration System (v2 -- Go Binary)

## 1. Context

Blue Flame is a wave-based multi-agent orchestration system inspired by Steve Yegge's Gastown but designed to be more straightforward. The mental model is **human as project lead/architect** directing a small development team of AI agents. The system uses wave-based execution (plan, work, validate, merge), strict permission enforcement via watchers, git worktree isolation, and YAML-based task management. It is optimized for modest hardware and reduced token usage.

### Design Lineage

This plan synthesizes three prior documents and incorporates findings from eight independent reviews:

- **[01-PLAN-go-binary.md](docs/plan-history/01-PLAN-go-binary.md)** (original Go binary plan) -- provided the Go project structure, `exec.Command` spawning, `flock`-based locking, and process group lifecycle management.
- **[04-Plan-ADR-hybrid.md](docs/plan-history/04-Plan-ADR-hybrid.md)** (hybrid Claude Code Skill + shell scripts) -- provided the three-phase watcher enforcement model, two-tier validation, Beads integration, Superpowers skills, cohesion groups, changeset chaining, re-queue logic, per-agent token budgets, model tiering, and detailed blueflame.yaml configuration.
- **[binary-orchestrator-investigation.md](docs/plan-history/reviews/binary-orchestrator-investigation.md)** -- evaluated both approaches against all review findings and concluded that a Go binary orchestrator resolves 54% of identified issues outright, partially helps with 28%, and introduces zero new problems.

The eight reviews identified 65+ specific issues across architecture, goals alignment, extensibility, wave handling, budget/cost, machine power, testability, and cohesion. This plan addresses every critical and high-severity issue and most medium-severity ones.

### Architecture Decision: Go Binary Orchestrator

| Decision | Rationale |
|----------|-----------|
| Go binary as orchestrator (NOT Claude Code Skill) | Resolves non-determinism (A2), context window exhaustion (A5), Task tool ambiguity (A6, C2, C4, C12), God Object risk (A1, A8), ulimit propagation gap (C3), untestable SKILL.md (T4), and eliminates $0.20-$0.50 per-session orchestrator token overhead (B3). |
| `claude` CLI for agent spawning | Workers, validators, and merger use `--print` (non-interactive). Planner optionally runs in interactive mode for brainstorm Q/A (`planning.interactive`). Provides explicit `--model`, `--system-prompt`, `--allowed-tools`, `--max-budget-usd`, `--output-format json`, `--json-schema`, `--settings` flags. [FEEDBACK: interactive planning] |
| Shell scripts retained ONLY for watcher hooks | Claude Code `PreToolUse` hooks must be external commands. All other shell scripts absorbed into Go packages. |
| All 04-Plan-ADR-hybrid.md design elements preserved | Wave-based execution, three-phase watchers, two-tier validation, git worktree isolation, advisory locking, YAML config, per-agent budgets, Beads, Superpowers, model tiering, human gates, changeset chaining, cohesion groups, re-queue logic, ephemeral agents, audit logging, defense-in-depth security. |

---

## 2. Architecture Overview

```
Human (Project Lead)
    |
    v
+-----------------------------------------------+
|  Blue Flame (compiled Go binary)                |
|  - Reads blueflame.yaml config                 |
|  - Deterministic wave state machine            |
|  - Spawns agents via: claude --print / interactive|
|  - Manages worktrees, locks, lifecycle         |
|  - Generates per-agent watcher hook scripts    |
|  - Presents changesets for human review        |
|  - Archives results to Beads                   |
|  - In-memory state, atomic persistence         |
|  - Zero token cost for orchestration           |
+-------------------+---------------------------+
                    | exec.Command("claude", ...)
                    v
+-----------------------------------------------+
|  Wave 1: PLANNING                             |
|  +---------+                                  |
|  | Planner | -> produces tasks.yaml           |
|  +---------+    -> human approves/edits       |
|-----------------------------------------------+
|  Wave 2: DEVELOPMENT                          |
|  +----------+ +----------+ +----------+       |
|  | Worker 1 | | Worker 2 | | Worker N |       |
|  | +Watcher | | +Watcher | | +Watcher |       |
|  | worktree | | worktree | | worktree |       |
|  +----------+ +----------+ +----------+       |
|  (goroutines + channels, max concurrency)     |
|-----------------------------------------------+
|  Wave 3: VALIDATION                           |
|  +-----------+ +-----------+                  |
|  |Validator 1| |Validator N|  (1 per task)    |
|  +-----------+ +-----------+                  |
|-----------------------------------------------+
|  Wave 4: MERGE                                |
|  +--------+                                   |
|  | Merger | -> cohesive changeset(s)          |
|  +--------+ -> human reviews & approves       |
+-----------------------------------------------+
    |
    v
Archive to Beads -> Repeat (if tasks remain)
```

Key properties of this architecture:

- **The orchestrator is NOT a Claude session.** It does not count toward the agent limit. During Wave 2 with 4 workers, there are exactly 4 `claude` processes, not 5. (Addresses Goals Alignment R2/C1.)
- **Zero token cost for orchestration.** Every operation the orchestrator performs (worktree creation, lock management, heartbeat checks, state transitions, human prompts) is a Go function call at zero API cost. (Addresses Budget B3.)
- **No context window.** The orchestrator holds state in typed Go structs. It can run for hours without degradation. (Addresses Architecture A5, Machine Power M3.)
- **Deterministic control flow.** The wave state machine executes identically every time. No risk of skipping a transition, calling functions in the wrong order, or misinterpreting task status. (Addresses Architecture A2.)

---

## 3. Project Structure

```
blueflame/
+-- cmd/
|   +-- blueflame/
|       +-- main.go                    # CLI entrypoint, flag parsing, signal handling
+-- internal/
|   +-- config/
|   |   +-- config.go                  # Parse and validate blueflame.yaml (typed structs)
|   |   +-- config_test.go             # Config parsing tests (valid, invalid, defaults, migration)
|   |   +-- defaults.go                # Default configuration values
|   |   +-- migrate.go                 # Schema version migration (v1 -> v2, etc.)
|   |   +-- migrate_test.go            # Migration tests
|   +-- orchestrator/
|   |   +-- orchestrator.go            # Wave state machine, main loop, signal handling
|   |   +-- orchestrator_test.go       # State machine tests (table-driven, mock spawner)
|   |   +-- planner.go                 # Wave 1: spawn planner, parse output, present plan
|   |   +-- planner_test.go
|   |   +-- worker.go                  # Wave 2: spawn workers, monitor, collect results
|   |   +-- worker_test.go
|   |   +-- validator.go               # Wave 3: spawn validators, collect verdicts
|   |   +-- validator_test.go
|   |   +-- merger.go                  # Wave 4: spawn merger, present changesets
|   |   +-- merger_test.go
|   |   +-- scheduler.go               # Task selection: dependency graph, lock conflicts, priority
|   |   +-- scheduler_test.go          # Dependency graph traversal, cascading failure tests
|   +-- agent/
|   |   +-- spawner.go                 # AgentSpawner interface + production implementation
|   |   +-- spawner_test.go            # Mock spawner tests
|   |   +-- lifecycle.go               # Process group tracking, heartbeat goroutines, timeout
|   |   +-- lifecycle_test.go
|   |   +-- hooks.go                   # Generate per-agent watcher shell scripts from config
|   |   +-- hooks_test.go              # Snapshot tests for generated scripts
|   |   +-- postcheck.go               # Post-execution filesystem diff validation
|   |   +-- postcheck_test.go          # Tests with synthetic worktrees
|   |   +-- sandbox_linux.go           # Linux: cgroups v2 for memory, unshare --net
|   |   +-- sandbox_darwin.go          # macOS: best-effort limits, document limitations
|   |   +-- sandbox_test.go            # Platform-conditional sandbox tests
|   |   +-- tokens.go                  # Token/cost tracking from claude JSON output
|   |   +-- tokens_test.go             # Parsing tests with fixture data
|   +-- tasks/
|   |   +-- tasks.go                   # Read/write/claim tasks, state transitions, atomic writes
|   |   +-- tasks_test.go              # State machine transition tests (table-driven)
|   |   +-- dependency.go              # Dependency graph resolution, cascading failure
|   |   +-- dependency_test.go         # Graph tests: linear, diamond, circular detection
|   +-- worktree/
|   |   +-- worktree.go                # Git worktree create/remove/list, branch naming
|   |   +-- worktree_test.go           # Tests with temp git repos
|   +-- locks/
|   |   +-- locks.go                   # flock-based advisory locking, conflict detection
|   |   +-- locks_test.go              # Acquisition, conflict, stale detection, concurrent tests
|   +-- memory/
|   |   +-- provider.go                # MemoryProvider interface (Save, Load)
|   |   +-- beads.go                   # Beads CLI implementation
|   |   +-- beads_test.go              # Interface contract tests
|   |   +-- noop.go                    # No-op implementation (when beads disabled)
|   +-- ui/
|   |   +-- prompt.go                  # Human approval prompts (plan, changeset, session)
|   |   +-- prompt_test.go             # Tests with programmatic input (Humble Object)
|   |   +-- diff.go                    # Diff display formatting
|   |   +-- progress.go                # Wave progress display (running agents, time, cost)
|   +-- state/
|   |   +-- state.go                   # Crash recovery: persist/restore orchestrator state
|   |   +-- state_test.go              # Serialize/deserialize, recovery scenario tests
|   +-- sanitize/
|       +-- sanitize.go                # Prompt injection mitigation for task content
|       +-- sanitize_test.go           # Injection pattern tests
+-- templates/
|   +-- watcher.sh.tmpl                # Go text/template for per-agent watcher hook scripts
|   +-- sandbox_darwin.sb.tmpl         # macOS sandbox-exec profile template
|   +-- planner-prompt.md.tmpl         # Planner system prompt template (non-interactive)
|   +-- planner-interactive-prompt.md.tmpl  # Planner system prompt template (interactive w/ brainstorm) [FEEDBACK]
|   +-- worker-prompt.md.tmpl          # Worker system prompt template
|   +-- validator-prompt.md.tmpl       # Validator system prompt template
|   +-- merger-prompt.md.tmpl          # Merger system prompt template
+-- testdata/
|   +-- configs/                       # Test fixture blueflame.yaml files
|   +-- tasks/                         # Test fixture tasks.yaml files
|   +-- hook-inputs/                   # Synthetic PreToolUse JSON payloads
|   +-- audit-logs/                    # Synthetic JSONL audit log fixtures
+-- blueflame.yaml.example             # Complete example configuration
+-- go.mod
+-- go.sum
+-- Makefile                           # build, test, lint, install targets
```

### Package Responsibilities

| Package | Responsibility | Key Interfaces |
|---------|---------------|----------------|
| `config` | Parse, validate, migrate blueflame.yaml | `Config` struct, `Load()`, `Validate()` |
| `orchestrator` | Wave state machine, human gates, main loop | `Orchestrator`, `Run()`, wave methods |
| `agent` | Spawn claude CLI processes, manage lifecycle | `AgentSpawner` interface, `Agent` struct |
| `tasks` | Task file I/O, state transitions, dependency resolution | `TaskStore`, `Task`, state transition methods |
| `worktree` | Git worktree CRUD | `WorktreeManager` interface |
| `locks` | Advisory file locking via flock | `LockManager` interface |
| `memory` | Persistent cross-session memory | `MemoryProvider` interface |
| `ui` | Human interaction (prompts, diffs, progress) | `Prompter` interface |
| `state` | Crash recovery persistence | `StateManager` |
| `sanitize` | Prompt injection mitigation | `SanitizeTaskContent()` |

---

## 4. Configuration: blueflame.yaml

```yaml
# blueflame.yaml - Project lead's configuration
# Written by the human before any blueflame session.

schema_version: 1                      # Schema version for migration support [FIX: E2]

project:
  name: "my-project"
  repo: "/path/to/repo"               # Target git repository
  base_branch: "main"                  # Branch to create worktrees from
  worktree_dir: ".trees"              # Directory for agent worktrees
  tasks_file: ".blueflame/tasks.yaml" # Explicit location [FIX: C1, cohesion #20]

# Per-wave concurrency limits
concurrency:
  planning: 1                          # Always 1 planner
  development: 4                       # Max parallel workers (1-4)
  validation: 2                        # Max parallel validators
  merge: 1                            # Always 1 merger
  adaptive: true                       # Auto-reduce based on available RAM [FIX: M4, #15]
  adaptive_min_ram_per_agent_mb: 600   # Minimum free RAM per agent before reducing concurrency

# Resource limits
limits:
  agent_timeout: 300s                  # Max wall-clock time per agent before kill
  heartbeat_interval: 30s             # Agent liveness check frequency
  max_retries: 2                       # Retries per task on agent failure
  max_wave_cycles: 5                   # Max wave cycles before forced stop [FIX: W4, #19]
  # Session-level budget circuit breaker [FIX: B4, #24]
  # At most ONE of max_session_cost_usd or max_session_tokens may be non-zero.
  # Both may be 0 (unlimited). Both non-zero is a config error.
  # [FEEDBACK: token-based budgets, 0 = unlimited]
  max_session_cost_usd: 10.00         # Session cost limit in USD (0 = unlimited)
  max_session_tokens: 0               # Session token limit (0 = unlimited, counts input+output)
  token_budget:
    # Per-agent budgets. USD budgets are enforced via claude --max-budget-usd flag.
    # Token budgets are enforced by the orchestrator (tracks JSON output token counts,
    # kills agent if exceeded). At most ONE unit (usd or tokens) may be non-zero per role.
    # Both may be 0 (unlimited). Both non-zero for the same role is a config error.
    # [FEEDBACK: token-based budgets, 0 = unlimited]
    planner_usd: 0.40                 # Max cost per planner agent (0 = unlimited)
    planner_tokens: 0                 # Alternative: max tokens per planner (0 = unlimited)
    worker_usd: 1.50                  # Max cost per worker agent (0 = unlimited)
    worker_tokens: 0                  # Alternative: max tokens per worker (0 = unlimited)
    validator_usd: 0.15               # Max cost per validator agent (0 = unlimited)
    validator_tokens: 0               # Alternative: max tokens per validator (0 = unlimited)
    merger_usd: 0.50                  # Max cost per merger agent (0 = unlimited)
    merger_tokens: 0                  # Alternative: max tokens per merger (0 = unlimited)
    warn_threshold: 0.8               # Warn at 80% of budget

# OS-level resource constraints (applied per agent at fork time)
sandbox:
  max_cpu_seconds: 600                # CPU time limit (ulimit -t)
  max_memory_mb: 2048                 # RSS limit via cgroups (Linux) [FIX: M2, #1]
  max_file_size_mb: 50                # Max single file size
  max_open_files: 1024                # File descriptors [FIX: M8]
  allow_network: false                # Agents may NEVER access the network

# Planning configuration
planning:
  interactive: true                    # Run planner in interactive mode with brainstorm Q/A [FEEDBACK]
                                       # true: planner runs interactively, human answers clarifying
                                       #   questions via superpowers brainstorm skill before plan output
                                       # false: planner runs via --print (one-shot, non-interactive)
                                       #   Use false for CI/automation or when task is well-defined

# Model configuration (cost optimization)
models:
  planner: "sonnet"                   # Good at decomposition and planning
  worker: "sonnet"                    # Primary work model
  validator: "haiku"                  # Cheapest - sufficient for focused review
  merger: "sonnet"                    # Needs context understanding for conflict resolution

# Permission rules
permissions:
  allowed_paths:                      # Glob patterns of files agents may touch
    - "src/**"
    - "tests/**"
    - "docs/**"
  blocked_paths:                      # Files agents must NEVER touch
    - ".env*"
    - "*.secret"
    - "*.key"
    - "blueflame.yaml"
    - ".claude/**"
    - ".blueflame/**"
    - "go.mod"
    - "go.sum"
    - "package-lock.json"
  allowed_tools:                      # Claude Code tools agents may use
    - "Read"
    - "Write"
    - "Edit"
    - "Glob"
    - "Grep"
    - "Bash"
  blocked_tools:                      # Tools agents must NEVER use
    - "WebFetch"
    - "WebSearch"
    - "NotebookEdit"
    - "Task"                          # Workers must not spawn sub-agents
  bash_rules:
    allowed_commands:                  # Prefix-match allowlist for bash
      - "go test"
      - "go build"
      - "go vet"
      - "go run"
      - "make"
      - "git diff"
      - "git status"
      - "git log"
      - "git add"
      - "git commit"
      - "npm test"
      - "npm run"
    blocked_patterns:                 # Regex patterns to block in bash
      - "rm\\s+-rf"
      - "git\\s+push"
      - "git\\s+checkout\\s+main"
      - "git\\s+merge"
      - "curl|wget"
      - "sudo"
      - "chmod"
      - "docker"
      - "nc\\s|netcat|ncat"
      - "ssh|scp|sftp"
      - "pip\\s+install|npm\\s+install|go\\s+get"

# Output validation rules (Tier 1 - mechanical, enforced by watcher hooks)
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
    enforce: true                     # Block changes outside task's declared file_locks
  # Diagnostic commands validators are allowed to run [FEEDBACK: validator diagnostic tooling]
  validator_diagnostics:
    enabled: true                      # Allow validators to run diagnostic commands
    commands:                          # Commands validators may execute via Bash
      - "go test ./..."
      - "go vet ./..."
      - "golangci-lint run"
      - "npm test"
      - "npm run lint"
      - "make test"
      - "make lint"
    timeout: 120s                      # Max time for each diagnostic command

# Superpowers configuration
superpowers:
  enabled: true
  skills:
    - "test-driven-development"
    - "systematic-debugging"
    - "requesting-code-review"
    - "brainstorm"                    # Used by planner [FIX: cohesion C8]
    - "write-plan"                    # Used by planner [FIX: cohesion C8]

# Beads configuration (persistent memory)
beads:
  enabled: true
  archive_after_wave: true
  include_failure_notes: true
  memory_decay: true
  decay_policy:
    summarize_after_sessions: 3       # Summarize closed tasks older than N sessions
    preserve_failures_sessions: 5     # Keep failure context longer

# Custom lifecycle hooks (optional, user-provided scripts)
hooks:
  post_plan: ""                       # Run after plan approval (e.g., notify team)
  pre_validation: ""                  # Run before validation wave (e.g., custom linter)
  post_merge: ""                      # Run after merge (e.g., trigger CI)
  on_failure: ""                      # Run on task failure (e.g., alert)
```

### Configuration Validation

On startup, the Go binary validates blueflame.yaml:
- `schema_version` must be present and supported. Missing = treated as v1 with warning. Newer than supported = hard failure with message.
- `project.repo` must be a valid git repository.
- `project.base_branch` must exist.
- `concurrency.development` must be 1-8.
- All path patterns must be valid globs.
- All regex patterns must compile.
- Exactly one of `max_session_cost_usd` or `max_session_tokens` must be non-zero, OR both may be 0 (unlimited). Both non-zero is an error. [FEEDBACK: token budgets, 0 = unlimited]
- For each agent role in `token_budget`, exactly one of the `_usd` or `_tokens` field must be non-zero, OR both may be 0 (unlimited). Both non-zero for the same role is an error.
- `max_wave_cycles` must be >= 1.
- `planning.interactive` must be a boolean.

Schema migration is handled by `internal/config/migrate.go`:

```go
func Migrate(raw []byte) (*Config, error) {
    var base struct { SchemaVersion int `yaml:"schema_version"` }
    yaml.Unmarshal(raw, &base)
    switch base.SchemaVersion {
    case 0, 1: return migrateV1(raw)
    case 2:    return parseV2(raw)
    default:   return nil, fmt.Errorf("unsupported schema_version %d (max supported: 2)", base.SchemaVersion)
    }
}
```

---

## 5. Task File: tasks.yaml

Located at the path specified by `project.tasks_file` (default: `.blueflame/tasks.yaml`). Generated by the planner, managed by the orchestrator. Agents never write to this file directly; only the Go orchestrator writes it (via atomic rename).

```yaml
# tasks.yaml - Generated by planner, managed exclusively by Blue Flame binary
schema_version: 1
session_id: "ses-20260206-143022"
wave_cycle: 1                          # Current wave cycle number

tasks:
  - id: "task-001"
    title: "Add JWT middleware to API router"
    description: |
      Create JWT validation middleware in pkg/middleware/auth.go.
      Must validate tokens from Authorization header.
      Return 401 on invalid/expired tokens.
    status: "pending"                  # pending | claimed | done | failed | blocked | requeued
    agent_id: null                     # Worker ID assigned by orchestrator when claimed
    priority: 1                        # Lower = higher priority
    cohesion_group: "auth"             # Logical grouping for merge ordering
    dependencies: []                   # Task IDs that must complete first
    file_locks:                        # Files/dirs this task needs exclusive access to
      - "pkg/middleware/"
      - "internal/auth/"
    worktree: null                     # Populated by orchestrator when claimed
    branch: null                       # Populated by orchestrator when claimed
    retry_count: 0                     # Incremented on each retry [FIX: cohesion gap]
    result:
      status: null                     # pass | fail (set by orchestrator from validator output)
      notes: ""
    history: []                        # Prior attempts: [{attempt, agent_id, timestamp, result, notes, rejection_reason}]

  - id: "task-002"
    title: "Add auth tests"
    description: |
      Write comprehensive tests for JWT middleware.
      Cover valid tokens, expired tokens, missing tokens, malformed tokens.
    status: "pending"
    agent_id: null
    priority: 2
    cohesion_group: "auth"
    dependencies: ["task-001"]         # Blocked until task-001 completes
    file_locks:
      - "tests/auth/"
    worktree: null
    branch: null
    retry_count: 0
    result:
      status: null
      notes: ""
    history: []
```

### Task State Machine

```
pending --> claimed --> done --> (validation pass) --> merged
    |           |         |         |
    |           |         |         +--> (validation fail) --> requeued --> pending
    |           |         |
    |           |         +--> (postcheck fail) --> failed --> (retry?) --> pending
    |           |
    |           +--> (agent crash/timeout/budget) --> failed --> (retry?) --> pending
    |
    +--> (dependency failed, retries exhausted) --> blocked
```

State transitions are methods on the `Task` struct, each with validation:

```go
func (t *Task) Claim(agentID, worktree, branch string) error
func (t *Task) Complete() error
func (t *Task) Fail(reason string) error
func (t *Task) MarkBlocked(reason string) error
func (t *Task) Requeue(notes string, history HistoryEntry) error
func (t *Task) SetValidationResult(status string, notes string) error
```

### History Entry Schema

```go
type HistoryEntry struct {
    Attempt         int       `yaml:"attempt"`
    AgentID         string    `yaml:"agent_id"`
    Timestamp       time.Time `yaml:"timestamp"`
    Result          string    `yaml:"result"`          // "failed", "rejected", "validation_failed"
    Notes           string    `yaml:"notes"`
    RejectionReason string    `yaml:"rejection_reason"` // Human's reason on changeset rejection
    CostUSD         float64   `yaml:"cost_usd"`
    TokensUsed      int       `yaml:"tokens_used"`     // Total input+output tokens [FEEDBACK: token budgets]
}
```

---

## 6. Agent Spawning Interface

The Go binary spawns agents via the `claude` CLI. Workers, validators, and the merger use `--print` (non-interactive). The planner optionally runs in interactive mode for brainstorm Q/A when `planning.interactive: true`. [FEEDBACK: interactive planning]

### Spawning a Worker

```go
func (s *ProductionSpawner) SpawnWorker(ctx context.Context, task *Task, cfg *Config) (*Agent, error) {
    // 1. Render the prompt from template, sanitizing task content
    prompt, err := s.renderPrompt("worker", task, cfg)
    if err != nil { return nil, fmt.Errorf("render prompt: %w", err) }

    // 2. Generate per-agent settings (watcher hook registration)
    settingsPath, err := s.generateSettings(task)
    if err != nil { return nil, fmt.Errorf("generate settings: %w", err) }

    // 3. Build system prompt
    systemPrompt, err := s.renderSystemPrompt("worker", task, cfg)
    if err != nil { return nil, fmt.Errorf("render system prompt: %w", err) }

    // 4. Assemble the command
    args := []string{
        "--print",
        "--model", cfg.Models.Worker,
        "--system-prompt", systemPrompt,
        "--allowed-tools", strings.Join(cfg.Permissions.AllowedTools, ","),
        "--disallowed-tools", strings.Join(cfg.Permissions.BlockedTools, ","),
        "--output-format", "json",
        "--settings", settingsPath,
        "--no-session-persistence",
        prompt,
    }

    // Per-agent budget: USD uses --max-budget-usd flag; token limits enforced
    // by orchestrator via JSON output tracking [FEEDBACK: token-based budgets]
    budget := cfg.Limits.TokenBudget.WorkerBudget() // returns BudgetSpec{Unit, Value}
    if budget.Unit == USD && budget.Value > 0 {
        args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", budget.Value))
    }
    // Token-based limits: orchestrator monitors agent's cumulative token count
    // from periodic JSON output checks and kills the process if exceeded.
    // Value of 0 = unlimited (no flag passed, no orchestrator enforcement).

    cmd := exec.CommandContext(ctx, "claude", args...)
    cmd.Dir = task.WorktreePath

    // 5. Set process group for clean cleanup
    cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

    // 6. Apply platform-specific resource limits at fork time [FIX: C3, M2]
    applySandboxLimits(cmd, cfg.Sandbox)

    // 7. Capture output
    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr

    // 8. Start the process
    if err := cmd.Start(); err != nil { return nil, fmt.Errorf("start agent: %w", err) }

    return &Agent{
        ID:        task.AgentID,
        Cmd:       cmd,
        Task:      task,
        Stdout:    &stdout,
        Stderr:    &stderr,
        Started:   time.Now(),
        Role:      RoleWorker,
        Budget:    budget,             // BudgetSpec (USD or tokens, 0 = unlimited) [FEEDBACK]
    }, nil
}
```

### Spawning a Validator (with structured output)

Validators use `--json-schema` to enforce structured pass/fail output:

```go
validatorSchema := `{
    "type": "object",
    "properties": {
        "status": {"type": "string", "enum": ["pass", "fail"]},
        "notes": {"type": "string"},
        "issues": {
            "type": "array",
            "items": {"type": "string"}
        }
    },
    "required": ["status", "notes"]
}`

args := []string{
    "--print",
    "--model", cfg.Models.Validator,
    "--system-prompt", validatorSystemPrompt,
    "--allowed-tools", "Read,Glob,Grep,Bash",        // Validators: read-only + diagnostic tooling [FIX: wave W3.2]
    "--disallowed-tools", "Write,Edit,WebFetch,WebSearch,NotebookEdit,Task",
    "--output-format", "json",
    "--json-schema", validatorSchema,
    "--settings", settingsPath,
    "--no-session-persistence",
    validatorPrompt,
}

// Validator budget: same USD/token duality as workers [FEEDBACK: token-based budgets]
budget := cfg.Limits.TokenBudget.ValidatorBudget()
if budget.Unit == USD && budget.Value > 0 {
    args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", budget.Value))
}

// Validator Bash access: restricted to diagnostic commands only [FEEDBACK: validator diagnostics]
// The validator's watcher hook allows ONLY commands from validation.validator_diagnostics.commands.
// This lets validators run tests, linters, scanners, etc. but not modify files.
```

### Spawning a Planner

The planner supports two modes based on `planning.interactive` config. [FEEDBACK: interactive planning]

**Non-interactive mode** (`planning.interactive: false`): uses `--print` with `--json-schema` for structured output.

**Interactive mode** (`planning.interactive: true`): runs an interactive `claude` session. The planner's system prompt includes the superpowers brainstorm skill, enabling Q/A with the human. The orchestrator captures the final structured output after the interactive session ends.

```go
func (s *ProductionSpawner) SpawnPlanner(ctx context.Context, desc string, prior string, cfg *Config) (*Agent, error) {
    if cfg.Planning.Interactive {
        return s.spawnInteractivePlanner(ctx, desc, prior, cfg)
    }
    return s.spawnNonInteractivePlanner(ctx, desc, prior, cfg)
}

// spawnInteractivePlanner runs claude in interactive mode with brainstorm skill.
// Human answers clarifying questions, then planner outputs structured JSON.
func (s *ProductionSpawner) spawnInteractivePlanner(ctx context.Context, desc string, prior string, cfg *Config) (*Agent, error) {
    systemPrompt, _ := s.renderSystemPrompt("planner-interactive", desc, cfg)
    settingsPath, _ := s.generateSettings(nil) // No task-specific watchers for planner

    // Interactive mode: no --print, no --json-schema
    // The planner's prompt instructs it to output JSON as its final message.
    // The orchestrator parses the last message from the session transcript after exit.
    args := []string{
        "--model", cfg.Models.Planner,
        "--system-prompt", systemPrompt,
        "--allowed-tools", "Read,Glob,Grep,Bash",
        "--disallowed-tools", "Write,Edit,WebFetch,WebSearch,NotebookEdit,Task",
        "--settings", settingsPath,
    }
    budget := cfg.Limits.TokenBudget.PlannerBudget()
    if budget.Unit == USD && budget.Value > 0 {
        args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", budget.Value))
    }
    cmd := exec.CommandContext(ctx, "claude", args...)
    cmd.Dir = cfg.Project.Repo
    cmd.Stdin = os.Stdin   // Human interacts directly
    cmd.Stdout = os.Stdout // Human sees planner output
    cmd.Stderr = os.Stderr
    // After session exits, orchestrator reads session transcript to extract
    // the final JSON task plan from the planner's last message.
    // ... start, wait, parse transcript, return Agent
}
```

Both modes use the same planner JSON schema to enforce valid task output:

```go
plannerSchema := `{
    "type": "object",
    "properties": {
        "tasks": {
            "type": "array",
            "items": {
                "type": "object",
                "properties": {
                    "id": {"type": "string"},
                    "title": {"type": "string"},
                    "description": {"type": "string"},
                    "priority": {"type": "integer"},
                    "cohesion_group": {"type": "string"},
                    "dependencies": {"type": "array", "items": {"type": "string"}},
                    "file_locks": {"type": "array", "items": {"type": "string"}}
                },
                "required": ["id", "title", "description", "priority", "file_locks"]
            }
        }
    },
    "required": ["tasks"]
}`
```

### Budget Types [FEEDBACK: token-based budgets]

```go
type BudgetUnit int
const (
    USD    BudgetUnit = iota
    Tokens
)

type BudgetSpec struct {
    Unit  BudgetUnit
    Value float64  // USD amount or token count
}

// TokenBudget methods return the active budget for each role.
// Exactly one of _usd or _tokens is non-zero (validated at config load).
// Both 0 = unlimited (returns BudgetSpec{USD, 0}).
func (tb *TokenBudget) WorkerBudget() BudgetSpec { ... }
func (tb *TokenBudget) PlannerBudget() BudgetSpec { ... }
func (tb *TokenBudget) ValidatorBudget() BudgetSpec { ... }
func (tb *TokenBudget) MergerBudget() BudgetSpec { ... }
```

### Agent Result Processing

When `cmd.Wait()` returns, the orchestrator processes results through multiple channels:

1. **Exit code**: Non-zero = agent failure.
2. **Stdout JSON**: With `--output-format json`, stdout contains structured output including the agent's final response, token usage, and cost.
3. **Git commits**: The orchestrator runs `git log --oneline base_branch..agent_branch` to verify commits exist.
4. **Filesystem state**: Post-execution diff validation against allowed paths.

```go
func (o *Orchestrator) collectResult(agent *Agent) AgentResult {
    err := agent.Cmd.Wait()
    exitCode := 0
    if err != nil {
        var exitErr *exec.ExitError
        if errors.As(err, &exitErr) {
            exitCode = exitErr.ExitCode()
        }
    }

    var output ClaudeOutput
    json.Unmarshal(agent.Stdout.Bytes(), &output)

    return AgentResult{
        AgentID:    agent.ID,
        TaskID:     agent.Task.ID,
        ExitCode:   exitCode,
        Output:     output,
        CostUSD:    output.CostUSD,
        TokensUsed: output.InputTokens + output.OutputTokens, // [FEEDBACK: token budgets]
        Duration:   time.Since(agent.Started),
    }
}
```

### AgentSpawner Interface (for testing)

```go
type AgentSpawner interface {
    // SpawnPlanner spawns the planner. If cfg.Planning.Interactive is true, runs in
    // interactive mode with brainstorm Q/A; otherwise uses --print. [FEEDBACK: interactive planning]
    SpawnPlanner(ctx context.Context, description string, priorContext string, cfg *Config) (*Agent, error)
    SpawnWorker(ctx context.Context, task *Task, cfg *Config) (*Agent, error)
    SpawnValidator(ctx context.Context, task *Task, diff string, auditSummary string, cfg *Config) (*Agent, error)
    SpawnMerger(ctx context.Context, branches []BranchInfo, cfg *Config) (*Agent, error)
}
```

In tests, a `MockSpawner` returns agents with predetermined behavior (exit codes, output, delays). Note: interactive planner mode cannot be tested with `MockSpawner`; it requires `--decisions-file` fallback to non-interactive mode for automated testing.

---

## 7. Wave Orchestration Flow

### Main Loop

```go
func (o *Orchestrator) Run(ctx context.Context, taskDescription string) error {
    if err := o.initialize(ctx); err != nil { return err }

    // Wave 1: Planning
    plan, err := o.runPlanning(ctx, taskDescription)
    if err != nil { return o.handlePlanningFailure(err) }
    if !o.presentPlanForApproval(plan) { return ErrPlanRejected }

    // Wave cycles (development -> validation -> merge -> repeat)
    for cycle := 1; cycle <= o.config.Limits.MaxWaveCycles; cycle++ {
        o.state.WaveCycle = cycle

        // Check session budget circuit breaker [FIX: #24, FEEDBACK: 0 = unlimited]
        if err := o.checkBudgetCircuitBreaker(); err != nil {
            o.ui.Warn(err.Error())
            break
        }

        // Wave 2: Development
        results := o.runDevelopment(ctx)
        o.handleDevelopmentResults(results)

        // Wave 3: Validation
        validationResults := o.runValidation(ctx)
        o.handleValidationResults(validationResults)

        // Wave 4: Merge
        changesets := o.runMerge(ctx)
        o.presentChangesetsForReview(changesets)

        // Check if more work remains
        if !o.hasRemainingTasks() { break }

        // Human decides: continue, re-plan, or stop
        decision := o.ui.SessionContinuation(o.state)
        switch decision {
        case Continue: continue
        case Replan:
            plan, err = o.runPlanning(ctx, "Re-plan remaining tasks")
            if err != nil { return err }
            if !o.presentPlanForApproval(plan) { return ErrPlanRejected }
        case Stop: return nil
        }
    }
    return nil
}
```

### Wave 1: Planning

1. Orchestrator validates prerequisites:
   - Git repo exists and is clean (or has only blueflame-managed changes)
   - `claude` CLI is available and the correct version
   - Beads is installed (if `beads.enabled`)
   - Worktree directory is writable
   - Sufficient disk space for configured concurrency
   - Sufficient RAM for configured concurrency (adaptive check) [FIX: M4]
2. If Beads enabled, load prior session context via `MemoryProvider.Load()`
3. **Spawn planner based on `planning.interactive` config** [FEEDBACK: interactive planning with brainstorm]:

   **If `planning.interactive: true` (default):**
   - Spawn the planner as an interactive `claude` session (NOT `--print`)
   - The planner's system prompt includes the superpowers brainstorm skill
   - The planner engages the human in a Q/A flow: clarifying scope, constraints,
     preferences, and ambiguities before producing its plan
   - The human interacts directly with the planner via the terminal
   - Once the planner has gathered enough context, it produces structured JSON task output
   - The orchestrator captures the final JSON output from the interactive session
   - **Note**: Interactive mode costs tokens for the Q/A exchange (planner context window
     includes the conversation). Budget enforcement still applies via `token_budget.planner_*`.
   - **Note**: Interactive mode cannot be used with `--decisions-file` (automated testing).
     If `--decisions-file` is provided, the orchestrator falls back to non-interactive
     mode with a warning.

   **If `planning.interactive: false`:**
   - Spawn the planner via `claude --print` with `--json-schema` (one-shot, non-interactive)
   - Planner receives all context in a single prompt, produces structured output
   - Use this mode for CI/automation, well-defined tasks, or when Q/A is unnecessary

4. Planner receives: human's task description + project context + blueflame.yaml constraints + prior session memory (sanitized) [FIX: A3]
5. Planner produces structured JSON with task list
6. Orchestrator parses and validates the task list:
   - No circular dependencies (topological sort check)
   - All file_locks are within allowed_paths
   - Task IDs are unique
   - Dependency references are valid
7. Orchestrator presents the plan to the human for approval
8. Human approves, edits tasks.yaml directly, requests re-plan, or aborts
   - **Abort** option always available (no infinite loop) [FIX: wave #5]
   - **Re-plan** passes rejection notes to next planner invocation
   - Max re-plan attempts: 3, then suggest manual plan writing [FIX: wave #4]

### Wave 2: Development (Workers)

1. Orchestrator reads tasks from in-memory state, identifies ready tasks using the scheduler:
   ```go
   func (s *Scheduler) ReadyTasks(tasks []Task) []Task {
       // Sort by priority (ascending)
       // Filter: status == "pending" AND all dependencies met (status "done" or "merged")
       // Check lock availability against already-selected tasks
       // Return up to concurrency.development tasks
   }
   ```
   **Task selection algorithm** [FIX: wave #8]:
   - Sort all pending tasks by priority (lower = higher priority)
   - For each task in order: check if its file_locks conflict with any already-selected task
   - If conflict: skip (defer to next batch within this wave)
   - If no conflict: select
   - Stop when selected count reaches concurrency limit

2. For each selected task:
   a. **Claim the task first** (atomic in-memory + persist) [FIX: C6, #5]
   b. Generate unique agent ID: `<role>-<8-char-hex>` (e.g., `worker-a1b2c3d4`)
   c. Create git worktree: `git worktree add -b blueflame/<task-id> <worktree_dir>/<agent-id> <base_branch>`
   d. Acquire advisory file locks via `flock` [FIX: A9]
   e. Generate per-agent watcher hook script and `.claude/settings.json`
   f. **Spawn worker** via `claude --print` in the worktree directory
   g. If spawn fails: unclaim task, release locks, remove worktree (rollback) [FIX: C6]
   h. Register agent in lifecycle tracker (goroutine watches for completion)

3. Orchestrator monitors all workers concurrently via goroutines:
   ```go
   for _, agent := range runningAgents {
       go func(a *Agent) {
           result := o.collectResult(a)   // blocks on cmd.Wait()
           resultCh <- result             // send completion signal
       }(agent)
   }

   // Event-driven collection (no polling) [FIX: A10]
   for remaining > 0 {
       select {
       case result := <-resultCh:
           o.handleWorkerResult(result)
           remaining--
       case <-ctx.Done():
           o.killAllAgents()
           return
       }
   }
   ```
   Concurrently, a heartbeat goroutine runs:
   - Every `heartbeat_interval`: check each agent PID via `syscall.Kill(pid, 0)` [FIX: M5]
   - If process dead but no result received: treat as crash, mark failed
   - Timeout check: if `time.Since(agent.Started) > agent_timeout`, kill via process group

4. As each worker completes:
   - Parse JSON output for cost tracking
   - Run `postcheck` (filesystem diff validation)
   - If postcheck fails: task marked `failed`, worktree branch deleted, changes discarded
   - If postcheck passes AND exit code 0: task marked `done`
   - Release file locks
   - Accumulate session cost; check circuit breaker [FIX: #24]

5. After all spawned workers complete AND no more tasks are spawnable [FIX: W6]:
   ```go
   func (o *Orchestrator) developmentComplete() bool {
       return len(o.runningAgents) == 0 && len(o.scheduler.ReadyTasks(o.tasks)) == 0
   }
   ```

6. **Cascading dependency failure** [FIX: W1, #8]:
   When a task fails and retries are exhausted, traverse the dependency graph and mark all transitive dependents as `blocked`:
   ```go
   func (s *Scheduler) CascadeFailure(failedTaskID string, tasks []Task) {
       queue := []string{failedTaskID}
       for len(queue) > 0 {
           id := queue[0]; queue = queue[1:]
           for i := range tasks {
               if tasks[i].DependsOn(id) && tasks[i].Status != "blocked" {
                   tasks[i].MarkBlocked(fmt.Sprintf("dependency %s failed", failedTaskID))
                   queue = append(queue, tasks[i].ID)
               }
           }
       }
   }
   ```

7. If there are still spawnable tasks (deferred due to lock conflicts), spawn another batch and repeat step 2-6 within the same wave.

### Wave 3: Validation

Two-tier validation. Tier 1 (mechanical) ran continuously during Wave 2 via watcher hooks and post-execution checks. Tier 2 (semantic) runs now.

1. For each task with status `done` (passed Tier 1), up to `concurrency.validation` at a time:
   a. Spawn a validator agent (haiku model) in the task's worktree
   b. Validator tools: Read, Glob, Grep, Bash. Bash is restricted to diagnostic commands only (tests, linters, scanners) as configured in `validation.validator_diagnostics.commands`. [FIX: wave #15, FEEDBACK: validator diagnostics]
   c. Validator receives via prompt:
      - Task description (sanitized)
      - Diff: `git diff <base_branch>...<task_branch>`
      - Watcher audit summary (filtered, not raw JSONL -- top violations and stats only) [FIX: wave #16]
   d. Validator uses `--json-schema` to return structured `{status, notes, issues}`
   e. Orchestrator parses the structured output and updates the task's validation result

2. **Validator failure/timeout handling** [FIX: W2, #9]:
   ```go
   func (o *Orchestrator) handleValidatorFailure(task *Task, err error) {
       task.ValidationRetries++
       if task.ValidationRetries <= 1 {
           // Retry once
           o.requeueForValidation(task)
       } else {
           // Escalate to human
           decision := o.ui.ValidatorFailedPrompt(task, err)
           switch decision {
           case ManualReview:  // Human reviews diff directly
           case SkipTask:      task.Fail("validator_failed_exhausted")
           case RetryTask:     task.Requeue("validator failed, human requested retry", ...)
           }
       }
   }
   ```

3. Validation scheduling order: tasks validated in priority order within each cohesion group, groups processed in dependency order.

4. For failed validations, the human is prompted with three options:
   - **Re-queue with notes**: Task gets re-queued with human guidance in history [FIX: wave #17]
   - **Re-queue with edited description/file_locks**: Human can modify the task [FIX: wave #27]
   - **Drop task**: Task removed from this session

5. Partial cohesion group handling: if a cohesion group has some tasks pass and some fail, only passing tasks proceed to merge. The human is warned that the group is incomplete. [FIX: wave #30]

### Wave 4: Merge

1. Spawn a single merger agent in a fresh worktree based on the current base branch
2. Merger receives via prompt:
   - All validated branches and their diffs, grouped by `cohesion_group`
   - Inter-group dependency information
   - Instructions to create one changeset per cohesion group
   - Instruction to stop and report on unresolvable semantic conflicts
3. Merger creates cohesive changesets per cohesion group

4. **Changeset presentation order respects inter-group dependencies** [FIX: W3, wave #21]:
   If any task in group B depends on a task in group A, group A's changeset is presented first. If group A is rejected, group B is automatically deferred.

5. Orchestrator presents changesets to human (changeset chaining):
   ```
   === BLUE FLAME: Changeset Review ===
   Session: ses-20260206-143022 | Wave cycle: 1
   Session cost so far: $2.14

   Changeset 1/3: [auth] Add JWT middleware + auth tests
     [16 files changed, +520, -12]
     Tasks: task-001, task-002
     (a)pprove / (r)eject / (v)iew diff / (s)kip? a

   Changeset 2/3: [api] Update API route documentation
     [2 files changed, +45, -3]
     Tasks: task-003
     (a)pprove / (r)eject / (v)iew diff / (s)kip? r
     Rejection reason: > docs reference endpoints that don't exist yet
     -> Task task-003 re-queued with rejection notes

   Changeset 3/3: [api] Add rate limiting middleware
     [4 files changed, +180, -0]
     Tasks: task-004
     NOTE: Depends on rejected changeset [api]. Auto-deferred.

   1 changeset approved, 2 re-queued.
   ```

6. **"Skip" semantics** [FIX: wave #22]: Skip defers the changeset to the next wave cycle without recording a rejection reason. It is neither approval nor rejection -- the task remains in its current state and will be re-presented.

7. Approved changesets merged to base branch **in presentation order** [FIX: wave #25]. If a later changeset conflicts with the newly-merged base, the orchestrator detects this and re-queues it for the next cycle.

8. Run `MemoryProvider.Save()` to persist session results (tasks, outcomes, costs, failure notes)

9. Clean up all worktrees (except on error, for debugging)

10. Re-queued tasks carry full history for the next wave cycle

### Session Continuation

```
Wave cycle 1 complete.
  Approved: 1 changeset (merged to main)
  Re-queued: 2 tasks (task-003, task-004)
  Blocked: 0 tasks
  Session budget: $2.14 / $10.00 USD limit        (or: 12,450 / 50,000 token limit)

  (c)ontinue to next wave cycle
  (r)e-plan (return to Wave 1 for new plan)    [FIX: wave #31]
  (s)top
```

---

## 8. Three-Phase Watcher Enforcement

Defense in depth: three distinct enforcement phases ensure no agent can violate its constraints, even if one layer is bypassed.

### Phase 1: Pre-Execution (Real-Time Hook Enforcement)

Enforced continuously during agent execution by generated shell scripts registered as Claude Code `PreToolUse` hooks. Fast, cheap, deterministic, zero token cost.

The Go binary generates these from `templates/watcher.sh.tmpl` via `text/template`:

```go
tmpl, _ := template.ParseFiles("templates/watcher.sh.tmpl")
data := WatcherData{
    AgentID:              agent.ID,
    Role:                 agent.Role,
    AllowedTools:         cfg.Permissions.AllowedTools,
    BlockedTools:         cfg.Permissions.BlockedTools,
    AllowedPaths:         cfg.Permissions.AllowedPaths,
    BlockedPaths:         cfg.Permissions.BlockedPaths,
    AllowedCommands:      cfg.Permissions.BashRules.AllowedCommands,
    BlockedPatterns:      cfg.Permissions.BashRules.BlockedPatterns,
    FileLocks:            task.FileLocks,
    CommitPattern:        cfg.Validation.CommitFormat.Pattern,
    AuditLogPath:         filepath.Join(blueflameDir, "logs", agent.ID+".audit.jsonl"),
    DiagnosticCommands:   cfg.Validation.ValidatorDiagnostics.Commands, // [FEEDBACK: validator diagnostics]
    // For validators: Bash restricted to DiagnosticCommands only (AllowedCommands ignored).
    // The watcher template checks Role == "validator" and uses DiagnosticCommands as the
    // Bash allowlist instead of AllowedCommands.
}
tmpl.Execute(watcherFile, data)
```

| Check | How | Config Source |
|-------|-----|---------------|
| Tool allowlist | Is this tool in `allowed_tools`? Block if not. | `permissions.allowed_tools` |
| Tool blocklist | Is this tool in `blocked_tools`? Block if so. | `permissions.blocked_tools` |
| Path allow | Is file path within `allowed_paths` globs? | `permissions.allowed_paths` |
| Path block | Is file path in `blocked_paths` globs? Block. | `permissions.blocked_paths` |
| File scope | Is file within task's `file_locks`? | `task.file_locks` |
| Bash allowlist | Does command prefix-match `allowed_commands`? | `bash_rules.allowed_commands` |
| Bash blocklist | Does command match `blocked_patterns` regex? Block. | `bash_rules.blocked_patterns` |
| Commit format | Does commit message match pattern? | `validation.commit_format` |
| File naming | Do new files follow naming style? | `validation.file_naming` |

Generated hook config (placed in per-agent worktree `.claude/settings.json`):

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "type": "command",
        "command": "/absolute/path/.blueflame/hooks/worker-a1b2c3d4/watcher.sh",
        "timeout": 5000
      }
    ]
  }
}
```

All watcher decisions are logged to `.blueflame/logs/<agent-id>.audit.jsonl` with schema:

```json
{
  "timestamp": "2026-02-06T14:31:52Z",
  "agent_id": "worker-a1b2c3d4",
  "tool": "Write",
  "target": "src/auth/middleware.go",
  "decision": "allow",
  "rule": "path_allowed",
  "details": ""
}
```

### Phase 2: Runtime Constraints (OS-Level Sandbox)

Applied by the Go binary at fork time via `exec.Cmd.SysProcAttr`. These are hard limits that cannot be circumvented by the agent. **Platform-specific implementations use build tags** [FIX: #1, #2, M2].

#### Linux (`sandbox_linux.go`)

```go
//go:build linux

func applySandboxLimits(cmd *exec.Cmd, cfg SandboxConfig) {
    cmd.SysProcAttr = &syscall.SysProcAttr{
        Setpgid: true,
    }

    // Memory: Use cgroups v2 for RSS limiting (NOT ulimit -v which kills Node.js)
    if cgroupsV2Available() {
        createCgroupForAgent(cmd, cfg.MaxMemoryMB)  // Limits RSS, not virtual memory
    }

    // CPU time: ulimit -t works correctly on Linux
    // Set via wrapper script or rlimit
    setRlimit(cmd, syscall.RLIMIT_CPU, cfg.MaxCPUSeconds)

    // File size
    setRlimit(cmd, syscall.RLIMIT_FSIZE, cfg.MaxFileSizeMB * 1024 * 1024)

    // Open files
    setRlimit(cmd, syscall.RLIMIT_NOFILE, cfg.MaxOpenFiles)

    // Network: unshare --net creates isolated network namespace
    if !cfg.AllowNetwork {
        cmd.SysProcAttr.Cloneflags = syscall.CLONE_NEWNET
    }
}
```

#### macOS (`sandbox_darwin.go`)

```go
//go:build darwin

func applySandboxLimits(cmd *exec.Cmd, cfg SandboxConfig) {
    cmd.SysProcAttr = &syscall.SysProcAttr{
        Setpgid: true,
    }

    // Memory: macOS has NO reliable RSS limiting mechanism.
    // ulimit -v kills Node.js (V8 maps >1GB virtual). [FIX: #1]
    // Document this limitation. Rely on agent timeout + budget as backstops.
    // Log a warning if max_memory_mb is set.
    log.Warn("macOS: memory limiting is best-effort only; relying on timeout and budget enforcement")

    // CPU time: ulimit -t works on macOS
    setRlimit(cmd, syscall.RLIMIT_CPU, cfg.MaxCPUSeconds)

    // File size: works on macOS
    setRlimit(cmd, syscall.RLIMIT_FSIZE, cfg.MaxFileSizeMB * 1024 * 1024)

    // Open files: works on macOS
    setRlimit(cmd, syscall.RLIMIT_NOFILE, cfg.MaxOpenFiles)

    // Network: sandbox-exec is deprecated but still functional on macOS 15 (Sequoia).
    // Use it if available; warn if not. [FIX: #2]
    if !cfg.AllowNetwork {
        if sandboxExecAvailable() {
            // Generate sandbox profile from template, wrap the command
            profile := generateSandboxProfile(cfg)
            wrapWithSandboxExec(cmd, profile)
        } else {
            log.Warn("macOS: sandbox-exec unavailable; network isolation not enforced. " +
                     "Watcher hooks will block network tool calls but not raw network access.")
        }
    }
}
```

### Phase 3: Post-Execution Filesystem Diff Validation

Run by `internal/agent/postcheck.go` after each agent completes. Final safety net.

```go
func (p *PostChecker) Check(task *Task, cfg *Config) (*PostCheckResult, error) {
    // 1. Get list of all modified/added/deleted files in the worktree
    changes, _ := gitDiffNameStatus(task.WorktreePath, cfg.Project.BaseBranch, task.Branch)

    result := &PostCheckResult{Pass: true}

    for _, change := range changes {
        // 2. Check: file is within allowed_paths
        if !matchesAnyGlob(change.Path, cfg.Permissions.AllowedPaths) {
            result.AddViolation("path_not_allowed", change.Path)
        }
        // 3. Check: file is NOT in blocked_paths
        if matchesAnyGlob(change.Path, cfg.Permissions.BlockedPaths) {
            result.AddViolation("blocked_path_modified", change.Path)
        }
        // 4. Check: file is within task's file_locks scope
        if cfg.Validation.FileScope.Enforce && !withinFileLocks(change.Path, task.FileLocks) {
            result.AddViolation("outside_file_scope", change.Path)
        }
        // 5. Check: no unexpected binary files
        if isBinary(change.Path) && !expectedBinary(change.Path, cfg) {
            result.AddViolation("unexpected_binary", change.Path)
        }
        // 6. Check: no sensitive content (secrets, keys, tokens)
        if containsSensitiveContent(change.Path) {
            result.AddViolation("sensitive_content", change.Path)
        }
    }

    return result, nil
}
```

---

## 9. Agent Lifecycle Management

### Process Tracking

Each agent tracked in an in-memory registry with periodic persistence to `.blueflame/agents.json`:

```go
type AgentEntry struct {
    ID            string     `json:"id"`           // e.g., "worker-a1b2c3d4"
    PID           int        `json:"pid"`
    PGID          int        `json:"pgid"`
    Role          string     `json:"role"`         // "planner", "worker", "validator", "merger"
    WorktreePath  string     `json:"worktree"`
    TaskID        string     `json:"task_id"`
    StartTime     time.Time  `json:"start_time"`
    Status        string     `json:"status"`       // "running", "completed", "failed", "killed"
    CostUSD       float64    `json:"cost_usd"`
    TokensUsed    int        `json:"tokens_used"`  // Total input+output tokens [FEEDBACK: token budgets]
    Budget        BudgetSpec `json:"budget"`        // Configured budget for this agent [FEEDBACK]
}
```

### Agent ID Format

All agent roles use the same format: `<role>-<8-char-hex>`. Examples: `planner-f1e2d3c4`, `worker-a1b2c3d4`, `validator-b2c3d4e5`, `merger-c3d4e5f6`. [FIX: cohesion #14]

### Heartbeat and Liveness

A single goroutine monitors all running agents:

```go
func (lm *LifecycleManager) monitorLoop(ctx context.Context) {
    ticker := time.NewTicker(lm.heartbeatInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            for _, agent := range lm.runningAgents {
                // Liveness check (zero overhead, no tool calls) [FIX: M5]
                if err := syscall.Kill(agent.PID, 0); err != nil {
                    lm.handleAgentDeath(agent)
                }
                // Timeout check
                if time.Since(agent.StartTime) > lm.agentTimeout {
                    lm.killAgent(agent, "timeout")
                }
                // Stall detection: check audit log last-modified time
                if lm.isStalled(agent) {
                    log.Warnf("Agent %s appears stalled (no activity for %v)", agent.ID, lm.stallThreshold)
                }
            }
        case <-ctx.Done():
            return
        }
    }
}
```

### Adaptive Concurrency [FIX: M4, #15]

Before spawning each agent, the orchestrator checks available system resources:

```go
func (o *Orchestrator) effectiveConcurrency() int {
    if !o.config.Concurrency.Adaptive {
        return o.config.Concurrency.Development
    }
    availableRAM := getAvailableRAMMB() // runtime.MemStats + OS-specific checks
    maxByRAM := availableRAM / o.config.Concurrency.AdaptiveMinRAMPerAgentMB
    if maxByRAM < 1 { maxByRAM = 1 }
    configured := o.config.Concurrency.Development
    if maxByRAM < configured {
        log.Warnf("Reducing concurrency from %d to %d due to available RAM (%d MB)",
            configured, maxByRAM, availableRAM)
        return maxByRAM
    }
    return configured
}
```

### Graceful Shutdown

On SIGINT/SIGTERM to the blueflame binary:

```go
func (o *Orchestrator) handleShutdown() {
    // 1. Send SIGTERM to all agent process groups
    for _, agent := range o.lifecycle.RunningAgents() {
        syscall.Kill(-agent.PGID, syscall.SIGTERM)
    }

    // 2. Wait 10 seconds for graceful exit
    time.Sleep(10 * time.Second)

    // 3. Send SIGKILL to survivors via process group
    for _, agent := range o.lifecycle.RunningAgents() {
        syscall.Kill(-agent.PGID, syscall.SIGKILL)
    }

    // 4. Release all locks
    o.lockManager.ReleaseAll()

    // 5. Persist state for crash recovery
    o.stateManager.Save(o.state)

    // 6. Do NOT clean worktrees (preserve for debugging)
    log.Info("Worktrees preserved at %s for debugging", o.config.Project.WorktreeDir)
}
```

### Orphan Prevention and Startup Cleanup

On startup, before any new work:

```go
func (o *Orchestrator) cleanupStaleState() error {
    // 1. Read agents.json from previous session (if exists)
    staleAgents, _ := o.lifecycle.LoadStaleAgents()
    for _, agent := range staleAgents {
        // Kill any surviving processes
        if processAlive(agent.PID) {
            syscall.Kill(-agent.PGID, syscall.SIGKILL)
            log.Warnf("Killed orphan agent %s (PID %d)", agent.ID, agent.PID)
        }
    }

    // 2. Release stale locks (lock holder PID is dead)
    o.lockManager.CleanStale()

    // 3. Clean stale worktrees (configurable: auto-clean or warn)
    staleWorktrees := o.worktreeManager.FindStale()
    if len(staleWorktrees) > 0 {
        log.Warnf("Found %d stale worktrees from previous session", len(staleWorktrees))
        // Optionally clean or prompt human
    }

    // 4. Check for crash recovery state
    if recoveryState, err := o.stateManager.Load(); err == nil {
        log.Infof("Found recovery state from wave cycle %d", recoveryState.WaveCycle)
        // Offer human the choice to resume or start fresh
    }

    return nil
}
```

---

## 10. File and Directory Locking

### Lock Mechanism: flock [FIX: A9, #18]

Advisory file locking using `syscall.Flock()` for atomic acquisition. No TOCTOU race conditions.

```go
type LockManager struct {
    lockDir string
    held    map[string]*os.File // path -> open file handle
    mu      sync.Mutex
}

func (lm *LockManager) Acquire(agentID string, paths []string) error {
    lm.mu.Lock()
    defer lm.mu.Unlock()

    var acquired []*os.File
    for _, path := range paths {
        lockPath := lm.lockFilePath(path)
        os.MkdirAll(filepath.Dir(lockPath), 0755)

        f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
        if err != nil { lm.rollback(acquired); return err }

        // Non-blocking flock -- fails immediately if held
        err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
        if err != nil {
            f.Close()
            lm.rollback(acquired)
            return fmt.Errorf("lock conflict on %s: %w", path, err)
        }

        // Write lock metadata
        f.Truncate(0)
        f.Seek(0, 0)
        fmt.Fprintf(f, "%s %d %s\n", agentID, os.Getpid(), time.Now().Format(time.RFC3339))

        acquired = append(acquired, f)
        lm.held[path] = f
    }
    return nil
}

func (lm *LockManager) Release(agentID string) {
    lm.mu.Lock()
    defer lm.mu.Unlock()
    for path, f := range lm.held {
        syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
        f.Close()
        os.Remove(lm.lockFilePath(path))
        delete(lm.held, path)
    }
}
```

### Lock File Path Convention

Path separators replaced with double underscores to avoid directory creation:
- `pkg/middleware/` -> `.blueflame/locks/pkg__middleware__.lock`
- `internal/auth/handler.go` -> `.blueflame/locks/internal__auth__handler.go.lock`

### Conflict Prevention

- The scheduler checks lock availability before selecting tasks for a batch
- Tasks with conflicting locks run in sequential batches within the same wave
- The planner is instructed to decompose tasks to minimize lock overlap
- Lock conflicts are detected at scheduling time (before spawning), not at spawn time

---

## 11. Beads Integration

Persistent cross-session memory via the `MemoryProvider` interface [FIX: E3].

### MemoryProvider Interface

```go
type MemoryProvider interface {
    // Save persists session results (tasks, outcomes, costs, failure notes)
    Save(session SessionResult) error
    // Load retrieves prior session context for the planner
    Load() (SessionContext, error)
}
```

### Beads Implementation

```go
type BeadsMemory struct {
    config BeadsConfig
}

func (m *BeadsMemory) Save(session SessionResult) error {
    // 1. Create a bead for each completed task
    for _, task := range session.CompletedTasks {
        data := BeadData{
            SchemaVersion: 1,
            TaskID:        task.ID,
            Title:         task.Title,
            Result:        task.Result.Status,
            ValidatorNotes: task.Result.Notes,
            FilesChanged:  task.FilesChanged,
            CostUSD:       task.CostUSD,
        }
        exec.Command("beads", "save", "--type", "task-result",
            "--data", mustJSON(data)).Run()
    }

    // 2. Create a bead for each failed task (with full context for next session)
    for _, task := range session.FailedTasks {
        data := BeadData{
            SchemaVersion: 1,
            TaskID:        task.ID,
            Title:         task.Title,
            FailureReason: task.FailureReason,
            RetryCount:    task.RetryCount,
            History:       task.History,
        }
        exec.Command("beads", "save", "--type", "task-failure",
            "--data", mustJSON(data)).Run()
    }

    // 3. Create a session summary bead
    summary := SessionSummary{
        SessionID:      session.ID,
        TotalTasks:     len(session.AllTasks),
        Completed:      len(session.CompletedTasks),
        Failed:         len(session.FailedTasks),
        TotalCostUSD:   session.TotalCostUSD,
        Duration:       session.Duration,
        WaveCycles:     session.WaveCycles,
    }
    exec.Command("beads", "save", "--type", "session-summary",
        "--data", mustJSON(summary)).Run()

    return nil
}

func (m *BeadsMemory) Load() (SessionContext, error) {
    output, err := exec.Command("beads", "load",
        "--type", "task-failure,session-summary",
        "--format", "json",
        "--limit", "20",
    ).Output()
    if err != nil { return SessionContext{}, nil } // Graceful degradation

    var context SessionContext
    json.Unmarshal(output, &context)
    return context, nil
}
```

### No-op Implementation

When `beads.enabled: false`, the orchestrator uses `NoopMemory`:

```go
type NoopMemory struct{}
func (m *NoopMemory) Save(session SessionResult) error { return nil }
func (m *NoopMemory) Load() (SessionContext, error) { return SessionContext{}, nil }
```

### Memory Decay

Beads' built-in memory decay summarizes old entries. The decay policy from blueflame.yaml controls retention:
- Tasks older than `decay_policy.summarize_after_sessions` sessions are summarized to a single line
- Failure context is preserved longer (`decay_policy.preserve_failures_sessions`)
- This keeps cross-session context at 5-15K tokens regardless of session count

---

## 12. Agent System Prompts

Each role gets a focused, minimal prompt rendered from Go templates. Template variables are type-checked at compile time via the `PromptData` structs.

### Available Template Variables

All templates receive:
- `{{.ProjectName}}` -- from blueflame.yaml
- `{{.BaseBranch}}` -- from blueflame.yaml
- `{{.SessionID}}` -- generated per session

### Planner Prompt -- Non-Interactive (`planner-prompt.md.tmpl`)

Used when `planning.interactive: false`.

```
You are a senior developer breaking down a feature request into isolated, implementable tasks.

## Your Task
{{.TaskDescription}}

## Constraints
- Each task MUST have clear file boundaries (file_locks) for exclusive access
- Each task MUST have explicit dependencies on other tasks (or none)
- Each task MUST be completable in a single focused session
- Each task MUST have a cohesion_group label for merge ordering
- Tasks should NOT have overlapping file_locks where possible
- Maximum {{.MaxWorkers}} tasks can run in parallel

## Output Format
Return a JSON object with a "tasks" array. Each task must have:
- id: "task-NNN" (sequential)
- title: short description
- description: detailed implementation instructions
- priority: integer (1 = highest)
- cohesion_group: logical grouping label
- dependencies: array of task IDs that must complete first
- file_locks: array of file/directory paths this task needs exclusive access to

{{if .PriorContext}}
## Prior Session Context
The following context is from previous sessions. Use it to avoid repeating past mistakes.
<prior-context>
{{.PriorContext}}
</prior-context>
{{end}}

{{if .ReplanNotes}}
## Re-planning Notes
The previous plan was rejected. Here is the feedback:
<rejection-feedback>
{{.ReplanNotes}}
</rejection-feedback>
{{end}}
```

### Planner Prompt -- Interactive (`planner-interactive-prompt.md.tmpl`) [FEEDBACK: interactive planning]

Used when `planning.interactive: true`. Includes brainstorm skill for Q/A with human.

```
You are a senior developer and project architect. Before creating a task plan,
you MUST use the superpowers brainstorm skill to engage the human in a Q/A flow.

## Your Goal
Break down the following request into isolated, implementable tasks:
{{.TaskDescription}}

## Phase 1: Brainstorm (Q/A with the human)
Use /brainstorm to explore:
- Clarify ambiguous requirements
- Understand scope boundaries and priorities
- Identify constraints the human cares about
- Discuss architectural trade-offs
- Confirm file/module boundaries

Do NOT skip this phase. The human is the project lead and has context you lack.

## Phase 2: Plan Output
After the brainstorm, produce your final plan as a JSON object with a "tasks" array.
Each task must have:
- id: "task-NNN" (sequential)
- title: short description
- description: detailed implementation instructions
- priority: integer (1 = highest)
- cohesion_group: logical grouping label
- dependencies: array of task IDs that must complete first
- file_locks: array of file/directory paths this task needs exclusive access to

## Constraints
- Each task MUST have clear file boundaries (file_locks) for exclusive access
- Each task MUST have explicit dependencies on other tasks (or none)
- Each task MUST be completable in a single focused session
- Each task MUST have a cohesion_group label for merge ordering
- Tasks should NOT have overlapping file_locks where possible
- Maximum {{.MaxWorkers}} tasks can run in parallel

{{if .PriorContext}}
## Prior Session Context
<prior-context>
{{.PriorContext}}
</prior-context>
{{end}}

{{if .ReplanNotes}}
## Re-planning Notes
<rejection-feedback>
{{.ReplanNotes}}
</rejection-feedback>
{{end}}
```

### Worker Prompt (`worker-prompt.md.tmpl`)

```
You are a developer implementing a specific task. Follow these rules strictly:

## Your Task
ID: {{.TaskID}}
Title: {{.TaskTitle}}
<task-description>
{{.TaskDescription}}
</task-description>

## Rules
- Work ONLY in your current directory (your isolated worktree)
- Touch ONLY files within these paths: {{range .FileLocks}}{{.}}, {{end}}
- Your watcher will block any out-of-scope changes
- Follow TDD: write tests first, then implementation
- Commit with format: type({{.TaskID}}): description
  Example: feat({{.TaskID}}): add JWT validation middleware
- You have NO network access
- Reference {{.TaskID}} in all commit messages

{{if .History}}
## Prior Attempts
This task has been attempted before. Learn from these results:
{{range .History}}
- Attempt {{.Attempt}} ({{.Timestamp}}): {{.Result}}
  Notes: {{.Notes}}
  {{if .RejectionReason}}Rejection reason: {{.RejectionReason}}{{end}}
{{end}}
{{end}}
```

### Validator Prompt (`validator-prompt.md.tmpl`)

```
You are a QA engineer reviewing a completed task. Evaluate the work objectively.

## Task Being Reviewed
ID: {{.TaskID}}
Title: {{.TaskTitle}}
<task-description>
{{.TaskDescription}}
</task-description>

## The Diff
<diff>
{{.Diff}}
</diff>

## Watcher Audit Summary
{{.AuditSummary}}

{{if .DiagnosticsEnabled}}
## Diagnostic Tooling [FEEDBACK: validator diagnostics]
You MUST run the following diagnostic commands and include their results in your evaluation.
Your Bash access is restricted to these commands only.

Available diagnostic commands:
{{range .DiagnosticCommands}}- {{.}}
{{end}}

Run each applicable command. If a command fails (non-zero exit), report the failure details
in your issues list. A failing test suite or linter error MUST result in a "fail" status.
{{end}}

## Your Evaluation Criteria
1. Diagnostics: Run all applicable diagnostic commands (tests, linters, scanners). Report results.
2. Task applicability: Do the changes solve the stated problem? Are there unrelated changes?
3. Correctness: Does the implementation look correct? Any logic errors?
4. Regressions: Do tests pass? Any broken behavior?
5. Scope creep: Changes stay within what was asked? No over-engineering?

Return your evaluation as JSON with status ("pass" or "fail"), notes (explanation), and issues (array of specific problems found).
```

### Merger Prompt (`merger-prompt.md.tmpl`)

```
You are a release engineer creating clean, cohesive changesets from validated work.

## Validated Branches
{{range .CohesionGroups}}
### Cohesion Group: {{.Name}}
{{range .Branches}}
- Branch: {{.Name}} (Task: {{.TaskID}} - {{.TaskTitle}})
  Files changed: {{.FilesChanged}}
{{end}}
{{end}}

## Instructions
1. For each cohesion group, create ONE clean changeset
2. Merge the branches in dependency order
3. Resolve textual merge conflicts where the resolution is unambiguous
4. If a semantic conflict cannot be safely resolved, STOP and report it
5. Write clear commit messages following: type(task-IDs): description
6. Do NOT guess at conflict resolution -- report uncertain conflicts to the human
```

### Prompt Injection Mitigation [FIX: A3, #17]

All task descriptions are sanitized before injection into prompts:

```go
func SanitizeTaskContent(content string) string {
    // Wrap in XML-like delimiters that the prompt instructs agents to treat as data
    // Strip any existing delimiter patterns to prevent nesting attacks
    content = strings.ReplaceAll(content, "<task-description>", "")
    content = strings.ReplaceAll(content, "</task-description>", "")
    content = strings.ReplaceAll(content, "<prior-context>", "")
    content = strings.ReplaceAll(content, "</prior-context>", "")
    return content
}
```

The prompts use explicit delimiters (`<task-description>`, `<prior-context>`, `<rejection-feedback>`) and instruct agents to treat delimited content as data, not instructions.

---

## 13. Approvals and Human Gates

### Plan Approval (After Wave 1)

```
=== BLUE FLAME: Plan Review ===

Session: ses-20260206-143022
Tasks: 4
Estimated cost: $1.50 - $4.50 (base - with retries)

  task-001 [auth] Add JWT middleware to API router        Priority: 1
    Locks: pkg/middleware/, internal/auth/
    Dependencies: none

  task-002 [auth] Add auth tests                          Priority: 2
    Locks: tests/auth/
    Dependencies: task-001

  task-003 [api] Update API route documentation           Priority: 3
    Locks: docs/api/
    Dependencies: none

  task-004 [api] Add rate limiting middleware              Priority: 3
    Locks: pkg/middleware/ratelimit/
    Dependencies: none

(a)pprove / (e)dit tasks.yaml / (r)e-plan / (q)uit?
```

### Changeset Approval (After Wave 4)

Changesets presented sequentially in dependency order. See Section 7 (Wave 4) for the full interaction flow.

### Session Continuation

See Section 7 (end of wave loop) for the continuation prompt with continue/re-plan/stop options.

### Humble Object Pattern for Testing [FIX: T6]

All human interaction goes through the `Prompter` interface:

```go
type Prompter interface {
    PlanApproval(plan Plan) PlanDecision
    ChangesetReview(cs Changeset) ChangesetDecision
    SessionContinuation(state OrchestratorState) SessionDecision
    ValidatorFailedPrompt(task *Task, err error) ValidatorFailureDecision
}

// Production: reads from terminal
type TerminalPrompter struct { ... }

// Testing: reads from programmatic decisions
type ScriptedPrompter struct {
    PlanDecisions      []PlanDecision
    ChangesetDecisions []ChangesetDecision
    SessionDecisions   []SessionDecision
}

// CLI flag: --decisions-file decisions.yaml
// Enables fully automated end-to-end testing
```

---

## 14. Cost Profile

Realistic estimates with ranges [FIX: B1, #10, #25].

### Per-Session Cost (3 tasks, single wave cycle)

| Agent | Model | Count | Base Cost | With Retries (worst) |
|-------|-------|-------|-----------|---------------------|
| Planner | Sonnet | 1 | $0.20-$0.40 | $0.40-$0.80 (rejected once) |
| Workers | Sonnet | 3 | $0.60-$2.25 | $1.80-$6.75 (all retry 2x) |
| Validators | Haiku | 3 | $0.03-$0.09 | $0.06-$0.18 |
| Merger | Sonnet | 1 | $0.20-$0.50 | $0.40-$1.00 |
| Orchestrator | N/A | 1 | **$0.00** | **$0.00** |
| **Total** | | **9** | **$1.03-$3.24** | **$2.66-$8.73** |

**Summary**: $1-3 typical, $3-9 worst case per 3-task session. The orchestrator costs $0.00 (Go binary, zero tokens).

### Session-Level Budget Circuit Breaker [FIX: B4, #24, FEEDBACK: token budgets, 0 = unlimited]

The circuit breaker supports both USD and token limits. A value of 0 means unlimited (no enforcement).

```go
func (o *Orchestrator) checkBudgetCircuitBreaker() error {
    // USD limit (0 = unlimited) [FEEDBACK: 0 = unlimited]
    if limit := o.config.Limits.MaxSessionCostUSD; limit > 0 {
        if o.sessionCost >= limit {
            return fmt.Errorf("session cost $%.2f exceeds limit $%.2f",
                o.sessionCost, limit)
        }
    }
    // Token limit (0 = unlimited) [FEEDBACK: token-based budgets]
    if limit := o.config.Limits.MaxSessionTokens; limit > 0 {
        if o.sessionTokens >= limit {
            return fmt.Errorf("session tokens %d exceeds limit %d",
                o.sessionTokens, limit)
        }
    }
    return nil
}
```

The circuit breaker is checked:
- Before spawning each agent
- After each agent completes (cost and token counts updated from JSON output)
- At each wave transition

When triggered, the orchestrator pauses and asks the human whether to continue (with increased limit) or stop.

### Per-Agent Token Budget Enforcement [FEEDBACK: token-based budgets]

When a role uses token-based budgets (e.g., `worker_tokens: 50000`), the orchestrator enforces the limit itself since `claude --print` only supports `--max-budget-usd`:

```go
func (o *Orchestrator) monitorAgentTokens(agent *Agent, tokenLimit int) {
    // Periodically check agent's cumulative token usage from JSON output stream.
    // If exceeded, kill the agent via process group SIGTERM -> SIGKILL.
    // The agent's task is marked failed with reason "token_budget_exceeded".
    // 0 = unlimited (this goroutine is not started).
}
```

### Retry Cost Accounting [FIX: #25]

Each retry gets a fresh per-agent budget (via `--max-budget-usd`). The effective per-task ceiling is `budget * (1 + max_retries)`. This is documented and accounted for in the cost estimates above. The session-level circuit breaker prevents unbounded retry cost escalation.

### Cost-Saving Measures

- Validators use haiku (3x cheaper than sonnet)
- Watcher hooks are deterministic shell scripts (zero token cost)
- Post-execution checks are deterministic Go code (zero token cost)
- Orchestration logic is a Go binary (zero token cost)
- Beads memory decay keeps cross-session context lean
- Focused prompts minimize per-agent context size
- `--max-budget-usd` provides hard per-agent cost caps (USD mode)
- Token-based per-agent caps enforced by orchestrator process monitoring (token mode) [FEEDBACK]
- `max_session_cost_usd` / `max_session_tokens` provides hard session-level cap (0 = unlimited) [FEEDBACK]
- Structured output (`--json-schema`) reduces validator output token waste

---

## 15. Testing Strategy

### Test Pyramid

```
                  /\
                 /  \      End-to-End (5 tests)
                /    \     - Full wave cycle with mock spawner
               /------\    - Real git operations, synthetic agents
              /        \
             / Integra- \  Integration (15 tests)
            /   tion     \ - Multi-package interactions
           /--------------\- Real git repos, mock claude
          /                \
         /     Unit (50+)   \ Unit Tests
        /                    \ - Per-function, table-driven
       /______________________\- No external deps, fast
```

### Unit Tests (50+ tests, zero API cost, <30 seconds)

Every `internal/` package gets `_test.go` files. Key test categories:

**Config package**:
- Parse valid blueflame.yaml (all fields)
- Parse minimal blueflame.yaml (defaults applied)
- Reject invalid config (bad globs, invalid regex, missing required fields)
- Schema migration v1 -> v2
- Schema version too new -> clear error message

**Tasks package** (table-driven state machine tests):
```go
func TestTaskStateTransitions(t *testing.T) {
    tests := []struct {
        name      string
        initial   string
        action    string
        expected  string
        expectErr bool
    }{
        {"claim pending task",    "pending",  "claim",    "claimed", false},
        {"claim claimed task",    "claimed",  "claim",    "",        true},
        {"complete claimed task", "claimed",  "complete", "done",    false},
        {"complete pending task", "pending",  "complete", "",        true},
        {"fail claimed task",     "claimed",  "fail",     "failed",  false},
        {"requeue failed task",   "failed",   "requeue",  "pending", false},
        {"block pending task",    "pending",  "block",    "blocked", false},
    }
    // ...
}
```

**Dependency package**:
- Linear dependency chain: A -> B -> C
- Diamond dependency: A -> B, A -> C, B -> D, C -> D
- Circular dependency detection: A -> B -> A
- Cascading failure: A fails -> B, C blocked
- Ready tasks with no unmet dependencies

**Scheduler package**:
- Priority ordering
- Lock conflict detection and deferral
- Mixed: high-priority task has lock conflict, lower-priority task does not

**Locks package**:
- Acquire and release
- Conflict detection (non-blocking flock fails)
- Stale lock cleanup
- Concurrent acquisition (goroutine stress test)

**Hooks package** (snapshot tests):
- Generate watcher script from sample config, compare to golden file
- Verify all config fields appear in generated script
- Verify audit log path is correct

**Postcheck package**:
- Allowed changes only -> pass
- Blocked path modified -> fail with violation
- Out-of-scope file modified -> fail with violation
- No changes at all -> pass (empty diff)
- Binary file added -> flagged

**Sanitize package**:
- Normal task description -> unchanged
- Description with delimiter injection -> stripped
- Description with prompt injection patterns -> wrapped safely

**State package**:
- Serialize/deserialize round-trip
- Recovery from corrupted state file (graceful failure)
- Recovery from missing state file (clean start)

### Integration Tests (15 tests, zero API cost, <2 minutes)

These test multi-package interactions with real git repos but mock agent spawner:

```go
type MockSpawner struct {
    responses map[string]AgentResult // taskID -> predetermined result
    delay     time.Duration
}

func (m *MockSpawner) SpawnWorker(ctx context.Context, task *Task, cfg *Config) (*Agent, error) {
    // Create a mock process that sleeps for delay then exits
    // Write predetermined output to stdout buffer
    // Return an Agent that cmd.Wait() will complete on
}
```

Key integration tests:
1. **Single-task wave cycle**: plan -> work -> validate -> merge (all mock agents succeed)
2. **Multi-task with dependencies**: task-002 deferred until task-001 completes
3. **Lock conflict deferral**: two tasks with overlapping locks run sequentially
4. **Worker failure and retry**: mock worker fails, verify retry up to max_retries
5. **Cascading dependency failure**: task fails, dependents marked blocked
6. **Validator failure with escalation**: mock validator crashes, verify retry then human prompt
7. **Changeset rejection and re-queue**: mock approval rejects changeset, verify task re-queued with history
8. **Session cost circuit breaker**: set low limit, verify session stops
9. **Max wave cycles**: set max_wave_cycles=2, verify forced stop
10. **Worktree creation and cleanup**: verify worktrees created, branches named correctly, cleaned after
11. **Postcheck failure**: mock agent modifies blocked path, verify task marked failed
12. **Adaptive concurrency**: mock low RAM, verify reduced concurrency
13. **Graceful shutdown**: send SIGTERM during wave, verify cleanup
14. **Orphan cleanup on startup**: leave stale agents.json, verify cleanup
15. **Atomic tasks.yaml writes**: verify write-to-temp-then-rename pattern

### End-to-End Tests (5 tests, minimal API cost)

These use real `claude --print` invocations against a small test repository:

1. **Smoke test**: Single simple task ("add a hello world function and test"), verify full wave cycle produces a valid changeset. Budget: <$1.00.
2. **Watcher enforcement**: Worker attempts blocked operation (write to .env), verify watcher blocks it and audit log records it.
3. **Hook integration**: Verify Claude Code correctly invokes PreToolUse hooks, respects block decisions, and handles hook timeouts.
4. **Structured output**: Verify validator returns parseable JSON matching the schema.
5. **Multi-worker**: Two non-conflicting tasks run in parallel, both succeed, merger produces two changesets.

### CI Strategy

```makefile
# Makefile targets
test-unit:        go test -short -race ./internal/...
test-integration: go test -run Integration -race ./internal/...
test-e2e:         go test -run E2E -count=1 ./test/e2e/...  # requires CLAUDE_API_KEY
test-all:         test-unit test-integration
lint:             golangci-lint run ./...
build:            go build -o bin/blueflame ./cmd/blueflame/
```

CI runs `test-unit` and `lint` on every push. `test-integration` runs on every PR. `test-e2e` runs manually or on release tags (requires API key as CI secret).

---

## 16. Implementation Phases

### Phase 1: Core Foundation (config, tasks, agent spawning)

**Files**: `cmd/blueflame/main.go`, `internal/config/`, `internal/tasks/`, `internal/agent/spawner.go`, `internal/sanitize/`

**Deliverables**:
- CLI entrypoint with flag parsing (`--config`, `--task`, `--dry-run`, `--decisions-file`)
- `Config` struct with full YAML parsing, validation, defaults, and schema migration
- `TaskStore` with YAML read/write, atomic persistence, state transitions
- `AgentSpawner` interface and production implementation (spawns real `claude --print`)
- `MockSpawner` for testing
- Task description sanitization
- Unit tests for all of the above

**Milestone**: Can parse blueflame.yaml, parse tasks.yaml, and spawn a single claude agent that produces output. All unit tests pass.

**Dependencies**: None

### Phase 2: Worktrees, Locks, and Scheduling

**Files**: `internal/worktree/`, `internal/locks/`, `internal/tasks/dependency.go`, `internal/orchestrator/scheduler.go`

**Deliverables**:
- Git worktree create/remove/list with branch naming convention (`blueflame/<task-id>`)
- flock-based advisory locking with conflict detection and rollback
- Dependency graph resolution with circular dependency detection
- Cascading failure propagation
- Task scheduler with priority ordering and lock-conflict deferral
- Unit and integration tests

**Milestone**: Can create worktrees, acquire/release locks, resolve dependencies, and schedule tasks with conflict avoidance.

**Dependencies**: Phase 1

### Phase 3: Watcher System (Three-Phase Enforcement)

**Files**: `internal/agent/hooks.go`, `internal/agent/postcheck.go`, `internal/agent/sandbox_linux.go`, `internal/agent/sandbox_darwin.go`, `templates/watcher.sh.tmpl`, `templates/sandbox_darwin.sb.tmpl`

**Deliverables**:
- Watcher hook script generation from config + task file_locks
- Per-agent `.claude/settings.json` generation with hook registration
- Post-execution filesystem diff validation
- Platform-specific sandbox: cgroups v2 on Linux, best-effort on macOS
- Network isolation: `CLONE_NEWNET` on Linux, `sandbox-exec` on macOS (with fallback warning)
- Audit logging to JSONL with defined schema
- Unit tests (synthetic hook inputs, synthetic worktree states)

**Milestone**: A manually spawned claude agent is constrained by generated watchers. Post-execution checks catch violations. Sandbox limits apply at fork time.

**Dependencies**: Phase 1 (config), Phase 2 (worktrees)

### Phase 4: Wave Orchestration (the state machine)

**Files**: `internal/orchestrator/`, `internal/ui/`, `templates/*-prompt.md.tmpl`

**Deliverables**:
- Wave state machine: plan -> develop -> validate -> merge -> repeat
- Planner: spawn, parse structured output, present plan for approval
- Workers: concurrent spawning via goroutines, event-driven completion collection
- Validators: spawn with `--json-schema`, parse structured pass/fail
- Merger: spawn, present changesets for human review
- Human approval gates: plan approval, changeset chaining, session continuation
- Re-queue logic with history preservation
- Changeset dependency ordering
- Session cost tracking and circuit breaker
- Max wave cycle enforcement
- All prompt templates for all four roles
- `Prompter` interface with terminal and scripted implementations
- Integration tests with mock spawner for full wave cycle

**Milestone**: Full wave cycle works end-to-end with mock agents. Human can approve plans, review changesets, re-queue rejected tasks, and chain sessions. Cost tracking and circuit breaker functional.

**Dependencies**: Phases 1, 2, 3

### Phase 5: Lifecycle Hardening and Beads Integration

**Files**: `internal/agent/lifecycle.go`, `internal/memory/`, `internal/state/`

**Deliverables**:
- Process group lifecycle management with heartbeat goroutines
- Timeout enforcement via process group SIGTERM/SIGKILL
- Orphan detection and cleanup on startup
- Adaptive concurrency based on available RAM
- Crash recovery state persistence and restoration
- `MemoryProvider` interface with Beads and No-op implementations
- Beads archive (save session results) and load (prior context for planner)
- Memory decay configuration passthrough
- Stall detection (audit log last-modified monitoring)
- Stale worktree and lock cleanup

**Milestone**: System recovers gracefully from crashes. Planner receives context from prior sessions. Runaway agents are killed. Orphans are cleaned. Adaptive concurrency adjusts to hardware.

**Dependencies**: Phase 4

### Phase 6: Polish and Production Readiness

**Deliverables**:
- Progress display during waves (running agents, elapsed time, cost so far)
- Cost summary after each session (actual vs. estimated, per-agent breakdown)
- Cohesion group display in changeset review
- Dry-run mode: show what would happen without spawning agents
- `blueflame cleanup` command: remove stale worktrees, old logs, orphaned hooks
- Log retention policy (configurable max age for audit logs)
- Disk space check before worktree creation
- Detailed error messages for all failure modes
- Custom lifecycle hooks support (`hooks` section in blueflame.yaml)
- End-to-end tests with real claude agents

**Milestone**: Production-ready orchestrator. Pleasant human experience. Clear diagnostics.

**Dependencies**: Phase 5

---

## 17. Verification Plan

Comprehensive test scenarios covering all identified edge cases from the reviews.

### Core Flow Tests

| # | Test | Covers | Phase |
|---|------|--------|-------|
| 1 | Parse valid blueflame.yaml with all fields | Config completeness | 1 |
| 2 | Reject blueflame.yaml with invalid regex patterns | Config validation | 1 |
| 3 | Migrate v1 config to v2 | Schema versioning [E2] | 1 |
| 4 | Task state transitions (table-driven, all valid and invalid paths) | Task state machine | 1 |
| 5 | Atomic tasks.yaml write (write-to-temp-then-rename) | Corruption prevention [W5] | 1 |
| 6 | Create/remove git worktree, verify branch naming | Worktree isolation | 2 |
| 7 | Acquire lock, attempt conflicting acquisition -> fail | Lock atomicity [A9] | 2 |
| 8 | Detect and clean stale locks (holder PID is dead) | Stale lock cleanup | 2 |
| 9 | Circular dependency detection | Dependency safety | 2 |
| 10 | Cascading failure: A fails -> B, C marked blocked | Dependency cascading [W1, #8] | 2 |

### Watcher Tests

| # | Test | Covers | Phase |
|---|------|--------|-------|
| 11 | Generated watcher blocks write to blocked path | Phase 1 enforcement | 3 |
| 12 | Generated watcher allows write to allowed path within file_locks | Phase 1 allow | 3 |
| 13 | Generated watcher blocks bash command matching blocked pattern | Phase 1 bash filtering | 3 |
| 14 | Generated watcher checks commit message format | Phase 1 commit format | 3 |
| 15 | Postcheck: only allowed files modified -> pass | Phase 3 postcheck | 3 |
| 16 | Postcheck: blocked path modified -> fail with details | Phase 3 postcheck | 3 |
| 17 | Postcheck: file outside file_locks modified -> fail | Phase 3 scope enforcement | 3 |
| 18 | Sandbox applies correct limits per platform (build tag conditional) | Phase 2 sandbox [M2] | 3 |

### Orchestration Tests

| # | Test | Covers | Phase |
|---|------|--------|-------|
| 19 | Full wave cycle with mock agents (all succeed) | End-to-end flow | 4 |
| 20 | Worker failure -> retry up to max_retries -> permanent failure | Retry logic | 4 |
| 21 | Validator failure -> retry once -> escalate to human | Validator recovery [W2, #9] | 4 |
| 22 | Changeset rejected -> task re-queued with history and rejection notes | Re-queue [wave #26, #27] | 4 |
| 23 | Lock conflict between tasks -> sequential execution in same wave | Lock scheduling | 4 |
| 24 | Task with dependencies -> deferred until dependency completes | Dependency ordering | 4 |
| 25 | Two cohesion groups, group B depends on group A -> present A first | Merge ordering [W3] | 4 |
| 26 | Group A rejected -> group B auto-deferred | Cascading rejection | 4 |
| 27 | Session cost exceeds limit -> pause and prompt human | Cost circuit breaker [#24] | 4 |
| 28 | Wave cycle count exceeds max_wave_cycles -> forced stop | Cycle limit [W4, #19] | 4 |
| 29 | All tasks fail in wave 2 -> wave 3 and 4 gracefully skip | Empty wave handling | 4 |
| 30 | Plan rejected 3 times -> suggest manual plan writing | Plan loop escape | 4 |
| 31 | Session continuation: choose re-plan -> return to wave 1 | Re-plan option [wave #31] | 4 |

### Lifecycle and Recovery Tests

| # | Test | Covers | Phase |
|---|------|--------|-------|
| 32 | Kill worker mid-execution -> detected, locks released, task failed | Crash detection | 5 |
| 33 | Kill orchestrator -> state persisted, restart recovers | Crash recovery | 5 |
| 34 | Startup finds stale agents.json -> orphan processes killed | Orphan cleanup | 5 |
| 35 | Startup finds stale worktrees -> warning displayed | Stale worktree detection | 5 |
| 36 | Low available RAM -> concurrency automatically reduced | Adaptive concurrency [M4] | 5 |
| 37 | Agent timeout -> SIGTERM to process group, then SIGKILL | Timeout enforcement | 5 |
| 38 | Beads save/load round-trip with synthetic session data | Beads integration | 5 |
| 39 | Beads disabled -> NoopMemory used, no errors | Graceful degradation | 5 |
| 40 | Planner receives prior session context from Beads | Cross-session memory | 5 |

### Edge Case Tests

| # | Test | Covers | Phase |
|---|------|--------|-------|
| 41 | Empty tasks.yaml (planner produces no tasks) -> graceful handling | Empty plan | 4 |
| 42 | Malformed tasks.yaml -> clear error message | YAML parsing | 1 |
| 43 | Agent produces no commits -> postcheck handles empty diff | No-commit worker | 3 |
| 44 | All workers produce partial work (commits some, not all) | Partial completion | 4 |
| 45 | Concurrent lock acquisition stress test (10 goroutines) | Lock concurrency | 2 |
| 46 | blueflame.yaml changes between wave cycles -> detected and warned | Config drift | 4 |
| 47 | Task history grows across multiple re-queues -> bounded | History size | 4 |
| 48 | Idempotent initialization (run init twice -> no error) | Idempotency | 5 |
| 49 | Token-based session budget triggers circuit breaker | Token budget [FEEDBACK] | 4 |
| 50 | Token-based per-agent budget kills agent on exceed | Token budget [FEEDBACK] | 4 |
| 51 | Budget set to 0 -> no circuit breaker enforcement (unlimited) | 0 = unlimited [FEEDBACK] | 4 |
| 52 | Both _usd and _tokens non-zero for same role -> config validation error | Budget validation [FEEDBACK] | 1 |
| 53 | Interactive planner: brainstorm Q/A produces valid task JSON | Interactive planning [FEEDBACK] | 4 |
| 54 | Interactive planner + --decisions-file -> falls back to non-interactive with warning | Interactive fallback [FEEDBACK] | 4 |
| 55 | planning.interactive: false -> planner uses --print (original behavior) | Non-interactive planning [FEEDBACK] | 4 |
| 56 | Validator runs configured diagnostic commands (tests, linters) | Validator diagnostics [FEEDBACK] | 3 |
| 57 | Validator diagnostic command fails -> validator reports "fail" with details | Validator diagnostics [FEEDBACK] | 3 |
| 58 | Validator Bash restricted to diagnostic commands only (other commands blocked) | Validator Bash scope [FEEDBACK] | 3 |

---

## 18. Platform Considerations

### Linux

| Feature | Mechanism | Status |
|---------|-----------|--------|
| Memory limiting | cgroups v2 (limits RSS, not virtual) | Full support |
| CPU time limiting | `ulimit -t` / `RLIMIT_CPU` | Full support |
| Network isolation | `CLONE_NEWNET` (via `SysProcAttr.Cloneflags`) | Full support |
| File size limiting | `RLIMIT_FSIZE` | Full support |
| Open files limiting | `RLIMIT_NOFILE` | Full support |
| Process groups | `Setpgid: true` | Full support |

### macOS (Darwin)

| Feature | Mechanism | Status |
|---------|-----------|--------|
| Memory limiting | **None reliable** | Best-effort only [FIX: #1] |
| CPU time limiting | `RLIMIT_CPU` | Works |
| Network isolation | `sandbox-exec` (deprecated but functional on macOS 15) | Works with warning [FIX: #2] |
| File size limiting | `RLIMIT_FSIZE` | Works |
| Open files limiting | `RLIMIT_NOFILE` | Works |
| Process groups | `Setpgid: true` | Works |

### macOS-Specific Limitations

1. **`ulimit -v` (virtual memory) MUST NOT be used.** Node.js (which `claude` CLI is built on) maps >1GB of virtual address space even at rest. Setting `ulimit -v 512MB` crashes the process immediately. The Go binary detects macOS and skips virtual memory limits entirely. [FIX: Critical #1]

2. **`sandbox-exec` is deprecated** since macOS 10.15 but still functional through macOS 15 Sequoia. The Go binary checks for its availability at runtime. If unavailable, network isolation falls back to watcher hooks only (which block network tool calls but not raw `curl` from bash). A warning is logged. [FIX: High, #2]

3. **No RSS limiting.** macOS has no cgroups equivalent. The `max_memory_mb` config field is enforced only on Linux. On macOS, agent timeout and cost budget serve as the primary resource controls. This is documented in the startup log.

### Build Tags

Platform-specific code uses Go build tags:

```go
// sandbox_linux.go
//go:build linux

// sandbox_darwin.go
//go:build darwin
```

Both files implement the same `applySandboxLimits(cmd *exec.Cmd, cfg SandboxConfig)` function with platform-appropriate behavior. The compiler selects the correct file at build time.

---

## 19. Existing Tools Leveraged

| Tool | Role | Why |
|------|------|-----|
| `claude` CLI (`--print` and interactive modes) | Agent runtime | `--print` mode for workers, validators, merger: `--model`, `--system-prompt`, `--allowed-tools`, `--max-budget-usd`, `--output-format json`, `--json-schema`, `--settings`. Interactive mode for planner when `planning.interactive: true`: brainstorm Q/A with human before structured plan output. [FEEDBACK: interactive planning] |
| Superpowers plugin | Agent skills | Planning (`/brainstorm`, `/write-plan`), TDD, code review, debugging. Reduces token waste by providing structured thinking tools to agents. |
| Git worktrees | Worker isolation | File-level isolation, shared object database, branch-per-task, no full clone overhead. Native git feature. |
| Beads | Persistent cross-session memory | Git-backed, agent-friendly, memory decay for context size management, hash IDs prevent merge collisions. |
| `flock` (syscall) | File locking | Atomic advisory locking. No TOCTOU race conditions. Built into POSIX. |
| cgroups v2 (Linux) | RSS memory limiting | Limits actual memory usage (RSS), not virtual address space. Does not crash Node.js. |
| `sandbox-exec` (macOS) | Network isolation | Deprecated but functional. Best available option on macOS for process-level network blocking. |
| `unshare --net` / `CLONE_NEWNET` (Linux) | Network isolation | Creates isolated network namespace. Zero overhead. Process and all children have no network. |
| YAML | Configuration and task files | Human-readable, human-editable, no external dependencies. |
| Go `text/template` | Prompt and hook generation | Type-safe template rendering for agent prompts and watcher scripts. |

---

## 20. What This Plan Intentionally Omits

| Omission | Rationale |
|----------|-----------|
| **Claude Code Skill as orchestrator** | Reviews demonstrated that an LLM orchestrator is non-deterministic, untestable, has context window limits, and costs tokens for coordination. Go binary resolves all of these. |
| **Shell scripts for mechanical operations** | All 9 shell scripts from 04-Plan-ADR-hybrid.md are absorbed into Go packages. Only watcher hook templates remain as shell (required by Claude Code's PreToolUse hook system). |
| **Python agent runtime** | Agents run via `claude` CLI. No second language dependency needed. |
| **Autonomous agent spawning** | Workers cannot spawn sub-agents (`Task` tool is blocked). Only the orchestrator dispatches agents. |
| **Network access for agents** | Blocked at three layers: watcher hooks (tool-level), OS sandbox (process-level), and blocked bash patterns (command-level). |
| **Persistent agents between waves** | Agents are ephemeral. They exist only for their task. No state leaks between waves. Session memory is handled by Beads. |
| **Rollback mechanism** | Once a changeset is merged to the base branch, there is no blueflame-level undo. The human can use `git revert` manually. This is a deliberate simplicity choice. |
| **Cross-task communication** | Workers are fully isolated. Task decomposition by the planner eliminates the need for inter-agent communication. |
| **Multi-repo support** | The system assumes a single git repository. Monorepo sub-project scoping is not implemented. |
| **Data-driven wave definitions** | While the extensibility review suggested making waves configurable, the fixed four-wave model (plan, develop, validate, merge) is retained for v1 simplicity. The Go code is structured so that adding new wave phases is a code change in one package, not a system-wide refactor. [FIX: #16, pragmatic compromise] |
| **Batch API for validators** | Validators use `claude --print` (synchronous). The Batch API offers 50% discount but requires direct API calls, not the CLI. This is a future optimization. |
| **Dynamic model selection per task** | All workers use the same model (configured in blueflame.yaml). Per-task model selection based on planner-annotated complexity is a future enhancement. |

---

## Appendix: Issue Resolution Cross-Reference

Every critical and high issue from the eight reviews, with its resolution in this plan:

### Critical Issues

| Issue | Source | Resolution |
|-------|--------|------------|
| ulimit -v 512MB crashes Node.js | Machine Power #4 | Platform-specific sandbox files: cgroups v2 on Linux, skip ulimit -v on macOS (Section 8, 18) |
| macOS sandbox-exec deprecated | Goals #C4, Machine Power #13 | Runtime detection, use if available, warn if not, document limitations (Section 8, 18) |
| Task tool concurrency undefined | Cohesion #C2, #C12 | Replaced by goroutines + channels + WaitGroups (Section 7, Wave 2) |
| ulimit can't propagate through Task tool | Cohesion #C3 | Resolved: SysProcAttr at fork time, Go is the parent process (Section 8) |
| Worker spawned before task claimed | Cohesion #C6 | Fixed ordering: claim first, spawn second, rollback on failure (Section 7, Wave 2 step 2) |
| SKILL.md untestable | Testability #T4 | Resolved: orchestrator is Go code with full unit tests (Section 15) |
| Worker completion detection undefined | Cohesion #C4 | Resolved: cmd.Wait() in goroutines with channel signaling (Section 7, Wave 2 step 3) |

### High Issues

| Issue | Source | Resolution |
|-------|--------|------------|
| Cascading dependency failure | Wave Handling #W1 | Explicit graph traversal marking transitive dependents as blocked (Section 7, Wave 2 step 6) |
| Validator crash/timeout recovery | Wave Handling #W2 | Retry once, then escalate to human with three options (Section 7, Wave 3 step 2) |
| Cost estimate understated | Budget #B1 | Realistic ranges: $1-3 typical, $3-9 worst case. Session circuit breaker. (Section 14) |
| No schema versioning | Extensibility #E2 | schema_version field in both files, migration support in config package (Section 4, 5) |
| Orchestrator context window growth | Architecture #A5 | Resolved: Go binary has no context window (Section 2) |
| Orchestrator as God Object | Architecture #A1 | Decomposed into well-separated Go packages with clear interfaces (Section 3) |
| Multiple unsynchronized state files | Architecture #A8 | In-memory state as authority, atomic persistence via write-to-temp-then-rename (Section 7) |
| 4 concurrent agents need 2-3GB RAM | Machine Power #M1 | Adaptive concurrency based on available RAM (Section 9) |
| Adding new agent roles requires 5+ changes | Extensibility #E1 | Partially addressed: Go interfaces make new roles easier; full data-driven waves deferred to v2 (Section 20) |
| No unit testing for orchestration logic | Testability #T1, #T2 | Full Go test suite with interfaces, mocks, table-driven tests (Section 15) |
| Orchestrator hidden token overhead | Budget #B3 | Resolved: Go binary makes zero API calls for orchestration (Section 14) |
| Inter-group merge ordering | Wave Handling #W3 | Changesets presented in dependency order; rejected group cascades to dependents (Section 7, Wave 4) |
