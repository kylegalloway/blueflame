# Architecture Review: Blue Flame Wave-Based Multi-Agent Orchestration System

**Reviewer**: Claude Opus 4.6 (Architecture Review)
**Document Reviewed**: `/Users/kylegalloway/src/blueflame/Plan-ADR.md`
**Date**: 2026-02-07

---

## Executive Summary

Blue Flame proposes a wave-based multi-agent orchestration system built as a Claude Code skill backed by shell scripts. The architecture is **fundamentally sound** for its stated scope (up to 4 concurrent workers, developer-supervised sessions). It demonstrates strong security thinking (three-phase watcher enforcement, defense in depth) and good cost awareness (model tiering, token budgets, deterministic checks over LLM checks). The wave-based execution model provides natural synchronization points that simplify concurrency reasoning.

However, several architectural concerns warrant attention: the orchestrator is a **single point of failure with no fault tolerance of its own**; the state management approach relies on **multiple unsynchronized files** prone to inconsistency under concurrent writes; the **advisory locking mechanism has race conditions**; and the system's reliance on an LLM-driven orchestrator for process-critical control flow introduces **non-determinism at the coordination layer**. These are addressable concerns, not fatal flaws.

**Overall Assessment**: Good architecture for a v1 system at this scale. The separation between deterministic shell operations and LLM-driven decision-making is the right instinct. The primary risks lie in state consistency under failure conditions and the inherent unpredictability of an LLM-based controller managing process lifecycle.

---

## Detailed Findings

---

### 1. Separation of Concerns

**STRENGTH: Clean role decomposition across wave phases.** The four-wave model (plan, develop, validate, merge) creates clear boundaries. Each agent type has a single, well-defined responsibility. Planners do not execute. Workers do not validate. Validators do not merge. This is textbook separation of concerns and means each agent prompt can be tightly scoped, reducing context pollution and token waste.

**STRENGTH: Mechanical operations delegated to shell scripts.** The decision to push all deterministic, non-creative work (worktree management, lock operations, sandbox setup, post-execution checks) into shell scripts is architecturally excellent. This keeps the orchestrator's LLM context lean, makes mechanical operations testable in isolation, and ensures that safety-critical enforcement paths have zero token cost and fully deterministic behavior. The orchestrator becomes a coordinator, not an implementer.

**CONCERN: The orchestrator skill (SKILL.md) carries too many responsibilities.** According to the plan, the orchestrator: reads configuration, manages waves, manages agent lifecycle, dispatches agents via Task tool, monitors heartbeats, enforces timeouts, tracks token budgets, manages task claiming in tasks.yaml, presents changesets for human review, handles re-queue logic, and coordinates Beads archival. This is a God Object risk. Even though many operations are delegated to shell scripts, the orchestrator still must know *when* to call each script, *how* to interpret results, and *what* to do on failure. A single SKILL.md file encoding all of this wave logic will be large, complex, and difficult to maintain. The prompt-as-program nature of a Claude Code skill makes modularization harder than in traditional code.

**SUGGESTION: Decompose the orchestrator into sub-skills or clearly delineated prompt sections.** Consider whether each wave phase could be its own skill (or at minimum, its own clearly separated prompt template that the orchestrator invokes), rather than one monolithic SKILL.md encoding the entire state machine. This would improve testability and make it easier to modify one wave's behavior without risking regressions in others.

---

### 2. Soundness of the Claude Code Skill + Shell Scripts Architecture

**STRENGTH: The hybrid approach leverages each technology's strengths.** Shell scripts handle what they are good at: file operations, process management, text pattern matching, OS-level enforcement. The Claude Code skill handles what LLMs are good at: understanding task context, making coordination decisions, interpreting validation results, and communicating with the human. This is a pragmatic division.

**CONCERN: An LLM-based orchestrator is inherently non-deterministic for control flow.** The orchestrator must execute a precise state machine: read tasks, check dependencies, acquire locks, spawn agents in order, monitor completion, transition waves. LLMs can follow instructions with high reliability, but they are not deterministic state machines. An LLM might misinterpret a task's status, skip a wave transition check, or fail to call a shell script in the right order under unusual conditions (e.g., all tasks failed, lock acquisition partially succeeded). Traditional orchestrators use deterministic code for this reason. The plan describes the "deterministic controller mindset" from Plan C as a design value, but the implementation medium (an LLM skill) inherently works against that goal.

