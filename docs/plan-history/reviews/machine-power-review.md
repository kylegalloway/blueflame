# Machine Power & Resource Consumption Review

## Plan Under Review: Blue Flame (Plan-ADR.md)

**Reviewer Focus**: Hardware requirements, resource consumption, feasibility on modest hardware

---

## Minimum Viable Hardware Estimate

| Resource | Minimum (1-2 workers) | Recommended (4 workers) | Notes |
|----------|----------------------|------------------------|-------|
| CPU | 4 cores | 8 cores | Each claude CLI process is CPU-intensive during inference I/O handling |
| RAM | 8 GB | 16 GB | Orchestrator + 4 agents + git + OS overhead |
| Disk (system) | 2 GB free | 5 GB free | Worktrees, logs, state files, audit trails |
| Disk (repo-dependent) | 1x repo size per worktree | 4x repo size for worktrees | Git worktrees share objects but duplicate working trees |
| Disk I/O | SSD strongly recommended | NVMe preferred | Concurrent git operations are I/O-bound |
| Network | Broadband for API calls | Same | All LLM inference is remote via claude CLI |

---

## Detailed Findings

### 1. Running Up to 4 Concurrent claude CLI Processes

**RISK**

The plan allows up to 4 concurrent worker agents (configurable via `concurrency.development: 4`), each running a `claude` CLI process. Each `claude` CLI process is a Node.js application that:

- Maintains a persistent connection to Anthropic's API
- Holds conversation context in memory (prompt + response history)
- Spawns child processes for tool execution (Bash, etc.)
- Runs PreToolUse hook scripts on every tool invocation

A single `claude` CLI process at rest consumes roughly 150-300 MB of RSS memory. With an active conversation context containing code files and diffs, this can climb to 400-600 MB. Four concurrent instances therefore require 1.6-2.4 GB of RAM just for the agent processes, before accounting for the orchestrator's own context, the OS, git operations, and any build tools (go test, npm test) the agents invoke.

On a machine with 8 GB RAM, running 4 agents concurrently leaves roughly 5.5-6 GB for everything else. This is feasible but tight, especially if agents trigger compilation or test suites that are themselves memory-hungry. On a machine with 4 GB RAM, this configuration would cause swapping and severe degradation.

The plan sets `sandbox.max_memory_mb: 512` per agent, which totals 2 GB for 4 workers. This is a reasonable cap, but see finding #4 for concerns about how ulimit interacts with Node.js processes.

Additionally, the plan does not account for Wave 3 (Validation), which can run up to `concurrency.validation: 2` validators concurrently. If validation overlaps with late-finishing workers (the plan says "once ALL workers complete" but a crash recovery scenario could blur this), peak concurrency could exceed 4 processes.

### 2. Git Worktree Disk Consumption

**CONCERN**

Git worktrees share the object database with the main repository, which is a significant space saving. However, each worktree duplicates the entire working tree -- every checked-out file. For a repository with:

- 100 MB working tree: 4 worktrees = 400 MB additional disk (manageable)
- 1 GB working tree: 4 worktrees = 4 GB additional disk (notable)
- 5 GB working tree (monorepo with assets): 4 worktrees = 20 GB additional disk (problematic)

The plan mentions `worktree_dir: ".trees"` and cleanup via `worktree-manage.sh cleanup` after Wave 4. This means worktrees persist through Waves 2, 3, and 4 -- potentially for a long time in a multi-wave session. The graceful shutdown section explicitly states "Do NOT clean worktrees (preserve for debugging)," which means crash scenarios leave worktrees on disk indefinitely.

The plan does not mention:
- Sparse checkout support for worktrees (only check out files relevant to the task)
- Disk space checks before creating worktrees
- Warnings when worktree creation would exceed available disk
- Maximum worktree age or staleness detection beyond crash recovery

For large repositories, worktree disk consumption is a real concern that could exhaust disk space on modest hardware.

### 3. Disk Space for Logs and Audit Trails

**CONCERN**

The plan creates per-agent audit logs at `.blueflame/logs/<agent-id>.audit.jsonl`. Every PreToolUse hook invocation writes a JSONL entry. A typical agent session might invoke 50-200 tool calls, each generating a log entry. With 4 workers + 2 validators + 1 planner + 1 merger per wave cycle, and potentially multiple wave cycles per session, the log volume grows steadily.

Individual log entries are small (likely 200-500 bytes each), so a single session produces perhaps 1-5 MB of logs. This is negligible for one session, but the plan does not mention log rotation, log size limits, or log cleanup across sessions. Over dozens of sessions, logs could accumulate without bound.

