# Wave Handling Review: Blue Flame Orchestration System

**Reviewer**: Claude Opus 4.6 (automated review)
**Date**: 2026-02-07
**Document reviewed**: `/Users/kylegalloway/src/blueflame/Plan-ADR.md`

---

## Per-Wave Summary Assessment

| Wave | Overall | Key Concern |
|------|---------|-------------|
| Wave 1 (Planning) | Solid | Planner constraint enforcement is implicit, not explicit |
| Wave 2 (Development) | Strong | Dependency handling within a wave needs more detail |
| Wave 3 (Validation) | Good with gaps | Validator failure/timeout handling is underspecified |
| Wave 4 (Merge) | Good with risks | Merge conflict resolution semantics are vague |
| Transitions | Adequate | Partial-failure at wave boundaries needs tighter specification |

---

## Wave 1: Planning

### 1.1 Planner Agent Scoping and Constraints

**SOUND**: The planner is always a single instance (`concurrency.planning: 1`), which eliminates coordination complexity. The planner receives a focused prompt with a clear directive: decompose into tasks with file boundaries, dependencies, and cohesion groups. The use of Superpowers `/brainstorm` and `/write-plan` skills gives the planner structured thinking tools rather than letting it free-form reason.

**GAP**: The planner agent is spawned via the Task tool, but the document does not specify whether the planner runs under the same watcher enforcement model as workers. The planner needs to produce `tasks.yaml`, which means it needs write access to that file. However, `tasks.yaml` is not listed in `allowed_paths` (which covers `src/**`, `tests/**`, `docs/**`). Either the planner operates outside the watcher system entirely, or there is a missing path allowance. The document should explicitly state whether the planner has watchers and what its permission scope is.

**GAP**: The planner has a token budget of 50,000 tokens, but there is no specification for what happens if the planner exceeds this budget mid-plan-generation. For workers, the behavior is clear (task marked `failed`). For the planner, a budget-exceeded failure would leave the system with no plan at all. The recovery path for a failed planner is not documented.

**SOUND**: The planner receives Beads context (prior failure notes, patterns, cost history, re-queued task context). This is well-designed because it gives the planner the information it needs to avoid repeating past mistakes, and the memory decay feature prevents this context from growing unboundedly.

### 1.2 Human Approval Gate

**SOUND**: The plan approval gate is well-defined with three options: approve, edit, reject. The presentation format includes task IDs, cohesion groups, locks, dependencies, and estimated cost. This gives the human enough information to make an informed decision.

**GAP**: The "edit" option is listed but the mechanics of editing are not specified. Can the human edit `tasks.yaml` directly? Does the planner re-run with the edits as constraints? Is there a structured editor or is it raw YAML editing? This is an important UX detail because malformed edits to the YAML could break downstream processing.

**GAP**: The "reject" option is listed but the consequences are not specified. Does rejection terminate the session entirely? Does it re-run the planner with additional guidance? The loop says "Loop until approved" but does not describe what the planner receives on rejection. If the planner is a fresh Task tool invocation each time, it has no memory of the prior rejection unless the orchestrator explicitly passes that context.

### 1.3 Plan-Edit-Reject Loop

**RISK**: The "loop until approved" design has no escape hatch specified. If the planner keeps producing unsatisfactory plans, the human is stuck in a loop. There should be an explicit "abort session" option alongside approve/edit/reject.

**SUGGESTION**: The rejection loop should specify how many re-planning attempts are allowed before the system suggests the human write the task plan manually. After 2-3 rejections, the planner is likely stuck and burning tokens.

### 1.4 Prior Session Context (Beads)

**SOUND**: The Beads integration is well-designed for the planner. The `beads-archive.sh load` step explicitly provides failure notes, patterns, cost history, and re-queued task context. The memory decay feature prevents unbounded context growth.

