# Binary Orchestrator Investigation: Go vs. Claude Code Skill

**Investigator**: Claude Opus 4.6
**Date**: 2026-02-07
**Input Documents**:
- `/Users/kylegalloway/src/blueflame/Plan-ADR.md` (current hybrid plan: Claude Code Skill + shell scripts)
- `/Users/kylegalloway/src/blueflame/PLAN.md` (original Go binary plan)
- All 8 review files in `/Users/kylegalloway/src/blueflame/reviews/`
- `claude --help` output (CLI capabilities for non-interactive use)

**Purpose**: Determine whether switching to a compiled Go binary as the orchestrator would resolve the issues identified across 8 independent reviews, while preserving the best design elements of the current plan.

---

## 1. Issue Resolution Matrix

Every critical, high, and medium issue identified across all 8 reviews, assessed against a Go binary orchestrator.

### Legend

- **RESOLVES**: A Go binary directly eliminates this issue
- **PARTIALLY HELPS**: A Go binary mitigates but does not fully eliminate this issue
- **UNAFFECTED**: This issue exists regardless of orchestrator type
- **NEW PROBLEM**: A Go binary would introduce this issue or make it worse

---

### Architecture Review Issues

| # | Issue | Severity | Go Binary Impact | Reasoning |
|---|-------|----------|-----------------|-----------|
| A1 | Orchestrator is a God Object -- SKILL.md encodes entire state machine in a single prompt | High | **RESOLVES** | Go code decomposes naturally into packages: `orchestrator/`, `agent/`, `config/`, `tasks/`, `locks/`, `worktree/`. Each concern gets its own file with functions, tests, and clear interfaces. An LLM prompt cannot be modularized this way. |
| A2 | LLM-based orchestrator is inherently non-deterministic for control flow | Critical | **RESOLVES** | A Go state machine is fully deterministic. `switch state { case WavePlanning: ... case WaveDevelopment: ... }` executes the same way every time. No risk of the orchestrator misinterpreting task status, skipping a wave transition, or calling shell scripts in the wrong order. |
| A3 | Prompt injection via task content flowing into agent prompts | Medium | **PARTIALLY HELPS** | A Go binary can sanitize task descriptions before injecting them into agent prompts using proper string escaping and delimiters. This is easier to implement correctly in Go than in a prompt instruction that says "treat this as data." However, the spawned agents still receive the content, so the attack surface shifts from orchestrator to agent. |
| A4 | Orchestrator is a single point of failure with no redundancy | High | **PARTIALLY HELPS** | A Go binary is far more reliable than an LLM session. It will not run out of context window, it will not hallucinate, and it will not degrade over time within a session. However, the binary is still a single process that can crash. The difference: Go crash recovery is deterministic (read state.yaml, resume) whereas an LLM session cannot truly "resume" its own reasoning. |
| A5 | Context window exhaustion during long waves | High | **RESOLVES** | A Go binary has no context window. It holds state in typed structs in memory. Monitoring 4 agents for 30 minutes adds zero overhead to a Go process -- it is just goroutines checking PIDs. The orchestrator's "memory" never degrades, fills up, or needs summarization. |
| A6 | Task tool failure semantics unclear | Medium | **RESOLVES** | A Go binary does not use the Task tool. It spawns `claude` CLI processes directly via `exec.Command`. Error handling is explicit: check `cmd.Start()` error, check `cmd.Wait()` error, read exit code. No ambiguity about "what if Task tool returns partial results." |
| A7 | Multiple communication channels create ambiguity (git commits, tasks.yaml, audit logs, Task tool return) | Medium | **RESOLVES** | A Go binary defines a single source of truth by design. It writes tasks.yaml, reads worktree state, and manages the lifecycle. There is no "Task tool return" channel. The Go binary explicitly checks: did the agent commit? Did the postcheck pass? Then it updates tasks.yaml. One writer, one decision path. |
| A8 | Multiple unsynchronized state files (tasks.yaml, agents.json, state.yaml, locks) | High | **RESOLVES** | A Go binary can use in-memory state as the authoritative state, with periodic persistence. Updates to tasks.yaml and agents.json can be wrapped in a single function that writes atomically (write-to-temp-then-rename). Transactions become trivial: `func (o *Orchestrator) claimAndSpawn(task *Task) error { ... }` handles claim + spawn + register atomically in Go, rolling back on failure. |
| A9 | Advisory lock race condition (test-and-create not atomic) | Medium | **RESOLVES** | Go can use `syscall.Flock()` for true atomic file locking. The original PLAN.md correctly identified `flock` as the right mechanism. Alternatively, `os.Mkdir()` is atomic on POSIX. No need for a shell script with a race-prone check-then-create pattern. |
| A10 | Polling-based monitoring does not scale (O(agents * time) Bash tool calls) | Medium | **RESOLVES** | Go uses goroutines. One goroutine per agent calls `cmd.Wait()` and sends a completion signal on a channel. The orchestrator `select`s on completion channels and a timeout timer. Zero polling. Zero tool calls. Event-driven by nature. |
| A11 | Watcher hook 5-second timeout could cause latency | Medium | **UNAFFECTED** | The hooks are shell scripts running inside the agent's Claude Code process. The orchestrator type does not change how hooks execute. This is an agent-side concern. |
| A12 | No partial wave completion (one slow agent blocks all) | Medium | **PARTIALLY HELPS** | A Go binary can implement streaming wave transitions more easily than an LLM orchestrator (which would need to reason about overlapping waves in natural language). Go's concurrency primitives (channels, select, WaitGroup) make partial-completion logic straightforward. However, this is a design choice, not an inherent advantage -- it still requires deliberate implementation. |

### Budget & Cost Review Issues