Combined with Beads archive data and the `.blueflame/state.yaml` crash recovery file, the `.blueflame/` directory will grow over time. The plan should specify a retention policy.

### 4. ulimit/Sandbox Constraints Appropriateness

**RISK**

The default sandbox limits are:
- `max_cpu_seconds: 600` (10 minutes CPU time)
- `max_memory_mb: 512` (512 MB virtual memory)
- `max_file_size_mb: 50`
- `max_open_files: 256`

Several concerns:

**Memory limit (512 MB)**: The plan uses `ulimit -v` (virtual memory limit). On modern systems, virtual memory size is vastly larger than RSS (resident set size). A Node.js process (which claude CLI is) typically maps 1-2 GB of virtual address space even when only using 200 MB of RSS. Setting `ulimit -v 524288` (512 MB in KB) would almost certainly cause the `claude` CLI process to crash immediately on startup, because Node.js's V8 engine reserves virtual memory far beyond 512 MB.

This is a critical miscalibration. Either:
- The limit needs to be 2-4 GB of virtual memory to allow Node.js to function, which defeats the purpose of a tight memory limit
- A different mechanism is needed (cgroups on Linux, which can limit RSS directly)
- On macOS, there is no good equivalent to cgroups for RSS limiting; ulimit -v is the only option and it interacts poorly with Node.js

**Open files (256)**: The `claude` CLI process, its child processes, git operations, and file editing tools all consume file descriptors. Node.js itself uses file descriptors for internal operations (libuv thread pool, DNS resolution, etc.). 256 is tight. A worker running `go test ./...` on a large project could easily open hundreds of files. This limit may cause spurious failures in build/test commands that are not actually misbehaving.

**CPU time (600s)**: This is 10 minutes of CPU time, which is distinct from wall-clock time. For an I/O-bound process like `claude` CLI (which spends most of its time waiting for API responses), 600 seconds of CPU time likely corresponds to 30-60 minutes of wall-clock time. This is reasonable but loosely correlated with the `agent_timeout: 300s` wall-clock limit. The interaction between these two limits is not discussed.

### 5. Memory Footprint of the Orchestrator (Parent Context)

**CONCERN**

The orchestrator is itself a Claude Code session -- the parent context. It must hold in memory:

- The full SKILL.md definition
- The blueflame.yaml configuration
- The tasks.yaml content (grows with task count)
- The agents.json registry
- Conversation history from all human interactions (plan approval, changeset review)
- Context from Beads memory loading
- Results and status from all spawned agents (via Task tool responses)

As a wave cycle progresses, the orchestrator's context grows. By Wave 4, it has accumulated context from planning, all worker results, all validator results, and merger output. For a session with 4 tasks, this could be 50-100K tokens of context in the parent, which translates to significant memory for the parent `claude` process.

The plan describes the orchestrator as having "minimal" cost and says "shell helpers do mechops," but the orchestrator's own memory footprint is not truly minimal -- it is the longest-lived process and accumulates the most context. The plan does not discuss context window pressure on the orchestrator or strategies for summarizing intermediate results.

This is especially relevant because the orchestrator runs for the entire session, across all waves, potentially for hours. Its memory will not be reclaimed until the session ends.

### 6. Shell Script vs. Compiled Go Binary: Resource Comparison

**EFFICIENT** (with caveats)

The plan explicitly chose shell scripts over a compiled Go binary for "mechanical operations" (worktree management, lock management, watcher generation, etc.). From a resource perspective:

**Advantages of shell scripts**:
- No compilation step (zero build-time resource usage)
- Each script runs, does its work, and exits (no persistent memory footprint)
- Bash is universally available; no runtime dependencies
- Individual script invocations are very lightweight (typically < 10 MB RSS, < 100ms)

**Disadvantages vs. a Go binary**:
- Each shell script invocation forks a new process (fork+exec overhead)
- Shell scripts that parse YAML (tasks.yaml, blueflame.yaml) will likely shell out to `yq` or use fragile sed/awk parsing -- this is slower and more error-prone than a Go YAML parser
- No connection pooling, caching, or shared state between script invocations
- The orchestrator (Claude Code) must invoke each script via the Bash tool, which has its own overhead (tool call round-trip, output parsing)

**Net assessment**: For the frequency of invocation (a few dozen script calls per wave cycle), the shell approach is genuinely lighter in total resource consumption. A persistent Go binary would consume 10-30 MB of RSS continuously, while the shell scripts consume nothing between invocations. The overhead per invocation is small enough that it does not matter at this scale. The claim is valid.