**GAP**: The document does not specify how re-queued tasks from a prior wave cycle interact with the planner. If Wave 4 re-queues task-002, the document says "repeat from Wave 2" -- meaning the planner is NOT re-invoked. But what if the re-queued task needs to be decomposed differently based on the rejection feedback? The re-queue path skips Wave 1 entirely, which means the plan cannot be adjusted. The only mitigation is the `history` array on the task, but the worker receiving that task has no authority to restructure the plan.

---

## Wave 2: Development

### 2.1 Worker Spawning, Task Claiming, and Isolation

**SOUND**: The worker spawning sequence is thorough and well-ordered:
1. Generate unique agent ID
2. Create git worktree (filesystem isolation)
3. Acquire advisory file locks (coordination)
4. Configure OS-level sandbox (hard resource limits)
5. Generate per-agent watcher hooks (runtime enforcement)
6. Spawn worker via Task tool
7. Claim task in `tasks.yaml`
8. Register in process group tracking

Each step has a dedicated shell script, which keeps the orchestrator lean and makes each step independently testable.

**SOUND**: Git worktree isolation is an excellent choice. Each worker gets its own filesystem view of the repo, branches are per-task, and the shared git object database avoids full-clone overhead.

**GAP**: Step 2f spawns the worker and step 2g writes the agent ID to `tasks.yaml` (claiming the task). This ordering creates a window where the worker is running but the task is not yet claimed. If the orchestrator crashes between these steps, the task is unclaimed but a worker is running against it. The claim should happen BEFORE the worker is spawned, not after.

**SUGGESTION**: Reverse steps 2f and 2g. Claim the task in `tasks.yaml` first, then spawn the worker. If the spawn fails, release the claim. This eliminates the unclaimed-but-running race condition.

### 2.2 Concurrent Worker Limits

**SOUND**: Concurrency is configurable per-wave via `concurrency.development` (max 4). The document specifies that the orchestrator reads `tasks.yaml`, identifies ready tasks (no unmet dependencies), and spawns up to the concurrency limit. This is straightforward and correct.

**SOUND**: Lock conflicts between ready tasks cause conflicting tasks to run sequentially rather than failing. Tasks are sorted by priority first, which ensures the most important work runs earliest.

**GAP**: The document does not specify how the orchestrator handles the case where there are more ready tasks than the concurrency limit AND some of those tasks have lock conflicts. For example, with concurrency=4 and 6 ready tasks where tasks 1-3 have no conflicts and tasks 4-6 conflict with each other: does the orchestrator pick tasks 1-3 + task 4 (highest priority among conflicting), or does it pick tasks 1-4 ignoring conflicts? The lock acquisition step (2c) would fail for conflicting tasks, but the document does not specify whether the orchestrator pre-checks lock availability before attempting to spawn, or whether it attempts to spawn and handles the failure.

**SUGGESTION**: Add explicit specification for the task selection algorithm. A clear pseudocode block showing: "Sort ready tasks by priority. For each task, check if its file_locks conflict with already-selected tasks. If conflict, defer. Select up to concurrency limit." This eliminates ambiguity.

### 2.3 Monitoring (Heartbeat, Timeout, Token Tracking)

**SOUND**: The monitoring model is comprehensive:
- Heartbeat via `kill -0 <pid>` at configurable intervals (default 30s)
- Timeout enforcement with graceful shutdown (SIGTERM, wait, SIGKILL)
- Token budget tracking with two-stage enforcement (warn at 80%, kill at 100%)
- All monitoring data persisted to `agents.json` and audit logs

**GAP**: The heartbeat check (`kill -0 <pid>`) only verifies that the process exists, not that it is making progress. An agent could be stuck in an infinite loop, consuming CPU and memory, while passing all heartbeat checks. The OS-level `ulimit -t` for CPU time partially mitigates this, but a worker could also be stuck waiting on I/O (e.g., a hung `git` command) which would not consume CPU time.

**SUGGESTION**: Consider adding a "progress heartbeat" in addition to the liveness heartbeat. For example, monitor the last modification time of the agent's audit log. If no new audit log entries appear for N heartbeat intervals, consider the agent stalled.