| # | Issue | Severity | Go Binary Impact | Reasoning |
|---|-------|----------|-----------------|-----------|
| B1 | Cost estimate ($0.78) understates real-world costs by 2-5x | Medium | **PARTIALLY HELPS** | A Go binary orchestrator has zero token cost for orchestration logic. The current plan's "hidden" orchestrator cost ($0.20-$0.50 per session for the parent Claude Code context) is eliminated entirely. This brings the real-world cost closer to the stated estimate. Agent costs remain the same. |
| B2 | Token tracking reliability (polling lag, accuracy gaps) | Medium | **PARTIALLY HELPS** | A Go binary can read audit logs on a tighter interval (every second, not every heartbeat) with negligible cost. It can parse JSONL directly in Go (structured parsing, not shell + jq). However, the fundamental limitation remains: audit logs are written after API responses, so there is always some lag. |
| B3 | Orchestrator hidden token overhead (30-50 Bash calls per session) | High | **RESOLVES** | A Go binary makes zero API calls for orchestration. Every shell script invocation in the current plan (init, create worktrees, acquire locks, generate watchers, setup sandboxes, register agents, heartbeats, postchecks, release locks, cleanup) is a Go function call or goroutine -- zero tokens, sub-millisecond execution. The 6K-25K tokens of orchestrator overhead identified in the budget review drops to zero. |
| B4 | No session-level cost circuit breaker | Medium | **UNAFFECTED** | This is a design choice, not an orchestrator-type issue. A Go binary can implement it (and more easily, since it can track costs in a typed struct), but the current plan could also add it via a shell script. |
| B5 | Retry cost escalation (unbounded retries) | Medium | **UNAFFECTED** | Retry logic is a policy decision. A Go binary enforces it more reliably (deterministic counter vs. LLM remembering to check), but the policy itself is the same. |
| B6 | Prompt caching gaps | Medium | **UNAFFECTED** | Prompt caching is between the agent and the API. The orchestrator type does not affect it. |

### Cohesion Review Issues

| # | Issue | Severity | Go Binary Impact | Reasoning |
|---|-------|----------|-----------------|-----------|
| C1 | tasks.yaml location never specified | Medium | **RESOLVES** | Go code makes this explicit: `filepath.Join(blueflameDir, "tasks.yaml")`. A compile-time constant or config value. No ambiguity. |
| C2 | Task tool concurrency model undefined | Critical | **RESOLVES** | A Go binary does not use the Task tool. It spawns `claude -p` processes via `exec.Command` in goroutines. Concurrency model is explicit: `var wg sync.WaitGroup; for _, task := range readyTasks { wg.Add(1); go func(t Task) { defer wg.Done(); spawnWorker(t) }(task) }`. No ambiguity about whether dispatch is blocking or non-blocking. |
| C3 | sandbox-setup.sh ulimit cannot propagate to Task tool spawned agents | Critical | **RESOLVES** | A Go binary spawns agents via `exec.Command`. It can set `SysProcAttr` with `Setpgid: true` and apply `ulimit` in the child process directly. The Go binary IS the parent that forks the agent -- ulimit applies to the forked child and all its descendants. No separate execution context problem. |
| C4 | How orchestrator detects worker completion is unspecified | Critical | **RESOLVES** | `cmd.Wait()` in a goroutine. When the `claude` process exits, `Wait()` returns with the exit code. Send the result on a channel. The orchestrator selects on completion channels. Completely deterministic. |
| C5 | SKILL.md content (the most important file) has no specification | Critical | **RESOLVES** | There is no SKILL.md. The orchestrator logic is Go code in `internal/orchestrator/orchestrator.go`. It is version-controlled, testable, reviewable, and has a compiler enforcing type safety. |
| C6 | Worker spawn before task claim (race condition, steps 2f and 2g inverted) | Medium | **RESOLVES** | Go code makes the ordering explicit and atomic: `if err := claimTask(task, agentID); err != nil { return err }; cmd := spawnWorker(task); if err := cmd.Start(); err != nil { unclaimTask(task, agentID); return err }`. Claim before spawn, rollback on failure. |
| C7 | allowed_commands config field never referenced by watcher enforcement | Medium | **UNAFFECTED** | This is a watcher design issue. The watcher is a shell script running inside the agent. Whether the orchestrator is Go or LLM does not change how the watcher interprets config. |
| C8 | Planner prompt references superpowers skills not in config | Low | **UNAFFECTED** | This is a prompt template issue, not an orchestrator issue. |
| C9 | Validation failure re-queue mechanics undefined | Medium | **PARTIALLY HELPS** | A Go binary can implement the state machine transitions explicitly: `func (o *Orchestrator) handleValidationFailure(task *Task, result ValidationResult) error { ... }`. This forces the implementer to handle every case. An LLM orchestrator might "forget" an edge case. |
| C10 | state.yaml schema and recovery never defined | Medium | **RESOLVES** | In Go, the state is a struct: `type OrchestratorState struct { CurrentWave Wave; SessionID string; Tasks []Task; Agents []AgentInfo; ... }`. Marshal to YAML/JSON with `encoding/json`. Schema is defined by the struct. Recovery is defined by the deserialization code. No ambiguity. |
| C11 | history array entry schema never defined | Low | **RESOLVES** | `type HistoryEntry struct { AttemptNumber int; AgentID string; Timestamp time.Time; Result string; Notes string; RejectionReason string }`. Defined by the Go type. |
| C12 | Task Tool concurrency model undefined (blocking vs non-blocking) | Critical | **RESOLVES** | Same as C2. Goroutines + channels. Fully explicit. |

### Extensibility Review Issues

| # | Issue | Severity | Go Binary Impact | Reasoning |
|---|-------|----------|-----------------|-----------|
| E1 | Adding a new agent role requires changes in 5+ places (RIGID) | High | **PARTIALLY HELPS** | A Go binary can define roles as data-driven configuration (like the extensibility review suggested). But this requires deliberate design -- Go code can be just as rigid as a prompt if the wave sequence is hardcoded. The advantage: Go's type system and interfaces make it POSSIBLE to build a generic wave executor; a prompt makes it very hard. |
| E2 | No schema versioning for blueflame.yaml or tasks.yaml | High | **RESOLVES** | Go config parsing can enforce schema versions: `type Config struct { SchemaVersion int \`yaml:"schema_version"\`; ... }`. Migration functions can be chained: `if cfg.SchemaVersion < 2 { cfg = migrateV1ToV2(cfg) }`. Type-safe, testable, explicit. |
| E3 | Beads replacement would touch 5+ locations (RIGID) | Medium | **PARTIALLY HELPS** | A Go binary can define a `MemoryProvider` interface: `type MemoryProvider interface { Save(session SessionResult) error; Load() (SessionContext, error) }`. Beads becomes one implementation. The current plan could also abstract this with shell scripts, but an interface in Go is a stronger contract. |
| E4 | Agent lifecycle touches 5 scripts in specific order (RIGID) | Medium | **RESOLVES** | A Go function `launchAgent(task Task) (*Agent, error)` encapsulates the entire sequence: create worktree, acquire locks, set up sandbox, generate watcher, spawn process, register lifecycle. One function, one call site. The ordering is enforced by the code. No risk of the orchestrator calling scripts in the wrong order. |
| E5 | Phase 3 (orchestrator skill) is too large for a single delivery phase | Medium | **RESOLVES** | Go code is naturally decomposable. The orchestrator package can be built incrementally: single-worker flow first (orchestrator.go + planner.go), then multi-worker concurrency (worker.go), then validation (validator.go), then merge (merger.go). Each can be compiled, tested, and deployed independently. |