However, the shell scripts do add latency. Each Bash tool invocation through Claude Code has overhead (the LLM must decide to call the tool, format the call, parse the result). This is a token cost, not a hardware cost, but it is worth noting.

### 7. Concurrent Git Operations and Disk I/O

**CONCERN**

When 4 workers run concurrently, each in its own worktree, the following git operations can happen simultaneously:

- `git add` (updates the shared index-like structure per worktree)
- `git commit` (writes objects to the shared object database)
- `git diff` (reads from the shared object database)
- `git status` (stats every file in the worktree)

Git worktrees share the object database (`.git/objects/`), which means concurrent commits all write to the same packfile or loose object directory. Git uses file-level locking for some operations (e.g., `refs/` updates), which means concurrent commits can block each other briefly.

On an HDD, concurrent `git status` across 4 worktrees would be painful -- each invocation stats every file in the working tree. On an SSD, this is typically fast (< 1 second for repositories with < 100K files). On NVMe, it is negligible.

The plan does not discuss:
- Disk I/O contention between concurrent workers
- Whether `git gc` or `git repack` might run automatically and lock the shared object database
- The interaction between workers committing and the merger reading branches

For modest hardware with an HDD (which is rare in 2026 but not impossible), concurrent git operations could be a bottleneck.

### 8. YAML-Based State Management I/O Bottlenecks

**CONCERN**

The plan uses two YAML/JSON files as shared state:
- `tasks.yaml`: read and written by the orchestrator to claim tasks, update status
- `agents.json`: read and written to track agent lifecycle

The orchestrator is the only writer for both files (agents do not write to them directly). This eliminates race conditions but creates a serialization point. Every status update requires:

1. Read the file
2. Parse YAML/JSON (via shell script or orchestrator logic)
3. Modify the in-memory representation
4. Write the entire file back

For `tasks.yaml` with 10-20 tasks, this is perhaps 2-5 KB of I/O per update. The frequency of updates is low (task claim, task completion, status change -- perhaps 20-40 writes per wave cycle). This is not an I/O bottleneck in any meaningful sense.

However, the parsing overhead is worth noting. If the orchestrator parses YAML via shell scripts using `yq` or similar, each parse operation forks a process and reads the file. This is fine for small files but would degrade if tasks.yaml grew to hundreds of tasks (unlikely in normal use but possible in a large planning session).

**EFFICIENT** for the expected scale of operation. Would become a concern only at scales far beyond the plan's intended use.

### 9. Behavior Under Resource Pressure

**RISK**

The plan does not adequately address degraded operation under resource pressure.

**Low memory**: If the system runs low on memory, the OS will start swapping. The `claude` CLI processes (Node.js) are particularly sensitive to swapping because the V8 garbage collector touches large portions of the heap frequently. A swapping Node.js process can slow down by 10-100x. The plan has no mechanism to detect memory pressure and reduce concurrency (e.g., dropping from 4 workers to 2).

**High CPU**: If workers are running CPU-intensive build/test commands (e.g., `go test ./...` compiling a large project), and all 4 workers do this simultaneously, the system will be CPU-bound. The `agent_timeout: 300s` is wall-clock time, but CPU-bound operations will make everything slow. There is no adaptive throttling.

**Slow disk**: On a slow disk, git operations (worktree creation, commits, diffs) will queue up. The plan's heartbeat checks (`kill -0`) will still pass (the process is alive, just slow), so the orchestrator will not detect the problem. Workers may time out due to slow I/O, leading to spurious failures.

**What is missing**:
- System resource monitoring (check available RAM before spawning another agent)
- Adaptive concurrency (reduce parallelism when resources are tight)
- I/O wait detection
- Graceful degradation (run fewer agents rather than failing)

### 10. Heartbeat/Monitoring Overhead

**EFFICIENT**

The heartbeat mechanism is `kill -0 <pid>` run every `heartbeat_interval` (default 30s). This is a system call that checks process existence without sending a signal. Its overhead is effectively zero -- it takes microseconds and no I/O.

The orchestrator runs this check for each active agent (up to 4) every 30 seconds. That is 4 system calls every 30 seconds, which is negligible by any measure.

However, the orchestrator must invoke this via the Bash tool (since it is a Claude Code skill, not a native process). Each Bash tool invocation has Claude Code overhead (tool dispatch, output capture). This means the "free" `kill -0` check actually costs a small amount of orchestrator context tokens and latency. Over a 30-minute session with checks every 30 seconds, that is 60 heartbeat rounds x 4 agents = 240 Bash tool calls just for liveness checks. The token cost of these tool calls is non-trivial.