**GAP**: The token budget tracking is described as "estimates token usage from Claude Code audit logs." The word "estimates" is concerning. If the estimate is inaccurate, a worker could significantly overshoot its budget before being killed, or be killed prematurely. The document should specify the accuracy expectations and the estimation method.

### 2.4 Dependencies Between Tasks Within a Wave

**SOUND**: The task schema includes a `dependencies` field (list of task IDs that must complete first). The orchestrator identifies "ready tasks" as those with no unmet dependencies. This is a correct topological-sort-based approach.

**GAP**: The document does not specify what happens when a dependency fails. If task-002 depends on task-001, and task-001 fails, is task-002 automatically failed? Is it left pending forever? Is the human notified? The only relevant statement is that the orchestrator waits for ALL workers to complete before transitioning to Wave 3, but there is no specification for cascading failure of dependent tasks.

**RISK**: Without cascading failure handling, a failed task could leave all its dependents stuck in `pending` status indefinitely within a wave. If task-001 fails and task-002 depends on it, the orchestrator would wait for all workers to complete, but task-002 was never spawned (its dependency was not met). The wave would technically "complete" (all spawned workers finished), but task-002 would remain pending. The document says "Once ALL workers complete, transition to Wave 3" -- but task-002 is not a worker, it is a pending task. This ambiguity could cause tasks to be silently dropped.

**SUGGESTION**: Add explicit handling: "If a task's dependency fails, the dependent task is marked `blocked` and is not eligible for the current wave. At wave transition, blocked tasks are either re-queued for the next wave cycle (if the failed dependency is retried and succeeds) or failed (if retries are exhausted)."

### 2.5 Worker Failure Mid-Execution

**SOUND**: Worker failure is handled at multiple levels:
- Process death detected via heartbeat -> task marked `failed`, locks released, worktree cleaned
- Timeout -> SIGTERM to process group, wait 5s, SIGKILL, task marked `failed`
- Token budget exceeded -> agent killed, task marked `failed`
- Post-execution check failure -> task marked `failed` with violation details

**SOUND**: Failed tasks are available for retry up to `max_retries` (default 2). The retry mechanism is reasonable.

**GAP**: The document does not specify WHEN retries happen. Are they immediate (within the same Wave 2 run) or deferred (to the next wave cycle)? If immediate, the orchestrator needs logic to re-spawn a worker for the failed task while other workers may still be running. If deferred, the task sits in `failed` status until the wave cycle repeats. The "repeat from Wave 2" path at the end of Wave 4 suggests retries are deferred, but this is not explicitly stated.

**GAP**: The `max_retries` counter location is not specified. The task schema has a `history` array, but no explicit retry counter. The orchestrator would need to count the entries in `history` to determine if retries are exhausted. This should be explicit.

**RISK**: If a worker fails due to a post-execution check violation (Phase 3 watcher), the document says "the agent's changes are discarded." But the agent already committed changes to its worktree branch. "Discarded" presumably means the branch is not considered for validation/merge, but the worktree and branch may still exist. The cleanup sequence for a postcheck failure should be explicit: does the branch get deleted? Does the worktree get cleaned immediately or at the end of the wave?

---

## Wave 3: Validation

### 3.1 Two-Tier Validation Model

**SOUND**: The two-tier model is well-conceived:
- Tier 1 (mechanical): Runs during Wave 2 via watcher hooks and post-execution checks. Zero token cost. Deterministic. Fast.
- Tier 2 (semantic): Runs in Wave 3 via haiku-model validators. Checks task applicability, correctness, regressions, scope creep.

This separation ensures that cheap deterministic checks run first, and expensive LLM-based checks only run on work that already passed mechanical validation.

**SOUND**: Using the cheapest model (haiku) for validators is a good cost optimization. The validator's job is well-scoped: it receives the task description, the diff, and the watcher audit log summary. This is sufficient context for a focused review.

### 3.2 Validator Isolation