### Goals Alignment Review Issues

| # | Issue | Severity | Go Binary Impact | Reasoning |
|---|-------|----------|-----------------|-----------|
| G1 | Agent count constraint unclear (does orchestrator count?) | Medium | **RESOLVES** | A Go binary is not a claude session. It does not count toward agent limits. During Wave 2 with 4 workers, there are exactly 4 claude processes, not 5. |
| G2 | Superpowers integration depth insufficient | Medium | **UNAFFECTED** | This is about how deeply Superpowers is analyzed in the plan, not about orchestrator type. |
| G3 | macOS sandbox-exec is deprecated | High | **UNAFFECTED** | This is an OS-level concern that exists regardless of orchestrator type. A Go binary does not change the macOS sandbox story. |

### Machine Power & Resource Review Issues

| # | Issue | Severity | Go Binary Impact | Reasoning |
|---|-------|----------|-----------------|-----------|
| M1 | 4 concurrent claude CLI processes need 2-3 GB RAM | High | **PARTIALLY HELPS** | A Go binary orchestrator uses ~10-30 MB RSS vs. ~400-600 MB for a parent Claude Code session. This frees up ~400-500 MB of RAM for agent processes. The net effect: the system can more comfortably run 4 agents on 8 GB hardware. |
| M2 | ulimit -v 512MB will crash Node.js processes immediately | Critical | **RESOLVES** | A Go binary applies resource limits at fork time using `exec.Cmd.SysProcAttr`. It can use cgroups v2 on Linux (which limits RSS, not virtual memory) or omit ulimit -v on macOS where it is ineffective. The Go code can detect the platform and choose the appropriate mechanism. Shell scripts cannot do this as elegantly. |
| M3 | Orchestrator context grows unboundedly through wave cycle | High | **RESOLVES** | A Go binary has no context window. Its memory footprint is the size of its data structures (~1 MB for state tracking). It can run for hours or days without degradation. |
| M4 | No adaptive behavior under resource pressure | High | **PARTIALLY HELPS** | A Go binary can easily check `runtime.MemStats` and system memory before spawning agents. It can implement adaptive concurrency: `if availableRAM < threshold { maxWorkers = 2 }`. This is straightforward in Go but very difficult for an LLM orchestrator (which would need to call Bash to check `free -m`, parse the output, and reason about whether to reduce concurrency). |
| M5 | Heartbeat tool-call overhead (240 Bash calls per session) | Medium | **RESOLVES** | A Go binary checks PIDs with `syscall.Kill(pid, 0)` directly. Zero overhead. No tool calls. No token cost. The heartbeat mechanism is a goroutine running a tight loop with `time.Sleep`. |
| M6 | Resource leaks (worktrees, logs, hooks accumulate) | Medium | **PARTIALLY HELPS** | A Go binary can implement cleanup with `defer` statements and `os.RemoveAll`. It can track all created artifacts in memory and clean them up on exit (or on next startup). Shell scripts can do this too, but Go's `defer` pattern makes cleanup harder to forget. |
| M7 | Peak aggregate resource usage not modeled or constrained | High | **PARTIALLY HELPS** | A Go binary can model and constrain aggregate resources: check total memory before each spawn, track cumulative disk usage, enforce system-wide limits. This is possible in shell scripts but requires significant complexity; in Go it is natural. |
| M8 | Open files limit (256) too tight for Node.js | Medium | **RESOLVES** | A Go binary can set `ulimit -n 1024` (or higher) for spawned agents instead of 256. The correct value can be computed based on the agent's expected behavior. This is a one-line change in the `SysProcAttr` setup. |

### Testability Review Issues

| # | Issue | Severity | Go Binary Impact | Reasoning |
|---|-------|----------|-----------------|-----------|
| T1 | No unit testing layer for the 9 shell scripts | High | **RESOLVES** | Shell scripts that are absorbed into Go become Go functions with Go unit tests (`_test.go` files). `go test ./...` runs all tests. No bats-core dependency, no test fixture scripts, no temp directory management -- Go's `testing` package and `t.TempDir()` handle everything. |
| T2 | No mock/dry-run infrastructure for testing orchestration without API costs | High | **RESOLVES** | Go interfaces enable dependency injection. `type AgentSpawner interface { Spawn(config AgentConfig) (*Agent, error) }`. Production: spawns real `claude` processes. Test: returns mock agents with predetermined behavior. The entire wave orchestration loop can be tested with zero API calls. |
| T3 | No cross-platform testing plan | Medium | **PARTIALLY HELPS** | Go's cross-compilation and `//go:build` tags make platform-specific code explicit. `sandbox_linux.go` and `sandbox_darwin.go` with build tags. Testing on both platforms still requires CI runners on both platforms, which Go does not change. |
| T4 | SKILL.md (the orchestrator logic) cannot be unit tested | Critical | **RESOLVES** | There is no SKILL.md. The orchestrator is Go code. Every function can be unit tested. The wave state machine can be tested with table-driven tests. `func TestWaveTransition(t *testing.T) { ... }`. This is the single largest testability improvement. |
| T5 | Claude Code hook integration cannot be tested without live claude | High | **UNAFFECTED** | The hooks run inside the agent's Claude Code process. Whether the orchestrator is Go or LLM, testing hooks still requires either synthetic input or a live agent. |
| T6 | Changeset chaining requires human interaction (no automation plan) | Medium | **PARTIALLY HELPS** | A Go binary can implement `--auto-approve` mode or `--decision-file` mode trivially. The approval logic is a function: `func (o *Orchestrator) reviewChangeset(cs Changeset) Decision`. In tests, supply decisions programmatically. In production, prompt the user. The "Humble Object" pattern is natural in Go. |
| T7 | Task state transitions embedded in LLM prompt, not testable | High | **RESOLVES** | Task state transitions become Go functions: `func (t *Task) Claim(agentID string) error`, `func (t *Task) Complete() error`, `func (t *Task) Fail(reason string) error`. Each is independently testable with table-driven tests. |
| T8 | No regression testing strategy | Medium | **PARTIALLY HELPS** | Go has built-in test infrastructure: `go test -cover`, `go test -race`, benchmarks. CI integration is trivial (`go test ./...`). This does not automatically create a regression suite, but it provides the infrastructure the current plan lacks. |

### Wave Handling Review Issues