**SUGGESTION**: Batch all heartbeat checks into a single Bash call (e.g., `kill -0 $PID1 && kill -0 $PID2 && ...`) or write a `lifecycle-manage.sh check-all` command that checks all registered agents in one invocation. This would reduce 240 tool calls to 60.

### 11. Resource Leaks

**RISK**

The plan has good coverage of cleanup for normal operation and crash recovery, but several leak vectors are not addressed:

**Worktrees after crash**: The graceful shutdown section says "Do NOT clean worktrees (preserve for debugging)." The startup cleanup (`blueflame-init.sh`) detects stale agents and cleans locks, but it is ambiguous about whether it cleans stale worktrees. If worktrees are preserved across crashes for debugging, and the user forgets to clean them manually, they accumulate.

**Audit logs**: `.blueflame/logs/<agent-id>.audit.jsonl` files are created per agent per session. With unique agent IDs per session, old log files never get overwritten. No retention policy is specified.

**Generated hook scripts**: `.blueflame/hooks/<agent-id>/watcher.sh` files are generated per agent. Old ones are never mentioned as being cleaned up.

**Lock files after unclean shutdown**: The plan handles lock files via PID liveness checking (if the PID is dead, the lock is stale). This works well on a single machine but could leave orphaned lock files if PIDs wrap around and a new, unrelated process gets the same PID. This is unlikely but possible on long-running systems.

**Beads storage**: The plan archives to Beads after each wave cycle. Beads uses git-backed storage. Over many sessions, this grows without bound. The "memory decay" feature summarizes old entries but does not delete them. This is not a leak per se, but it is unbounded growth.

**Temp files**: The plan does not mention temporary files. Shell scripts that parse YAML or generate hooks may create temp files. If a script is interrupted mid-execution, temp files may be left behind.

### 12. Handling of "Lower Hardware Specs"

**RISK**

The plan states in its opening line that it is "optimized for low hardware specs," but the concrete accommodations are limited:

- Concurrency is configurable (can set `development: 1` for low-spec machines)
- Token budgets prevent runaway consumption (cost optimization, not hardware optimization)
- Shell scripts avoid compilation overhead (marginal benefit)
- Validators use the cheapest model (cost optimization, not hardware optimization)

What is NOT provided:
- A "low-spec" configuration profile or preset
- Minimum hardware requirements documentation
- Pre-flight system resource checks (available RAM, disk space, CPU cores)
- Automatic concurrency adjustment based on available resources
- Warnings when configured concurrency exceeds what the hardware can handle
- Memory-efficient alternatives for large repositories (sparse checkout, shallow clone)

The plan's default configuration (`development: 4`, `max_memory_mb: 512`) is tuned for a reasonably capable machine (16 GB RAM, 8 cores, SSD). A user with a laptop (8 GB RAM, 4 cores) would need to know to reduce concurrency to 1-2 workers, but the plan does not guide them to do so.

The claim of optimization for "low hardware specs" is more accurately described as "configurable for different hardware specs." True optimization for low specs would include adaptive behavior and resource-aware defaults.

### 13. Network Isolation Overhead

**EFFICIENT** (Linux) / **CONCERN** (macOS)

The plan uses two mechanisms for network isolation:
- `unshare --net` on Linux
- `sandbox-exec` on macOS

**Linux (`unshare --net`)**: This creates a new network namespace for the agent process. The overhead is minimal -- it is a single system call at process creation time. The process runs in its own network namespace with no interfaces, so all network calls fail immediately. There is no ongoing overhead. This is the ideal approach.