**SOUND**: Validators run in the task's worktree, which provides filesystem isolation. Each validator operates independently.

**GAP**: The document does not specify whether validators have the same watcher enforcement as workers. Can a validator modify files? It should not need to -- it only reads the diff and produces a pass/fail verdict. But the document does not explicitly state that validators are read-only. If a validator has write access, it could corrupt the worktree state before merge.

**SUGGESTION**: Explicitly state that validators are read-only agents. Their `allowed_tools` should exclude `Write`, `Edit`, and any mutating Bash commands. They should only need `Read`, `Glob`, `Grep`, and limited `Bash` (for running tests).

**GAP**: The document says validators receive "watcher audit log summary" but does not specify who summarizes the audit log or how. Is this a raw dump of the JSONL? A filtered summary? An LLM-generated summary? If the audit log is large (many tool invocations), passing the raw log would waste validator tokens. If it is summarized, the summarization method needs specification.

### 3.3 Pass/Fail Feedback Loop

**SOUND**: The validator marks the task result as `pass` or `fail` with specific notes. This is clear and actionable.

**GAP**: The feedback loop for failed validations is underspecified. The document says "Failed tasks: human is notified, can retry (re-queue) or skip." But this is only two options. There is no option for the human to provide guidance on what to fix. If a task fails validation, the re-queued version would presumably retry the same approach (since the worker prompt is the same). The human should be able to attach notes to the re-queued task that the next worker will see.

**SUGGESTION**: Add a "retry with notes" option for failed validations. The human's notes get appended to the task's `history` array and are included in the next worker's prompt.

### 3.4 Validator Failure or Timeout

**RISK**: The document does not specify what happens when a validator itself fails or times out. The validator has a token budget (30,000 tokens) and presumably falls under the same timeout enforcement as workers. But the consequences are different: a failed worker means the task was not completed; a failed validator means the task was completed but not reviewed.

**GAP**: If a validator crashes, is the task considered unvalidated (and therefore blocked from merge)? Is the validation retried? Is the task sent back to the human for manual review? None of these are specified.

**SUGGESTION**: Add explicit validator failure handling: "If a validator fails or times out, the task is marked `validation_failed`. The orchestrator retries validation up to `max_retries`. If retries are exhausted, the human is prompted to manually review the diff or skip the task."

**GAP**: The document says validation runs up to `concurrency.validation` at a time, but does not specify the scheduling order. Are tasks validated in priority order? In dependency order? In completion order? If a high-priority task depends on a low-priority task, the dependency's validation might be deferred, blocking the dependent task's progression.

---

## Wave 4: Merge

### 4.1 Merger Scope

**SOUND**: The merger is a single instance (`concurrency.merge: 1`), which eliminates coordination complexity. The merger sees all validated branches and their diffs, grouped by cohesion group. The merger's directive is clear: create clean changesets, resolve cross-task conflicts within groups, and stop-and-report for semantic conflicts it cannot safely resolve.

**SOUND**: The "stop and report rather than guess" directive for semantic conflicts is excellent. An LLM merger that guesses at conflict resolution would be dangerous. Escalating to the human is the right call.

**GAP**: The merger's scope relative to the watcher system is not specified. Does the merger have watchers? The merger needs to create commits on branches, which means it needs write access. But which paths? The merger works across all cohesion groups, so its `file_locks` would need to cover all files from all validated tasks. The permission model for the merger needs explicit specification.

### 4.2 Cohesion Group-Based Merging

**SOUND**: Cohesion groups are a good abstraction for merge ordering. Tasks that logically belong together (e.g., all "auth" tasks) are merged as a single changeset. This produces cleaner git history and more reviewable changesets for the human.

**GAP**: The document does not specify what happens when tasks in different cohesion groups have overlapping file changes. The planner is instructed to minimize `file_locks` overlap, but cohesion groups could still touch the same files. For example, the "auth" group might add middleware and the "api" group might update route documentation that references the middleware. The merger would need to merge these as separate changesets, but if the "api" changeset is rejected, the "auth" changeset might reference files that assume the "api" changes exist.