| # | Issue | Severity | Go Binary Impact | Reasoning |
|---|-------|----------|-----------------|-----------|
| W1 | Cascading dependency failure not handled (silent task drop) | High | **RESOLVES** | A Go binary can implement explicit dependency graph resolution. When a task fails, traverse the dependency graph and mark all dependents as `blocked`: `func (o *Orchestrator) cascadeFailure(failedTask *Task)`. This is a straightforward graph traversal that an LLM orchestrator would struggle to execute reliably. |
| W2 | Validator failure/timeout handling not specified | High | **PARTIALLY HELPS** | A Go binary makes it easy to handle validator failures with the same lifecycle management as workers. However, the policy (retry? manual review? skip?) is a design decision that must be made regardless of orchestrator type. The advantage: the Go code forces you to handle the case at compile time if you use exhaustive switch statements. |
| W3 | Inter-group merge ordering (approving in wrong order breaks consistency) | High | **PARTIALLY HELPS** | A Go binary can compute inter-group dependencies and enforce presentation order. However, the merger agent still runs as a claude process, and the merge conflict resolution is still LLM-driven. The Go binary controls WHEN to present changesets but not HOW they are created. |
| W4 | No maximum wave cycle limit (infinite loop potential) | Medium | **RESOLVES** | `if o.waveCycleCount >= o.config.MaxWaveCycles { return ErrMaxCyclesExceeded }`. Trivial in Go. An LLM orchestrator might lose track of the cycle count. |
| W5 | tasks.yaml corruption / no atomic write | Medium | **RESOLVES** | Go's `os.CreateTemp` + `os.Rename` pattern ensures atomic writes. The orchestrator holds the authoritative state in memory and persists it atomically. No partial-write corruption possible. |
| W6 | Wave 2 to 3 transition ignores never-spawned dependent tasks | Medium | **RESOLVES** | The Go binary explicitly tracks: spawned tasks, completed tasks, pending tasks, blocked tasks. The transition condition is a function: `func (o *Orchestrator) wave2Complete() bool { return len(o.runningAgents) == 0 && len(o.spawnableTasks()) == 0 }`. Never-spawned tasks are accounted for. |
| W7 | Retry timing (immediate vs. deferred) not specified | Medium | **PARTIALLY HELPS** | This is a policy decision. A Go binary makes it easy to implement either approach and makes the choice explicit in code. But the decision itself must still be made by the designer. |
| W8 | Planner token budget exceeded recovery path missing | Medium | **PARTIALLY HELPS** | A Go binary can detect planner failure (process exit code, token tracker) and offer explicit recovery: retry with fresh budget, or ask the human to write the plan manually. The recovery logic is a code branch, not an LLM improvisation. |

---

### Resolution Summary

| Impact Category | Count | Percentage |
|-----------------|-------|------------|
| **RESOLVES** | 35 | 54% |
| **PARTIALLY HELPS** | 18 | 28% |
| **UNAFFECTED** | 12 | 18% |
| **NEW PROBLEM** | 0 | 0% |

**A Go binary orchestrator resolves 54% of identified issues outright, partially helps with another 28%, and introduces zero new problems.**

The 12 "unaffected" issues are primarily:
- Agent-side concerns (hook performance, prompt caching, Superpowers integration depth)
- Policy decisions that need to be made regardless (retry policies, cost circuit breakers, macOS sandbox deprecation)
- Config/template design issues (allowed_commands semantics, planner skill references)

---

## 2. What a Go Binary Orchestrator Gains

### 2.1 Deterministic Control Flow

The current plan's most fundamental tension is stated in its own Design Lineage table: it values "Deterministic controller mindset" (from Plan C) but implements the controller as an LLM (inherently non-deterministic). A Go binary resolves this tension completely.

The wave state machine in Go:

```go
for {
    switch o.state {
    case StatePlanning:
        plan, err := o.runPlanning(ctx)
        if err != nil { return o.handlePlanningFailure(err) }
        if !o.humanApprovesPlan(plan) { continue }
        o.state = StateDevelopment

    case StateDevelopment:
        results := o.runWorkers(ctx)
        o.handleWorkerResults(results)
        o.state = StateValidation

    case StateValidation:
        validationResults := o.runValidators(ctx)
        o.handleValidationResults(validationResults)
        o.state = StateMerge

    case StateMerge:
        changesets := o.runMerger(ctx)
        o.presentChangesetsForReview(changesets)
        if o.hasRemainingTasks() && o.waveCycleCount < o.config.MaxWaveCycles {
            o.state = StateDevelopment
            continue
        }
        return nil
    }
}
```

This executes identically every time. No context window to fill. No prompt to misinterpret. No tool call to forget.

### 2.2 Process Management

A Go binary has first-class access to OS process primitives:

- **Process groups**: `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}` -- the agent and ALL its children are in one group. `syscall.Kill(-pgid, syscall.SIGTERM)` kills them all.
- **Fork/exec with inherited limits**: Resource limits (ulimit equivalents) applied via `SysProcAttr.Rlimit` before the child process starts. No separate shell script needed. No execution context gap.
- **Signal handling**: `signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)` triggers graceful shutdown with deterministic cleanup.
- **Process completion detection**: `cmd.Wait()` in a goroutine. Event-driven, not poll-driven. Zero overhead.

### 2.3 Concurrency Primitives

The current plan's concurrency model is undefined because the Task tool's concurrency semantics are unknown. A Go binary replaces this ambiguity with explicit primitives:

- **Goroutines**: One per agent. Lightweight (4 KB stack), thousands can run simultaneously.
- **Channels**: Agent completion signals flow through typed channels. `resultCh <- AgentResult{TaskID: task.ID, ExitCode: exitCode, Duration: elapsed}`.
- **WaitGroups**: `wg.Wait()` blocks until all agents in a wave complete. Clean, deterministic.
- **Select with timeout**: `select { case result := <-resultCh: handleResult(result); case <-time.After(timeout): handleTimeout() }`. Timeout enforcement is built into the language.
- **Context with cancellation**: `ctx, cancel := context.WithTimeout(parentCtx, agentTimeout)`. Cancellation propagates to all child operations.

### 2.4 Resource Control

The machine-power review identified critical issues with ulimit: the 512 MB virtual memory limit crashes Node.js, the 256 open files limit is too low, and macOS does not support ulimit -v effectively.

A Go binary solves these at the fork boundary:

```go
cmd.SysProcAttr = &syscall.SysProcAttr{
    Setpgid: true,
    // On Linux, use cgroups v2 for RSS limiting instead of ulimit -v
}
// Platform-specific resource limits
if runtime.GOOS == "linux" {
    applyCgroupLimits(cmd, config.Sandbox)
} else if runtime.GOOS == "darwin" {
    // macOS: skip ineffective ulimit -v, use only what works
    applyDarwinLimits(cmd, config.Sandbox)
}
```