**macOS (`sandbox-exec`)**: This is a deprecated API (deprecated since macOS 10.15 Catalina, removed in some configurations in later releases). On macOS Sequoia (which the reviewer's system is running, based on Darwin 25.2.0), `sandbox-exec` may not be available or may behave unexpectedly. Even when available, the sandbox profile must be carefully crafted. The overhead of sandbox-exec is small (profile is evaluated at process startup) but non-zero.

The plan does not discuss:
- What happens if `sandbox-exec` is unavailable on the target macOS version
- Fallback mechanisms if network isolation cannot be established
- Whether the agent should refuse to start if network isolation fails
- Alternative macOS network isolation approaches (e.g., application firewall rules, custom network extensions)

Given that the development environment appears to be macOS (Darwin 25.2.0), this is a practical concern, not a theoretical one.

### 14. Worktree Creation Time and Concurrency Startup

**CONCERN**

Creating a git worktree involves:
1. `git worktree add` (creates the worktree directory, checks out files)
2. The checkout step copies/hardlinks files from the repository to the worktree

For a repository with 10,000 files, worktree creation takes 1-3 seconds on SSD. For 100,000 files, it may take 10-30 seconds. The plan creates worktrees sequentially (one per worker, before spawning the worker).

With 4 workers, worktree creation adds 4-120 seconds of serial startup time before any work begins. This is not a hardware resource concern per se, but it is a latency concern that compounds on slower disks.

The plan could create worktrees in parallel to reduce startup time, but this would increase peak disk I/O.

### 15. Peak Resource Consumption Profile

**CONCERN**

The worst-case resource consumption occurs at the start of Wave 2, when all workers have just been spawned:

| Resource | Consumption |
|----------|-------------|
| Processes | 1 orchestrator + 4 workers + 4 watcher hooks running = 9+ processes |
| RAM | ~400 MB (orchestrator) + 4 x ~400 MB (workers) + OS + git = ~2.5-3 GB |
| Disk | 4 worktrees + logs + state files |
| File descriptors | ~100 per Node.js process x 5 = ~500 system-wide |
| CPU | Bursty (high during tool execution, low during API wait) |

If all 4 workers simultaneously invoke build/test commands (e.g., `go test ./...`), peak CPU and memory spike dramatically because each `go test` invocation compiles and runs tests, consuming additional RAM and CPU cores.

The plan does not model or constrain this compound resource usage. The per-agent limits do not account for the aggregate.

---

## Summary of Findings

| # | Finding | Category | Severity |
|---|---------|----------|----------|
| 1 | 4 concurrent claude CLI processes need 2-3 GB RAM minimum | RISK | High |
| 2 | Git worktrees duplicate working trees; large repos need significant disk | CONCERN | Medium |
| 3 | Audit logs and state files grow without retention policy | CONCERN | Low |
| 4 | ulimit -v 512MB will crash Node.js processes; open files limit too tight | RISK | Critical |
| 5 | Orchestrator context grows unboundedly through wave cycle | CONCERN | Medium |
| 6 | Shell scripts are genuinely lighter than a persistent Go binary | EFFICIENT | N/A |
| 7 | Concurrent git operations contend on shared object database | CONCERN | Medium |
| 8 | YAML state file I/O is negligible at expected scale | EFFICIENT | N/A |
| 9 | No adaptive behavior under resource pressure | RISK | High |
| 10 | Heartbeat mechanism itself is free but tool-call overhead adds up | EFFICIENT | N/A |
| 11 | Worktrees, logs, hooks, and temp files can leak over time | RISK | Medium |
| 12 | "Low hardware specs" claim is not backed by adaptive defaults | RISK | Medium |
| 13 | macOS sandbox-exec is deprecated; may not work on modern macOS | CONCERN | High |
| 14 | Sequential worktree creation adds startup latency | CONCERN | Low |
| 15 | Peak aggregate resource usage is not modeled or constrained | CONCERN | High |

---

## Recommendations

1. **Fix the ulimit -v setting**: 512 MB virtual memory will not work with Node.js. Either raise the limit to 4 GB (which makes it largely symbolic), use cgroups v2 on Linux for RSS limiting, or remove the memory ulimit and rely on the agent timeout as the primary constraint. This is the single most critical issue.

2. **Add pre-flight resource checks**: Before spawning agents, check available RAM and disk space. Warn or reduce concurrency if resources are tight. A simple `free -m` / `vm_stat` and `df -h` check in `blueflame-init.sh` would suffice.

3. **Add adaptive concurrency**: If available RAM < 8 GB, default to `development: 2`. If < 4 GB, default to `development: 1`. Document minimum requirements clearly.

4. **Raise max_open_files to 1024**: 256 is too low for Node.js processes that also run build tools. The default system limit (typically 1024 or 4096) is fine.

5. **Add disk space checks before worktree creation**: Estimate worktree size from the current repository and check available disk before creating worktrees.

6. **Add a resource leak cleanup command**: A `blueflame cleanup` command that removes old worktrees, logs older than N days, stale hook scripts, and orphaned lock files.

7. **Batch heartbeat checks**: Combine all agent liveness checks into a single shell script invocation to reduce orchestrator tool-call overhead.

8. **Address macOS sandbox-exec deprecation**: Investigate alternatives for network isolation on modern macOS, or document that network isolation is best-effort on macOS and mandatory only on Linux.

9. **Document minimum hardware requirements**: Add a section to the plan specifying minimum and recommended hardware for different concurrency levels.

10. **Consider sparse checkout for worktrees**: For large repositories, create worktrees with sparse checkout that only includes the files relevant to the task's `file_locks`. This would dramatically reduce worktree disk consumption.