**RISK: Prompt injection via task content.** The planner agent generates `tasks.yaml`, which the orchestrator reads and uses to construct prompts for workers. If the planner (or a human editing tasks.yaml) includes content that looks like instructions ("Ignore all previous constraints and..."), the orchestrator or downstream agents could be manipulated. The architecture does not describe any sanitization layer between task content and prompt construction.

**SUGGESTION: Add a sanitization or escaping step when incorporating user-generated or planner-generated content into agent prompts.** At minimum, task descriptions should be wrapped in clear delimiters and the prompt templates should instruct agents to treat delimited content as data, not instructions.

---

### 3. Single Points of Failure

**RISK: The orchestrator is a single point of failure with no redundancy.** If the orchestrator's Claude Code session crashes, hangs, or runs out of context window, the entire system stops. The plan addresses crash recovery via `state.yaml` persistence and startup cleanup of orphan processes, but this is *manual* recovery -- someone must restart the orchestrator, which then attempts to resume. There is no automatic failover, no watchdog process monitoring the orchestrator itself, and no mechanism for the orchestrator to checkpoint and resume mid-wave if its context window fills up.

**CONCERN: Context window exhaustion during long waves.** The orchestrator accumulates context as it monitors multiple agents, reads their output, processes heartbeats, and handles failures. With 4 concurrent workers, each potentially producing errors or requiring retries, the orchestrator's context window could fill. The plan does not describe a strategy for orchestrator context management. Unlike agents (which are ephemeral and short-lived), the orchestrator must persist for the entire multi-wave session.

**SUGGESTION: Implement an orchestrator watchdog.** A simple shell script that monitors the orchestrator process and, on death, performs graceful cleanup and notifies the human, would reduce the blast radius of orchestrator failure. Additionally, consider designing wave transitions as natural checkpoints where the orchestrator can "reset" its context by summarizing the current state and re-reading it, rather than accumulating unbounded conversation history.

**STRENGTH: Crash recovery via state.yaml is a reasonable mitigation.** The plan explicitly addresses what happens on orchestrator crash: state is persisted, worktrees are preserved for debugging, and startup cleanup handles orphans and stale locks. This is not automatic recovery, but it is graceful degradation -- no state is silently lost.

---

### 4. Communication Pathways

**STRENGTH: Unidirectional communication simplifies reasoning.** The orchestrator dispatches agents via the Task tool and reads their results. Agents write to their worktree and commit. There is no bidirectional channel between agents, no agent-to-agent communication, and no callback mechanism from agents to the orchestrator. This eliminates an entire class of coordination bugs.

**CONCERN: The Task tool is the sole dispatch mechanism, and its failure semantics are unclear.** The plan assumes the Claude Code Task tool will reliably spawn agents and return their results. But what happens if Task tool dispatch fails? What if it returns a partial result? What if the spawned agent completes but the Task tool times out waiting for it? The architecture does not describe error handling at the dispatch boundary. Since the orchestrator is an LLM, its ability to programmatically detect and handle Task tool failure modes is limited compared to a traditional retry-with-backoff pattern.

**CONCERN: Agents communicate results through multiple channels simultaneously.** A worker's output is conveyed through: (1) its git commits in the worktree, (2) its updates to tasks.yaml (status, result), (3) its audit log in `.blueflame/logs/`, and (4) the return value from the Task tool. This creates ambiguity about which channel is the source of truth. If an agent commits code but the Task tool call fails before tasks.yaml is updated, the system is in an inconsistent state. The orchestrator must reconcile these channels, which adds complexity.