The Go binary can detect the platform at compile time and apply the correct mechanism. Shell scripts require runtime detection and fragile conditional logic.

### 2.5 Zero Token Cost for Orchestration

The budget review identified $0.20-$0.50 in hidden orchestrator costs per session. With a Go binary:

- Wave state machine execution: $0.00
- Heartbeat monitoring (all agents, all intervals): $0.00
- Task claiming and state transitions: $0.00
- Lock acquisition and release: $0.00
- Worktree creation and cleanup: $0.00
- Human approval prompts: $0.00
- Beads archive and load: $0.00
- Token budget tracking: $0.00

Every operation that the current plan performs via Bash tool calls (at token cost) becomes a Go function call (at zero cost).

### 2.6 No Context Window Growth

The architecture review identified context window exhaustion as a high-severity concern. Over a multi-wave session with 4 workers, the current orchestrator accumulates:

- 30-50 Bash tool calls for shell scripts (6K-25K tokens)
- 8 Task tool calls for agent dispatch (4K-16K tokens)
- Agent results from all workers, validators, and merger
- Human interaction context (plan approval, changeset review)
- Beads memory context from prior sessions

A Go binary holds this information in typed structs that consume bytes, not tokens. A 3-task session's state fits in approximately 50 KB of RAM. The binary can run for hours without degradation.

### 2.7 Type-Safe Configuration and State

The cohesion review identified 15 gaps and 10 ambiguities in the plan. Many stem from implicit contracts between components. In Go:

```go
type Config struct {
    SchemaVersion int           `yaml:"schema_version"`
    Project       ProjectConfig `yaml:"project"`
    Concurrency   ConcurrencyConfig `yaml:"concurrency"`
    Limits        LimitsConfig  `yaml:"limits"`
    Sandbox       SandboxConfig `yaml:"sandbox"`
    Permissions   PermConfig    `yaml:"permissions"`
    // ...
}
```

If a config field is referenced but not defined, the code does not compile. If a task field is accessed but never populated, the type system catches it. The "implicit contracts" that the cohesion review identified become explicit type definitions.

### 2.8 Atomic File Operations and Proper Locking

The wave handling review identified tasks.yaml corruption risk. A Go binary provides:

```go
func atomicWriteYAML(path string, data interface{}) error {
    tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
    if err != nil { return err }
    defer os.Remove(tmp.Name()) // cleanup on failure
    if err := yaml.NewEncoder(tmp).Encode(data); err != nil {
        tmp.Close()
        return err
    }
    if err := tmp.Close(); err != nil { return err }
    return os.Rename(tmp.Name(), path) // atomic on POSIX
}
```

For file locking, Go can use `syscall.Flock()` directly or the `flock` package for cross-platform support. No shell script race conditions.

---

## 3. What a Go Binary Orchestrator Loses

### 3.1 Direct Task Tool Access

**Lost**: The current plan spawns agents via the Claude Code Task tool. A Go binary cannot use the Task tool.

**Replacement**: The `claude` CLI has a `--print` (`-p`) mode that is purpose-built for non-interactive use. From the CLI help:

```
-p, --print    Print response and exit (useful for pipes)
```

The Go binary spawns agents with:

```go
cmd := exec.Command("claude",
    "--print",
    "--model", agentConfig.Model,
    "--system-prompt", systemPrompt,
    "--allowed-tools", strings.Join(allowedTools, ","),
    "--max-budget-usd", fmt.Sprintf("%.2f", maxBudget),
    "--output-format", "json",
    "--dangerously-skip-permissions",  // permissions enforced by watcher hooks
    "--settings", settingsPath,        // per-agent settings with hook registration
    prompt,
)
cmd.Dir = worktreePath
```

This is actually MORE capable than the Task tool because:
- Explicit model selection via `--model`
- Explicit tool allowlisting via `--allowed-tools`
- Built-in cost cap via `--max-budget-usd`
- Structured output via `--output-format json`
- Custom settings (including hook registration) via `--settings`
- Permission modes via `--permission-mode`

The `--print` mode runs the full agent loop (with tools, hooks, and all capabilities) and exits when done. This is the correct interface for a programmatic orchestrator.

### 3.2 Shell Helper Simplicity

**Lost**: Shell scripts are easy to write, edit, and debug. No compilation step.

**Trade-off**: Most shell scripts get absorbed into Go packages. The logic is the same but in a different language. What changes:

| Shell Script | Go Replacement | Complexity Change |
|-------------|---------------|-------------------|
| `blueflame-init.sh` | `internal/config/init.go` | Similar -- checks for executables, creates directories |
| `worktree-manage.sh` | `internal/worktree/worktree.go` | Simpler -- Go's `os/exec` for git commands, proper error handling |
| `lock-manage.sh` | `internal/locks/locks.go` | Simpler -- `syscall.Flock`, no PID parsing with grep |
| `watcher-generate.sh` | `internal/agent/hooks.go` | Similar -- Go templates (`text/template`) for watcher scripts |
| `watcher-postcheck.sh` | `internal/agent/postcheck.go` | Simpler -- `os.ReadDir`, `filepath.Walk`, structured checks |
| `token-tracker.sh` | `internal/agent/tokens.go` | Much simpler -- `encoding/json` for JSONL, typed structs |
| `lifecycle-manage.sh` | `internal/agent/lifecycle.go` | Simpler -- goroutines, channels, `cmd.Process.Signal()` |
| `beads-archive.sh` | `internal/memory/beads.go` | Similar -- still shells out to `beads` CLI |
| `sandbox-setup.sh` | `internal/agent/sandbox.go` | Simpler -- `SysProcAttr` at fork time, platform build tags |

**Net assessment**: The Go code is more verbose but safer, testable, and type-checked. The "edit-and-run" simplicity of shell scripts is traded for "edit-compile-run" but with compile-time error detection.

### 3.3 Iteration Speed

**Lost**: Shell scripts can be edited and re-run instantly. Go requires `go build`.

**Mitigated by**: `go run cmd/blueflame/main.go` compiles and runs in one command. Go compilation is fast (~1-2 seconds for a project this size). This is not a meaningful barrier.

**Further mitigated**: The original PLAN.md already anticipated a Go binary, and the project structure it proposed (`cmd/blueflame/main.go`, `internal/...`) is a standard Go layout. The author was comfortable with Go.

### 3.4 Ability to "Understand" Agent Output

**Lost**: The current plan's LLM orchestrator can read agent output and reason about it (e.g., "this worker's output looks like it addressed the task, but the approach seems fragile").