**RISK**: Cohesion group ordering is not specified. If group A's changeset is approved and group B's changeset depends on group A's changes, the approval order matters. If the human reviews them out of dependency order and rejects group A but approves group B, the result could be an inconsistent codebase. The document does not specify whether changeset presentation order respects inter-group dependencies.

**SUGGESTION**: Add explicit inter-group dependency tracking. If any task in group B depends on a task in group A, present group A's changeset first. If group A is rejected, automatically defer group B rather than presenting it.

### 4.3 Changeset Chaining UX

**SOUND**: The changeset chaining UX is practical and well-designed. The human sees each changeset sequentially with options to approve, reject, view diff, or skip. The summary at the end shows approved/re-queued counts. The session continuation prompt (continue/stop) enables multi-cycle workflows.

**GAP**: The "skip" option is listed in the UX example but its semantics are not defined. Is "skip" different from "reject"? Does a skipped changeset get re-queued? Does it stay in limbo? If "skip" means "defer to next review cycle," this is different from "reject" (which records rejection notes). The distinction needs clarification.

**GAP**: The document does not specify what happens if the human approves changeset 1 but then rejects changeset 2, and changeset 2's code depends on changeset 1's code. Can the human un-approve changeset 1? Is there a "review all, then finalize" mode? The sequential-and-final model means the human cannot reconsider earlier approvals.

### 4.4 Merge Conflicts

**RISK**: The document says the merger "resolves any cross-task conflicts within groups" but does not define what constitutes a resolvable conflict vs. a semantic conflict that requires human intervention. Textual merge conflicts (two tasks modified the same line) are common, but the merger's ability to resolve them depends on understanding the semantic intent of both changes. The boundary between "safe to auto-resolve" and "stop and report" is not specified.

**GAP**: The document does not address merge conflicts between cohesion groups. If group A's changeset is approved and merged to the base branch, and group B's changeset conflicts with the newly-merged state of the base branch, who resolves that? The merger has already completed. Does the orchestrator re-run the merger? Does it fail the group B changeset?

**SUGGESTION**: Specify that changesets are merged to the base branch in presentation order. If a later changeset conflicts with the updated base branch (after earlier changesets were merged), the orchestrator should detect this and either re-run the merger for the conflicting changeset or notify the human.

### 4.5 Re-Queue Logic for Rejected Changesets

**SOUND**: Rejected changesets mark their tasks as `requeued` with rejection notes in the `history` array. This preserves context for the next attempt. The worker on the next wave cycle will see the prior attempt's history.

**SOUND**: The "repeat from Wave 2" loop for re-queued tasks is a reasonable approach. The system does not require re-planning for a single rejected task; it simply retries the same task with additional context.

**GAP**: The re-queue logic does not specify whether the re-queued task's `file_locks` are preserved or can be modified. If the human rejects a changeset because the task modified the wrong files, the next worker is still constrained to the same `file_locks`. The human would need to manually edit `tasks.yaml` to fix this, which is not part of the documented workflow.

**GAP**: The re-queue does not distinguish between "the implementation was wrong" and "the task itself was wrong." If the task description is flawed, re-queuing it will produce the same flawed result. There should be an option to re-queue with an edited task description, not just rejection notes.

**SUGGESTION**: When a human rejects a changeset, offer three options: (1) re-queue as-is with notes, (2) re-queue with edited task description/file_locks, (3) drop the task entirely.

---

## Wave Transitions

### Transition: Wave 1 to Wave 2

**SOUND**: The transition is gated by human approval. The plan must be approved before workers are spawned. This is a clean, unambiguous gate.

### Transition: Wave 2 to Wave 3

**GAP**: The document says "Once ALL workers complete, transition to Wave 3." This is clear but raises the question: what about tasks that were never spawned because their dependencies failed? These tasks are still `pending` but will never become `done` in this wave. The transition condition should be "once all SPAWNED workers complete AND no more tasks are eligible for spawning," not just "all workers complete."