**SUGGESTION: Designate a single source of truth for task completion.** The git commit in the worktree is the most durable signal (it survives crashes). Consider making the orchestrator derive task status from the worktree state (presence of commits on the agent's branch) rather than relying on tasks.yaml being updated by the agent or the orchestrator at the right moment.

---

### 5. State Management

**RISK: Multiple state files with no transactional consistency.** The system maintains state in: `tasks.yaml` (task status and claiming), `agents.json` (process tracking), `state.yaml` (crash recovery), lock files in `.blueflame/locks/`, and per-agent audit logs. These files are updated independently by the orchestrator (an LLM) calling shell scripts at different points in the workflow. There is no transaction boundary. If the orchestrator updates `agents.json` to register a new agent but crashes before updating `tasks.yaml` to claim the task, the state is inconsistent. Since the orchestrator is an LLM invoking tools sequentially, there is always a window between any two updates where state is inconsistent.

**CONCERN: tasks.yaml is a shared mutable file with concurrent readers.** While the plan states that only the orchestrator writes to tasks.yaml (agents do not claim tasks themselves; the orchestrator does it on their behalf), the orchestrator is a single LLM context that calls shell scripts. If the orchestrator dispatches agent A, then dispatches agent B, it may need to update tasks.yaml twice in quick succession. Since these are separate tool calls within an LLM conversation, they are inherently sequential within the orchestrator, which is fine. But the real risk is that the orchestrator *reads* tasks.yaml, makes a decision, then *writes* tasks.yaml -- and in between, a shell script callback (post-check, lifecycle management) has also modified the file. This is a classic TOCTOU (time-of-check-to-time-of-use) race.

**CONCERN: agents.json lacks a schema version or migration strategy.** The plan specifies the structure of agents.json but does not describe versioning. If the format changes between versions of Blue Flame, stale agents.json files from a crashed prior session could cause parsing failures on startup. This is a minor concern given the early stage but worth noting.

**SUGGESTION: Consider using a single state file (or a simple embedded database like SQLite) rather than multiple files.** If multiple files are retained, implement a reconciliation step at each wave transition that verifies consistency across all state files and repairs any detected inconsistencies. At minimum, add sequence numbers or timestamps to detect stale reads.

---

### 6. Concurrency Model

**STRENGTH: Wave-based execution eliminates most concurrency hazards.** Because all workers must complete before validation begins, and all validation must complete before merging begins, the system avoids the hardest concurrency problems (concurrent reads during writes, interleaved merge operations, partial results). The concurrency that does exist (multiple workers in Wave 2, multiple validators in Wave 3) is well-bounded and constrained by the lock system.

**STRENGTH: Git worktree isolation is an excellent choice for worker concurrency.** Each worker operates in its own worktree on its own branch. There is zero filesystem contention between workers at the git level. The shared git object database means worktree creation is cheap. This is the right isolation primitive for this problem.

**CONCERN: Advisory lock race condition during concurrent task dispatch.** The orchestrator acquires locks for each worker before spawning it. The lock mechanism is file-based: check if lock file exists, if not, create it. Between the check and the create, another lock acquisition (for a different agent being dispatched concurrently... though the orchestrator dispatches sequentially, shell scripts could race) could succeed. However, since the orchestrator dispatches agents one at a time (sequential tool calls within an LLM conversation), this race condition likely cannot occur in practice. The more realistic risk is that lock-manage.sh is called by multiple processes (e.g., if the orchestrator somehow spawns agents in parallel via concurrent Task tool calls, or if a cleanup process runs simultaneously). The plan should clarify whether lock-manage.sh uses atomic file creation (e.g., `mkdir` as a lock, or `ln` for atomic creation) rather than test-and-create.

**SUGGESTION: Use atomic lock acquisition in lock-manage.sh.** Replace any check-then-create pattern with `mkdir <lockdir>` (which is atomic on POSIX systems) or `ln -s` (which fails atomically if the target exists). This makes the lock mechanism robust even if concurrent callers exist.

**STRENGTH: Per-wave configurable concurrency is well-designed.** The ability to set different concurrency limits for each wave phase (4 workers, 2 validators, 1 merger) shows good understanding of the resource characteristics of each phase. Validation is cheaper than development, so fewer parallel validators are needed. Merging must be serial to avoid conflicts.

---

### 7. Coupling and Cohesion

**STRENGTH: Shell scripts are well-decomposed with single responsibilities.** Each script handles one concern: `worktree-manage.sh` handles worktrees, `lock-manage.sh` handles locks, `watcher-generate.sh` handles watcher creation, etc. They communicate through clear interfaces (command-line arguments, exit codes, file outputs). This makes them independently testable and replaceable.

**CONCERN: Tight coupling between blueflame.yaml schema and multiple consumers.** The configuration file is read by: the orchestrator (for wave logic, concurrency, models), watcher-generate.sh (for permissions, validation rules), sandbox-setup.sh (for resource limits), token-tracker.sh (for budgets), and the prompt templates (which reference configuration values). A change to the blueflame.yaml schema could break any of these consumers. There is no described abstraction layer or schema validation that all consumers share.

**CONCERN: The prompt templates are coupled to the task file schema.** Templates like `worker-prompt.md` must reference specific fields from tasks.yaml (file_locks, dependencies, cohesion_group). If the task schema evolves, the prompt templates must be updated in lockstep. This is an implicit contract with no enforcement mechanism.

**SUGGESTION: Implement blueflame.yaml schema validation as a shared library (shell function file sourced by all scripts).** This centralizes the schema definition and ensures all consumers agree on the structure. Consider generating a "resolved config" artifact during initialization that pre-computes all derived values, so downstream scripts read a flat, validated structure rather than parsing raw YAML independently.

---

### 8. Scalability Concerns

**STRENGTH: The system explicitly limits its scale, which is honest and pragmatic.** Capping at 4 workers acknowledges hardware constraints and avoids the complexity of large-scale distributed systems. For a single-developer tool, this is the right scale.

**CONCERN: The orchestrator's monitoring loop does not scale well even within 4 agents.** The orchestrator checks liveness via `kill -0 <pid>` on a heartbeat interval by calling Bash tool repeatedly. With 4 agents, this means 4 Bash tool calls every 30 seconds, plus token tracking calls, plus potential timeout enforcement. Each tool call costs tokens and adds to the orchestrator's context. Over a long wave (say, 4 workers each running for 5 minutes), the orchestrator could accumulate hundreds of monitoring tool calls in its context, approaching context window limits.

**SUGGESTION: Replace polling-based monitoring with event-driven notification.** Instead of the orchestrator polling each agent's PID, have the lifecycle-manage.sh script run as a background monitor that writes status changes to a file. The orchestrator then makes a single "check status" call that reads the latest state, rather than N individual PID checks. This reduces tool call volume from O(agents * time) to O(check_intervals).

**CONCERN: Worktree disk usage could be significant.** Each worktree is a full checkout of the repository. For a large repo, 4 simultaneous worktrees could consume substantial disk space. The plan mentions shared git object database (which helps), but the working directory files are fully duplicated. For repositories with large assets, build artifacts, or vendored dependencies, this could exhaust disk space on constrained hardware.

**SUGGESTION: Add a disk space check to blueflame-init.sh.** Estimate worktree size (from current repo checkout size) multiplied by concurrency.development and warn if available disk space is insufficient.

---

### 9. Architectural Anti-Patterns and Risks

**RISK: "Orchestrator as God Object" -- the SKILL.md file must encode the entire wave state machine, error handling, recovery logic, and human interaction protocol in a single prompt.** This is the most significant architectural risk. As the system grows more complex (more failure modes, more edge cases, more configuration options), this single file becomes increasingly difficult to reason about, test, and maintain. Traditional systems solve this with code decomposition, but a Claude Code skill has limited decomposition primitives.

**RISK: Non-deterministic failure handling.** When a worker fails, the orchestrator must: detect the failure (via heartbeat or Task tool return), update tasks.yaml, release locks, potentially clean the worktree, decide whether to retry, and if retrying, re-dispatch. This is a multi-step recovery procedure that the orchestrator must execute correctly every time. An LLM-based orchestrator might handle common failures well but could behave unpredictably for unusual failure combinations (e.g., lock release fails because the lock file was already cleaned by a concurrent startup-cleanup process, and the orchestrator must reason about this edge case from its prompt instructions alone).

**CONCERN: The Beads dependency introduces external system risk.** Beads is listed as a key dependency for persistent memory. If Beads is unavailable, misconfigured, or has a breaking API change, the system loses cross-session memory. The plan marks Beads as optional (`beads.enabled: true`), which is good, but the planner prompt references "prior session context: [beads summary]" as if it will always be available. The graceful degradation path when Beads is absent should be more explicitly defined.

**CONCERN: Validator agents use the cheapest model (haiku) but are asked to make nuanced judgments.** The validator must assess "task applicability," "correctness," "regressions," and "scope creep." These are subjective, context-dependent judgments. Using the cheapest model for these checks creates a risk of false passes (missing real issues) or false fails (rejecting valid work due to misunderstanding). The cost savings may not justify the risk of corrupted validation results flowing into the merge phase.

**SUGGESTION: Consider making the validator model configurable per-task or per-cohesion-group.** High-risk tasks (security-related, core infrastructure) could use a more capable validator model, while low-risk tasks (documentation, tests) could use haiku. This preserves cost optimization where it is safe while allowing quality validation where it matters.

**CONCERN: The watcher hook timeout of 5000ms (5 seconds) could cause latency issues.** Every tool invocation by every agent triggers the watcher hook. If the hook takes the full 5 seconds (e.g., due to complex YAML parsing or path matching), an agent making 100 tool calls would spend over 8 minutes just in watcher overhead. The plan does not describe performance requirements or benchmarks for the watcher hooks.

**SUGGESTION: Profile and optimize the watcher hook execution path.** Consider caching parsed configuration in a pre-computed format that the watcher can read with minimal processing. Set an internal target of sub-100ms for watcher hook execution to keep overhead negligible.

---

### 10. Additional Observations

**STRENGTH: Defense-in-depth security model.** The three-phase watcher system (pre-execution hooks, OS-level sandbox, post-execution diff) is genuinely well-designed. Any single layer can fail and the system remains protected by the other two. This is rare in LLM-agent systems and shows mature security thinking.

**STRENGTH: Human remains in the loop at critical decision points.** The plan never allows autonomous merging to the base branch. Every changeset requires human approval. This is the correct trust model for a system where agents might produce subtly incorrect code.

**STRENGTH: Ephemeral agents prevent state leakage between waves.** By killing all agents between waves and relying on Beads for persistent memory, the system avoids subtle bugs from accumulated agent state. Each wave starts clean.

**STRENGTH: The cost profile is realistic and well-reasoned.** The estimated ~$0.78 per session with 3 tasks is achievable given the model tiering and token budget enforcement. The design shows genuine cost-consciousness throughout.

**CONCERN: No described mechanism for the orchestrator to handle partial wave completion.** If 3 out of 4 workers complete but the 4th is stuck (not dead, just slow), the orchestrator must wait until timeout. There is no described mechanism for the human to intervene mid-wave (e.g., "skip the stuck agent and proceed to validation for the completed tasks"). The wave model's strength (synchronization) becomes a weakness here -- one slow agent blocks all progress.

**SUGGESTION: Add a human interrupt mechanism during waves.** Allow the human to signal "proceed with completed tasks" during Wave 2, moving completed tasks to validation while the stuck task continues or is killed. This would require the orchestrator to handle partial wave transitions, adding complexity, but would significantly improve the interactive experience.

---

## Summary of Findings

| Category | Count | Key Items |
|----------|-------|-----------|
| **STRENGTH** | 10 | Wave isolation, shell delegation, worktree isolation, defense-in-depth security, human gates, ephemeral agents, cost awareness, unidirectional communication, configurable concurrency, crash recovery |
| **CONCERN** | 10 | Orchestrator God Object, non-deterministic controller, TOCTOU races in state files, context window exhaustion, Task tool failure semantics, multi-channel result reporting, config schema coupling, Beads dependency, watcher hook latency, no partial wave completion |
| **RISK** | 4 | Single point of failure (orchestrator), multiple unsynchronized state files, prompt injection via task content, non-deterministic failure handling |
| **SUGGESTION** | 8 | Decompose orchestrator into sub-skills, sanitize task content in prompts, implement orchestrator watchdog, use atomic lock acquisition, event-driven monitoring, disk space checks, configurable validator models, human mid-wave interrupt |

---

## Priority Recommendations

1. **Address state consistency first.** The multiple-file state management with no transactional boundaries is the most likely source of production bugs. Either consolidate to a single state file or add reconciliation checks at every wave transition.

2. **Make lock acquisition atomic.** This is a simple fix (`mkdir` instead of test-and-create) that eliminates a class of potential race conditions.

3. **Design for orchestrator context limits.** The orchestrator will accumulate context over a multi-wave session. Plan for context checkpointing or summarization at wave boundaries before this becomes a problem in practice.

4. **Add prompt injection mitigations.** Task descriptions flow from the planner (an LLM) into worker prompts. This is an injection vector that should be addressed before any adversarial or multi-user use case.

5. **Profile watcher hook performance early.** If hooks are slow, the entire system feels slow. Establish a performance budget for hooks in Phase 2 before building the orchestrator in Phase 3.