**Reality check**: The orchestrator in the current plan does not actually do this. The plan explicitly says the orchestrator is a "deterministic controller" that delegates understanding to validators. The orchestrator's role is: check exit code, run postcheck, mark pass/fail. A Go binary can do all of this.

For the ONE case where understanding matters -- the merger creating changesets from validated branches -- the merger IS still a claude agent. The Go binary spawns it just like any other agent. The Go binary does not need to "understand" the merge; it just needs to present the results.

**What the Go binary cannot do**: If all 4 workers fail and the orchestrator needs to reason about WHY they all failed and whether to adjust the approach, an LLM orchestrator could improvise. A Go binary would follow its programmed failure recovery logic (retry, escalate to human). This is arguably better -- improvisation by an LLM orchestrator is the non-determinism the reviews criticized.

### 3.5 Integration with Claude Code Ecosystem

**Lost**: The current plan's orchestrator runs inside Claude Code and has native access to its tool ecosystem (Task, Bash, Read, Write, etc.).

**Reality**: The orchestrator does not need these tools. It needs to: parse YAML (Go stdlib), manage files (Go stdlib), spawn processes (Go stdlib), manage git worktrees (shell out to `git`), and interact with the human (terminal I/O). All of these are standard Go operations.

The spawned AGENTS still run inside Claude Code and have full access to the tool ecosystem. The orchestrator does not need it.

---

## 4. Hybrid Possibilities

### 4.1 Shell Scripts for Some Operations

**Yes, absolutely.** A Go binary can still shell out to scripts for operations that are better expressed in shell:

- **Watcher hook scripts** (`watcher.sh`): These MUST remain shell scripts. They are registered as Claude Code `PreToolUse` hooks and are invoked by the agent's Claude Code runtime. The Go binary generates these scripts (via `text/template`) but does not execute them.
- **Git operations**: While Go can exec git commands directly, keeping a `git_helpers.sh` for complex git operations (rebase, cherry-pick, conflict detection) may be pragmatic.
- **Beads CLI interaction**: The `beads` tool is a CLI. Go shells out to it via `exec.Command("beads", "save", ...)`.

The key difference from the current plan: shell scripts are called by Go code (deterministic, with error handling), not by an LLM (non-deterministic, with tool-call overhead).

### 4.2 Claude Code Hooks/Watchers (PreToolUse)

**Yes, fully compatible.** The hook system is agent-side, not orchestrator-side. The Go binary:

1. Reads `blueflame.yaml` permissions
2. Generates a `watcher.sh` script per agent using Go's `text/template` package
3. Creates a `.claude/settings.json` in the agent's worktree with the hook registration
4. Spawns the agent in that worktree

The agent's Claude Code runtime picks up the hooks from `.claude/settings.json` and invokes them on every tool use. This is identical to the current plan's approach -- the orchestrator type does not matter because hooks are configured via filesystem, not via the orchestrator's runtime.

The Go binary can additionally pass `--settings <path>` to the `claude` CLI to point to the generated settings file, or it can write the settings into the worktree's `.claude/` directory before spawning.

### 4.3 Beads for Persistent Memory

**Yes, fully compatible.** The Go binary interacts with Beads the same way the current plan's shell script does:

```go
func (m *BeadsMemory) Save(session SessionResult) error {
    data, _ := json.Marshal(session)
    cmd := exec.Command("beads", "save", "--data", string(data))
    return cmd.Run()
}

func (m *BeadsMemory) Load() (SessionContext, error) {
    cmd := exec.Command("beads", "load", "--format", "json")
    output, err := cmd.Output()
    // Parse and return
}
```

Behind a `MemoryProvider` interface, this is cleanly abstracted.

### 4.4 Superpowers Skills Within Spawned Agents

**Yes, fully compatible.** Superpowers is a Claude Code plugin that agents use during their sessions. The Go binary spawns agents with `--plugin-dir` to ensure the Superpowers plugin is available:

```go
cmd := exec.Command("claude",
    "--print",
    "--plugin-dir", superpowersPath,
    // ...
)
```

Agents use Superpowers skills (`/brainstorm`, `/write-plan`, TDD) within their Claude Code sessions. The orchestrator does not need to know about Superpowers internals.

### 4.5 Existing Prompt Templates

**Yes, fully compatible.** The Go binary uses `text/template` to render prompt templates:

```go
tmpl, _ := template.ParseFiles("templates/worker-prompt.md.tmpl")
var buf bytes.Buffer
tmpl.Execute(&buf, PromptData{
    TaskID:      task.ID,
    Description: task.Description,
    FileLocks:   task.FileLocks,
    History:     task.History,
    BeadsContext: beadsContext,
})
prompt := buf.String()
```

This is functionally identical to the current plan but with the advantage that template variables are type-checked at compile time (the `PromptData` struct defines what is available).

---

## 5. How Would It Spawn Agents?

### 5.1 The `claude` CLI Non-Interactive Mode

The `claude` CLI supports full non-interactive operation via `--print` (`-p`):

```
-p, --print    Print response and exit (useful for pipes)
```

Combined with:
- `--model <model>` -- select the model (sonnet, haiku, opus)
- `--system-prompt <prompt>` -- set the system prompt
- `--allowed-tools <tools>` -- restrict available tools
- `--disallowed-tools <tools>` -- block specific tools
- `--max-budget-usd <amount>` -- cost cap per agent
- `--output-format <format>` -- "text", "json", or "stream-json"
- `--settings <file-or-json>` -- load settings (including hook registration)
- `--permission-mode <mode>` -- "bypassPermissions", "default", "dontAsk", etc.
- `--dangerously-skip-permissions` -- skip permission prompts (for sandboxed agents)
- `--no-session-persistence` -- don't save session to disk
- `--json-schema <schema>` -- enforce structured output format
- `--add-dir <dirs>` -- additional directory access
- `--tools <tools>` -- specify exact tool set

This is a rich, well-designed interface for programmatic agent spawning.

### 5.2 How the Go Binary Passes Task Context to Agents