**RISK**: The all-or-nothing transition means a single slow worker blocks the entire wave. If worker 1 finishes in 30 seconds and worker 3 takes 290 seconds (near timeout), worker 1's completed task cannot begin validation until worker 3 finishes. This is suboptimal for overall throughput.

**SUGGESTION**: Consider allowing "streaming" transition: as each worker completes and passes post-execution checks, its task can enter validation immediately (subject to `concurrency.validation` limits). This would require Wave 2 and Wave 3 to overlap, which adds complexity but significantly improves throughput. If this is intentionally omitted for simplicity, it should be documented as a known trade-off.

### Transition: Wave 3 to Wave 4

**SOUND**: The document says "Once ALL validations complete, transition to Wave 4." This is clear.

**GAP**: The handling of partial failures at this boundary is underspecified. If 2 out of 3 tasks pass validation and 1 fails, what goes to the merger? Only the 2 passing tasks? The failed task is presumably excluded from merge, but the document does not explicitly state this. If the failing task is in the same cohesion group as a passing task, the cohesion group is incomplete. Does the merger merge an incomplete cohesion group, or does it hold the entire group?

**SUGGESTION**: Explicitly specify: "Only tasks with `result.status = pass` proceed to the merger. If a cohesion group has partial failures, the merger receives only the passing tasks from that group. The human is warned that the cohesion group is incomplete."

### Transition: Wave 4 to Wave 2 (Repeat)

**SOUND**: The repeat path is triggered by re-queued tasks or remaining tasks. The system loops back to Wave 2, not Wave 1, which avoids unnecessary re-planning.

**GAP**: The document says "If re-queued tasks or remaining tasks in `tasks.yaml`, repeat from Wave 2." But it does not specify whether the human is given a choice to re-plan (go to Wave 1) instead of directly retrying (Wave 2). After seeing validation failures and rejections, the human might realize the plan itself was flawed and want to re-plan rather than retry the same tasks.

**SUGGESTION**: At the session continuation prompt, offer three options: (1) continue to next Wave 2 with existing tasks, (2) return to Wave 1 for re-planning, (3) stop.

### Cross-Cutting Transition Concern

**RISK**: The document does not specify a maximum number of wave cycles. A pathological scenario could loop indefinitely: tasks fail validation, get re-queued, fail again, get re-queued again, etc. The `max_retries` limit applies to individual agent failures, but not to the re-queue loop driven by human rejections or validation failures.

**SUGGESTION**: Add a configurable `max_wave_cycles` limit (e.g., default 5). After this many cycles, the system forces a stop and presents a summary of all unresolved tasks. This prevents runaway sessions.

---

## Cross-Wave Findings

### SOUND: Deterministic Controller Philosophy

The orchestrator is explicitly designed as a deterministic controller. Shell scripts handle mechanical operations at zero token cost. The orchestrator's role is coordination, not creativity. This is the right philosophy for reliability.

### SOUND: Defense in Depth

The three-phase watcher model (pre-execution hooks, runtime OS sandbox, post-execution filesystem diff) provides genuine defense in depth. Even if one layer fails, the others catch violations.

### SOUND: Cost Optimization

Using haiku for validators, deterministic shell scripts for mechanical checks, token budgets per agent, and memory decay for Beads are all effective cost controls.

### GAP: No Specification for Orchestrator Failure During a Wave Transition

The document covers orchestrator crash during Wave 2 (graceful shutdown, state persistence). But it does not specify what happens if the orchestrator crashes BETWEEN waves (e.g., after Wave 2 completes but before Wave 3 starts). The `state.yaml` crash recovery state is mentioned but its schema and recovery logic are not specified. What state is saved? What wave does the system resume from?

### GAP: No Specification for tasks.yaml Corruption

`tasks.yaml` is the single source of truth for task state. Multiple agents and the orchestrator read/write it. If it becomes corrupted (e.g., partial write during a crash), the entire system could fail. There is no backup, checksum, or transactional write mechanism specified.