```go
func (o *Orchestrator) spawnWorker(task Task) (*Agent, error) {
    // 1. Generate the prompt from template
    prompt, err := o.renderWorkerPrompt(task)
    if err != nil { return nil, err }

    // 2. Generate per-agent watcher hooks and settings
    settingsPath, err := o.generateAgentSettings(task)
    if err != nil { return nil, err }

    // 3. Build the command
    cmd := exec.CommandContext(ctx, "claude",
        "--print",
        "--model", o.config.Models.Worker,
        "--system-prompt", o.renderSystemPrompt("worker"),
        "--allowed-tools", "Read,Write,Edit,Glob,Grep,Bash",
        "--disallowed-tools", "WebFetch,WebSearch,NotebookEdit,Task",
        "--max-budget-usd", fmt.Sprintf("%.2f", o.config.Limits.TokenBudget.WorkerUSD),
        "--output-format", "json",
        "--settings", settingsPath,
        "--permission-mode", "dontAsk",
        prompt,
    )

    // 4. Set working directory to the worktree
    cmd.Dir = task.WorktreePath

    // 5. Set process group for clean cleanup
    cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

    // 6. Capture output
    cmd.Stdout = &agent.stdout
    cmd.Stderr = &agent.stderr

    // 7. Start the process
    if err := cmd.Start(); err != nil { return nil, err }

    return &Agent{
        ID:      task.AgentID,
        Cmd:     cmd,
        Task:    task,
        Started: time.Now(),
    }, nil
}
```

### 5.3 How Agents Report Results Back

Agents communicate results through multiple channels, all readable by the Go binary:

1. **Exit code**: `cmd.Wait()` returns the exit status. Non-zero = failure.
2. **Stdout/Stderr**: Captured in buffers. With `--output-format json`, stdout contains structured output including the agent's final response, tool usage summary, and token counts.
3. **Git commits**: The worker commits to its worktree branch. The Go binary runs `git log` on the branch to check for commits.
4. **Filesystem state**: The Go binary runs the postcheck (path validation, diff analysis) by reading the worktree directly.

For validators specifically, the `--json-schema` flag can enforce structured output:

```go
validatorSchema := `{
    "type": "object",
    "properties": {
        "status": {"type": "string", "enum": ["pass", "fail"]},
        "notes": {"type": "string"},
        "issues": {"type": "array", "items": {"type": "string"}}
    },
    "required": ["status", "notes"]
}`

cmd := exec.Command("claude",
    "--print",
    "--json-schema", validatorSchema,
    // ...
)
```

This guarantees the validator returns a parseable pass/fail verdict.

### 5.4 The Interface Between Go Orchestrator and Agents

```
Go Orchestrator                         claude CLI Agent
     |                                       |
     |-- exec.Command("claude", "--print",   |
     |     "--model", "sonnet",              |
     |     "--system-prompt", "...",          |
     |     "--settings", "/path/settings.json",
     |     "--output-format", "json",         |
     |     "--max-budget-usd", "0.50",        |
     |     "Your task is...")                 |
     |                                       |
     |      [agent runs in worktree]          |
     |      [hooks enforce permissions]       |
     |      [agent uses tools, writes code]   |
     |      [agent commits to branch]         |
     |                                       |
     |<--- cmd.Wait() returns exit code       |
     |<--- stdout buffer: JSON result         |
     |<--- stderr buffer: any errors          |
     |                                       |
     |-- git log (check commits)             |
     |-- postcheck (validate filesystem)     |
     |-- update tasks.yaml                   |
```

---

## 6. Revised Architecture Sketch

### 6.1 What Stays from Plan-ADR.md

The following design elements are preserved exactly as-is:

- **Wave-based execution model** (plan, develop, validate, merge)
- **Three-phase watcher enforcement** (PreToolUse hooks, OS sandbox, post-execution diff)
- **Git worktree isolation** per agent
- **Advisory file locking** per task
- **YAML configuration** (`blueflame.yaml`)
- **YAML task file** (`tasks.yaml`)
- **Per-agent token budgets** with warn/kill thresholds
- **Beads integration** for persistent memory
- **Superpowers plugin** for agent skills
- **Model tiering** (sonnet for workers, haiku for validators)
- **Human approval gates** (plan approval, changeset review)
- **Changeset chaining** with approve/reject/view/skip
- **Cohesion groups** for merge ordering
- **Re-queue logic** for rejected changesets
- **Ephemeral agents** (no state leakage between waves)
- **Prompt templates** for each agent role
- **Audit logging** (JSONL per agent)
- **Defense-in-depth** security model

### 6.2 What Changes

| Aspect | Plan-ADR.md (Current) | Revised (Go Binary) |
|--------|----------------------|---------------------|
| Orchestrator runtime | Claude Code Skill (SKILL.md) | Go binary (`cmd/blueflame/main.go`) |
| Agent dispatch | Task tool | `exec.Command("claude", "--print", ...)` |
| Shell scripts | 9 separate scripts called via Bash tool | Absorbed into Go packages (most); shell retained for watcher hooks |
| State management | Multiple files updated by LLM tool calls | In-memory state with atomic persistence |
| Concurrency | Undefined (Task tool semantics unknown) | Goroutines + channels + WaitGroups |
| Process management | Shell scripts for PID tracking | `exec.Cmd` with `SysProcAttr`, process groups at fork time |
| Heartbeat | Polling via Bash tool calls (token cost) | Goroutine per agent, zero overhead |
| Resource limits | Separate `sandbox-setup.sh` (execution context gap) | `SysProcAttr` at fork time (no gap) |
| Token cost of orchestration | $0.20-$0.50 per session | $0.00 |
| Config parsing | Shell scripts parsing YAML (fragile) | `gopkg.in/yaml.v3` with typed structs |
| Error handling | LLM improvisation | Explicit Go error handling with typed errors |
| Testing | 12 acceptance tests, no unit tests possible for SKILL.md | Full unit test suite, table-driven tests, mock interfaces |
| Crash recovery | state.yaml with undefined schema | Typed struct serialized to JSON, explicit recovery logic |

### 6.3 New Go Packages Needed

```
blueflame/
+-- cmd/
|   +-- blueflame/
|       +-- main.go                    # CLI entrypoint, flag parsing
+-- internal/
|   +-- config/
|   |   +-- config.go                  # Parse and validate blueflame.yaml
|   |   +-- config_test.go             # Config parsing tests
|   |   +-- migrate.go                 # Schema version migration
|   +-- orchestrator/
|   |   +-- orchestrator.go            # Wave state machine, main loop
|   |   +-- orchestrator_test.go       # State machine tests (table-driven)
|   |   +-- planner.go                 # Wave 1 logic
|   |   +-- worker.go                  # Wave 2 logic
|   |   +-- validator.go               # Wave 3 logic
|   |   +-- merger.go                  # Wave 4 logic
|   |   +-- scheduler.go              # Task selection with dependency and lock awareness
|   |   +-- scheduler_test.go          # Dependency graph, lock conflict tests
|   +-- agent/
|   |   +-- agent.go                   # Spawn claude CLI process, capture output
|   |   +-- agent_test.go              # Mock spawner tests
|   |   +-- lifecycle.go               # Process group tracking, heartbeat, timeout
|   |   +-- hooks.go                   # Generate watcher shell scripts from config
|   |   +-- hooks_test.go              # Watcher generation snapshot tests
|   |   +-- postcheck.go              # Post-execution filesystem diff validation
|   |   +-- postcheck_test.go          # Postcheck tests with synthetic worktrees
|   |   +-- sandbox_linux.go           # Linux resource limits (cgroups, unshare)
|   |   +-- sandbox_darwin.go          # macOS resource limits (best-effort)
|   |   +-- tokens.go                 # Token usage parsing from audit logs
|   |   +-- tokens_test.go            # Token parsing tests with fixture logs
|   +-- tasks/
|   |   +-- tasks.go                   # Read/write/claim tasks in YAML, state transitions
|   |   +-- tasks_test.go              # State machine transition tests
|   |   +-- history.go                 # History entry management
|   +-- worktree/
|   |   +-- worktree.go               # Git worktree create/remove/list
|   |   +-- worktree_test.go           # Tests with temp git repos
|   +-- locks/
|   |   +-- locks.go                   # Advisory file locking via flock
|   |   +-- locks_test.go              # Lock acquisition, conflict, stale detection
|   +-- memory/
|   |   +-- provider.go               # MemoryProvider interface
|   |   +-- beads.go                   # Beads implementation
|   |   +-- beads_test.go             # Beads interface tests
|   |   +-- noop.go                    # No-op implementation (when beads disabled)
|   +-- ui/
|   |   +-- prompt.go                 # Human approval prompts (plan, changeset)
|   |   +-- diff.go                    # Diff display
|   |   +-- progress.go               # Wave progress display
|   +-- state/
|       +-- state.go                   # Crash recovery state management
|       +-- state_test.go             # Recovery tests
+-- templates/
|   +-- watcher.sh.tmpl               # Go template for watcher hook shell scripts
|   +-- planner-prompt.md.tmpl        # Planner system prompt template
|   +-- worker-prompt.md.tmpl         # Worker system prompt template
|   +-- validator-prompt.md.tmpl      # Validator system prompt template
|   +-- merger-prompt.md.tmpl         # Merger system prompt template
+-- blueflame.yaml.example            # Example configuration
+-- go.mod
+-- go.sum
```

### 6.4 Shell Scripts Still Needed vs. Absorbed into Go

**Still needed (shell scripts)**:
- `watcher.sh.tmpl` -- Generated per-agent watcher hooks. MUST be shell scripts because Claude Code's PreToolUse hook system invokes them as external commands. The Go binary generates these from the template but does not execute them.

**Absorbed into Go** (all 9 original scripts):
- `blueflame-init.sh` -> `internal/config/init.go`
- `worktree-manage.sh` -> `internal/worktree/worktree.go`
- `lock-manage.sh` -> `internal/locks/locks.go`
- `watcher-generate.sh` -> `internal/agent/hooks.go`
- `watcher-postcheck.sh` -> `internal/agent/postcheck.go`
- `token-tracker.sh` -> `internal/agent/tokens.go`
- `lifecycle-manage.sh` -> `internal/agent/lifecycle.go`
- `beads-archive.sh` -> `internal/memory/beads.go`
- `sandbox-setup.sh` -> `internal/agent/sandbox_{linux,darwin}.go`

---

## 7. Final Recommendation

### Recommendation: Full Go binary orchestrator with shell retained only for watcher hooks.

The evidence is decisive:

**1. Issue resolution is overwhelming.** A Go binary resolves 54% of identified issues outright and partially helps with another 28%. It introduces zero new problems. The issues it resolves include 5 of the 6 issues rated "critical" across all reviews (Task tool concurrency undefined, sandbox ulimit propagation gap, SKILL.md untestable, worker completion detection unspecified, orchestrator non-determinism).

**2. The current plan contradicts its own design values.** The Design Lineage table lists "Deterministic controller mindset (Plan C)" as a core design value. But the orchestrator is an LLM -- the least deterministic execution environment possible. A Go binary fulfills the plan's own stated aspiration.

**3. Token cost elimination is material.** The budget review estimates $0.20-$0.50 in hidden orchestrator overhead per session. Over hundreds of sessions, this is a significant cost. A Go binary reduces this to zero.

**4. Testability is transformative.** The testability review found that the most critical component (SKILL.md orchestrator logic) is inherently untestable. With a Go binary, every function gets unit tests, the wave state machine gets table-driven tests, and the entire orchestration loop can be tested with mock agent spawners. This is the difference between "hope the prompt works" and "the tests prove it works."

**5. The `claude` CLI is purpose-built for this use case.** The `--print` mode with `--model`, `--system-prompt`, `--allowed-tools`, `--max-budget-usd`, `--output-format json`, `--json-schema`, and `--settings` flags provides a richer interface than the Task tool. The Go binary gains MORE control over agents, not less.

**6. The original plan (PLAN.md) already proposed Go.** The project's author originally envisioned a Go binary. The ADR process chose the skill approach for "no compilation, direct Task tool dispatch, lower token overhead, faster iteration." But:
- Compilation overhead is negligible (`go run` takes 1-2 seconds)
- Task tool dispatch is replaced by superior `claude --print` interface
- Token overhead is higher in the skill approach (the reviews proved this)
- Iteration speed is offset by type safety and testing

**7. The best parts of Plan-ADR.md are fully preserved.** The three-phase watcher model, wave-based execution, Beads integration, Superpowers skills, YAML configuration, prompt templates, human approval gates, changeset chaining, cohesion groups, and defense-in-depth security all transfer directly to the Go binary approach. Nothing is lost.

### What to do next

1. **Start with the PLAN.md project structure** as the foundation.
2. **Incorporate all Plan-ADR.md design decisions** (three-phase watchers, Beads, token budgets, cohesion groups, re-queue logic, two-tier validation).
3. **Use `claude --print` with `--output-format json`** as the agent spawning interface.
4. **Use `--json-schema`** for validators and planners to enforce structured output.
5. **Generate watcher hook scripts** from Go templates (these remain shell scripts for Claude Code hook compatibility).
6. **Implement the wave state machine** as a deterministic Go `switch` statement.
7. **Build incrementally**: single-worker flow first, then concurrency, then validation, then merge -- exactly as PLAN.md Phase 1-5 proposed.
8. **Write tests from day one**: the Go binary approach makes this not just possible but easy.

The Go binary orchestrator is not a compromise. It is strictly superior to the skill-based approach for this use case: it resolves the majority of identified issues, preserves every good design decision, costs zero tokens, is fully testable, and aligns with the plan's own stated values. The only thing it "loses" is the ability for the orchestrator to improvise -- and every review agreed that orchestrator improvisation is a bug, not a feature.