### SUGGESTION: Add Atomic Write for tasks.yaml

Use write-to-temp-then-rename for all `tasks.yaml` updates. This ensures the file is always in a consistent state, even if the orchestrator crashes mid-write.

---

## Finding Summary Table

| # | Wave | Category | Finding |
|---|------|----------|---------|
| 1 | W1 | GAP | Planner watcher enforcement not specified |
| 2 | W1 | GAP | Planner token budget exceeded recovery path missing |
| 3 | W1 | GAP | "Edit" option mechanics not specified |
| 4 | W1 | GAP | "Reject" loop context passing not specified |
| 5 | W1 | RISK | No escape hatch from plan approval loop |
| 6 | W1 | GAP | Re-queued tasks skip Wave 1, preventing plan adjustment |
| 7 | W2 | GAP | Task claimed after worker spawn (race condition) |
| 8 | W2 | GAP | Task selection algorithm with lock conflicts is ambiguous |
| 9 | W2 | GAP | Heartbeat only checks process existence, not progress |
| 10 | W2 | GAP | Token budget estimation accuracy not specified |
| 11 | W2 | RISK | Cascading dependency failure not handled |
| 12 | W2 | GAP | Retry timing (immediate vs. deferred) not specified |
| 13 | W2 | GAP | Retry counter location not explicit in task schema |
| 14 | W2 | RISK | Postcheck failure cleanup sequence not explicit |
| 15 | W3 | GAP | Validator read-only enforcement not specified |
| 16 | W3 | GAP | Audit log summarization method not specified |
| 17 | W3 | GAP | Failed validation re-queue lacks human guidance mechanism |
| 18 | W3 | RISK | Validator failure/timeout handling not specified |
| 19 | W3 | GAP | Validation scheduling order not specified |
| 20 | W4 | GAP | Merger watcher/permission model not specified |
| 21 | W4 | RISK | Inter-group dependency ordering for changeset presentation |
| 22 | W4 | GAP | "Skip" option semantics undefined |
| 23 | W4 | GAP | Cannot reconsider earlier changeset approvals |
| 24 | W4 | RISK | Resolvable vs. semantic conflict boundary undefined |
| 25 | W4 | GAP | Cross-group merge conflicts not addressed |
| 26 | W4 | GAP | Re-queued task file_locks cannot be modified |
| 27 | W4 | GAP | No distinction between wrong implementation and wrong task |
| 28 | Trans | GAP | Wave 2 to 3 transition ignores never-spawned dependent tasks |
| 29 | Trans | RISK | All-or-nothing wave transition blocks throughput |
| 30 | Trans | GAP | Partial cohesion group handling at Wave 3 to 4 boundary |
| 31 | Trans | GAP | No option to return to Wave 1 for re-planning |
| 32 | Trans | RISK | No maximum wave cycle limit (infinite loop potential) |
| 33 | Cross | GAP | Orchestrator crash between waves recovery not specified |
| 34 | Cross | GAP | tasks.yaml corruption/atomic write not addressed |

**Total: 10 SOUND, 20 GAP, 8 RISK, 8 SUGGESTION**

---

## Recommendation

The wave-based architecture is fundamentally sound. The four-wave model (plan, develop, validate, merge) with human gates is a good fit for supervised multi-agent orchestration. The primary areas needing attention are:

1. **Dependency failure cascading** (Wave 2, finding #11) -- this is the highest-priority gap because it can silently drop tasks.
2. **Validator failure handling** (Wave 3, finding #18) -- validators are agents too and can fail; the system needs a path for this.
3. **Inter-group merge ordering** (Wave 4, finding #21) -- approving changesets in the wrong order can produce an inconsistent codebase.
4. **Wave cycle bounds** (Transitions, finding #32) -- without a maximum, pathological inputs can loop indefinitely.

Addressing these four issues would bring the orchestration flow from "good" to "robust."
