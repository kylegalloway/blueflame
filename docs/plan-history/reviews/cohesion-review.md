# Cohesion Review: Blue Flame Plan-ADR

**Overall Cohesion Score: 8 / 10**

**Summary**: The plan is impressively well-integrated for its scope. The wave-based orchestration model, shell helper delegation, watcher enforcement model, and agent prompt design tell a consistent story. The skill structure maps cleanly to what the orchestration flow needs, and the configuration schema covers nearly everything referenced downstream. The most significant issues are (a) a handful of terminology and behavioral misalignments between sections, (b) some under-specified interactions between the orchestrator skill and its shell helpers, and (c) a few features referenced in one section that never appear elsewhere. None of these are design-breaking, but several would force an implementer to make judgment calls.

---

## 1. Do All Sections Tell the Same Story?

### **COHESIVE**: Overall narrative arc
The document maintains a clear, consistent story throughout: a human project lead configures `blueflame.yaml`, a Claude Code Skill reads it and runs four waves (plan, develop, validate, merge), shell scripts handle mechanical operations, watchers enforce constraints at three layers, Beads provides cross-session memory, and the human approves at two gates. Every section reinforces this arc.

### **COHESIVE**: The cost optimization thread
The plan consistently emphasizes low-cost operation from start to finish: haiku for validators, deterministic shell-based watchers (zero token cost), focused prompts, token budgets, Beads memory decay, and per-wave concurrency limits. This thread is never contradicted.

### **COHESIVE**: Defense-in-depth enforcement model
The three-phase watcher model (hooks, OS sandbox, post-execution diff) is consistently described across the Watcher Enforcement section, the Wave 2 flow (steps 2d, 2e, 4b-4d), and the Verification Plan (tests 2, 3, 4). The layering is coherent.

---

## 2. Does the Skill Structure Match What the Wave Orchestration Flow Needs?

### **COHESIVE**: Script-to-flow mapping
Every script listed in the `scripts/` directory is called by name somewhere in the Wave Orchestration Flow:
- `blueflame-init.sh` -- Wave 1 step 1
- `worktree-manage.sh` -- Wave 2 step 2b, Wave 4 step 8
- `lock-manage.sh` -- Wave 2 steps 2c, 4e
- `watcher-generate.sh` -- Wave 2 step 2e
- `watcher-postcheck.sh` -- Wave 2 step 4b
- `sandbox-setup.sh` -- Wave 2 step 2d
- `token-tracker.sh` -- Wave 2 step 3
- `lifecycle-manage.sh` -- Wave 2 step 2h
- `beads-archive.sh` -- Wave 1 step 2, Wave 4 step 7

All accounted for; no orphaned scripts.

### **COHESIVE**: Template-to-role mapping
Four prompt templates (`planner-prompt.md`, `worker-prompt.md`, `validator-prompt.md`, `merger-prompt.md`) map 1:1 to the four agent roles spawned in the four waves. The Agent System Prompts section provides content for each.

### **GAP**: Where does `tasks.yaml` live?
The Task File section defines `tasks.yaml` but the Skill Structure does not show it. The planner generates it (Wave 1 step 6), and the orchestrator reads it (Wave 2 step 1). It is presumably written to the project root or the `.blueflame/` runtime directory, but neither location is stated. An implementer would need to guess. The `.blueflame/` runtime state directory lists `agents.json`, `state.yaml`, `locks/`, `hooks/`, and `logs/`, but not `tasks.yaml`. This is a notable omission since `tasks.yaml` is the single most important runtime artifact -- the entire Wave 2-4 flow pivots on it.

### **AMBIGUITY**: Hook placement path differs between sections
In the Skill Structure, generated hooks live in `.blueflame/hooks/`. In the Three-Phase Watcher Enforcement Model, the hook command path is `.blueflame/hooks/<agent-id>/watcher.sh` (a subdirectory per agent). The `.claude/settings.json` snippet shows this per-agent subdirectory path. The Skill Structure section does not indicate per-agent subdirectories under `.blueflame/hooks/`, only showing `.blueflame/hooks/` as a flat directory. This is a minor inconsistency -- it could be inferred, but it should be explicit.

---

## 3. Does the Configuration Schema Cover Everything the Watcher and Orchestrator Reference?

### **COHESIVE**: Comprehensive coverage
The `blueflame.yaml` schema is thorough. Every check listed in the Phase 1 Watcher table can be traced to a config field:
- Tool allowlist -> `permissions.allowed_tools` / `permissions.blocked_tools`
- Path restrictions -> `permissions.allowed_paths` / `permissions.blocked_paths`
- Bash filtering -> `permissions.bash_rules.blocked_patterns`
- File scope -> `validation.file_scope.enforce` (plus task-level `file_locks`)
- Commit format -> `validation.commit_format.pattern`
- File naming -> `validation.file_naming.style`
- Test requirements -> `validation.require_tests`
- Token budget -> `limits.token_budget`

### **GAP**: `allowed_commands` referenced but not enforced
The config defines `bash_rules.allowed_commands` (a whitelist of allowed bash commands like `go test`, `go build`, etc.), but the Phase 1 Watcher table only mentions checking against `blocked_patterns`. There is no watcher check described as "Is this bash command in `allowed_commands`?" The watcher table mentions "Block network commands" and "Does command match `blocked_patterns`?" but never references the allowlist. Either `allowed_commands` is redundant (since `blocked_patterns` provides a denylist), or the watcher should also enforce an allowlist check. The plan never clarifies which model (allowlist, denylist, or both) applies to bash commands.

### **GAP**: `superpowers.skills` configuration is not connected to enforcement
The config defines `superpowers.skills` listing three skills ("test-driven-development", "systematic-debugging", "requesting-code-review"), but there is no mechanism described for ensuring agents only use these skills and not others. The planner prompt mentions `/brainstorm` and `/write-plan`, which are not in the configured list. The worker prompt says "Follow TDD via superpowers" which would align with "test-driven-development", but the skills `brainstorm` and `write-plan` are never listed in the config. This creates a disconnect between what the config declares and what agents are actually told to use.

### **AMBIGUITY**: `allowed_commands` semantics unclear
The `bash_rules.allowed_commands` list includes both commands with arguments (`go test`, `go build`) and bare commands (`make`). It is unclear whether `go test ./...` would match `go test`, whether partial prefix matching is used, or whether this is an exact-match list. Since the watcher enforcement model does not reference this field, its matching semantics are undefined.

---

## 4. Are Shell Script Responsibilities Clearly Delineated?

### **COHESIVE**: Clear separation of concerns
Each script has a distinct responsibility with no overlap:
- `blueflame-init.sh`: prereq validation + directory creation + stale cleanup
- `worktree-manage.sh`: git worktree CRUD
- `lock-manage.sh`: advisory lock CRUD
- `watcher-generate.sh`: generate per-agent hook scripts
- `watcher-postcheck.sh`: post-execution diff validation
- `token-tracker.sh`: token usage estimation from audit logs
- `lifecycle-manage.sh`: PID/PGID tracking and orphan management
- `beads-archive.sh`: save/load session data to/from Beads
- `sandbox-setup.sh`: OS-level resource limits

No two scripts share responsibility for the same concern.

### **AMBIGUITY**: Who is responsible for writing `tasks.yaml` updates?
The orchestrator "writes agent ID to tasks.yaml" (Wave 2 step 2g) and marks tasks as `done` or `failed` (Wave 2 step 4). The validator "marks result as `pass` or `fail`" (Wave 3 step 1d). But the validator is a spawned agent in a worktree -- does it write directly to `tasks.yaml`? If `tasks.yaml` is in the main repo, the validator would need access to it. If it is in `.blueflame/`, that path is in `blocked_paths`. The plan never specifies the mechanism by which a validator's pass/fail decision is communicated back to the orchestrator. The orchestrator could read the validator's output from the Task tool return value, but this is never stated.

### **GAP**: `sandbox-setup.sh` interaction with Task tool spawning
The Wave 2 flow says `sandbox-setup.sh <agent-id>` runs before agent launch (step 2d), and then the worker is spawned via Task tool (step 2f). But `ulimit` settings apply to the current shell and its children. If `sandbox-setup.sh` runs in a separate Bash invocation (as the orchestrator would call it), those ulimits would not persist to the Task tool invocation, which is a different execution context entirely. The plan does not address how OS-level limits are actually inherited by the spawned agent process. This is a significant implementation gap -- the `ulimit` approach may not work as described unless the agent is launched as a child of the sandboxed shell.

---

## 5. Do Agent Prompts Align with What the Orchestrator Expects?

### **COHESIVE**: Worker prompt matches enforcement model
The worker prompt tells the agent to "Work ONLY in your worktree. Touch ONLY files listed in your task's file_locks." This aligns with the watcher Phase 1 file scope check and Phase 3 post-execution diff. The prompt also says "Commit with format: `type(task-ID): description`" which matches the `commit_format.pattern` regex.

### **COHESIVE**: Validator prompt matches Wave 3 expectations
The validator prompt's four checks (applicability, correctness, regressions, scope creep) match the validator checks listed in Wave 3 step 1c.

### **COHESIVE**: Merger prompt matches Wave 4 expectations
The merger prompt's instruction to "stop and report" on unresolvable conflicts matches Wave 4 step 3's "does not guess" behavior.

### **INCONSISTENCY**: Planner prompt references superpowers skills not in config
The planner prompt says "Use superpowers /brainstorm and /write-plan." The `superpowers.skills` config lists "test-driven-development", "systematic-debugging", and "requesting-code-review". Neither `/brainstorm` nor `/write-plan` appears in the configured skills list. Either the config is incomplete, or the planner prompt references capabilities that will not be available.

### **GAP**: Worker prompt mentions TDD superpowers but does not specify which skill
The worker prompt says "Follow TDD via superpowers" without specifying a skill name. The config lists "test-driven-development" as a superpowers skill, so presumably this is it, but the prompt does not name it. An implementer would need to decide whether to inject the skill name or leave it implicit.

### **AMBIGUITY**: How does the planner know the YAML schema?
The planner prompt says "Output tasks in the YAML schema provided" but the prompt itself does not include the schema. Presumably the orchestrator injects the schema as part of the context, but this is never stated. The Task File section defines the schema, but it is unclear whether the full schema or just an example is passed to the planner.

---

## 6. Does the Implementation Phase Ordering Make Sense?

### **COHESIVE**: Dependency ordering is sound
- Phase 1 (shell helpers) has no dependencies on other phases
- Phase 2 (watcher system) depends on Phase 1's `blueflame-init.sh` for setup and the existence of the config schema
- Phase 3 (orchestrator skill) depends on Phase 1 (shell helpers to call) and Phase 2 (watchers to configure)
- Phase 4 (lifecycle + Beads) depends on Phase 3 (orchestrator to integrate with)
- Phase 5 (polish) depends on all prior phases

This is a clean dependency chain.

### **INCONSISTENCY**: Phase 2 milestone says "manually spawned claude agent" but Phase 3 has not built the orchestrator yet
Phase 2's milestone states: "A manually spawned claude agent is constrained by generated watchers." This is achievable without the orchestrator, but it implicitly requires someone to manually run `watcher-generate.sh`, manually create a worktree, and manually spawn a Claude agent. The prerequisite scripts from Phase 1 would be needed, but the watcher-generate script also needs a task definition (for `file_locks`), which comes from `tasks.yaml`, which comes from the planner in Phase 3. This means Phase 2 testing requires a manually-written `tasks.yaml` or the `watcher-generate.sh` must accept file locks as arguments rather than reading them from `tasks.yaml`. This is not stated.

### **GAP**: `token-tracker.sh` is in Phase 2 but token budget enforcement is in Phase 4
Phase 2 deliverables include `token-tracker.sh` for parsing audit logs and estimating token usage. Phase 4 deliverables include "Token budget hard-stop: kill agents that exceed their token budget." But the Wave 2 orchestration flow (step 3) shows the orchestrator doing token budget tracking during worker execution, which requires both the tracker (Phase 2) and the kill mechanism (Phase 4). At Phase 3 (orchestrator), the token tracker exists but the enforcement mechanism does not. This means the Phase 3 orchestrator either cannot enforce token budgets or must implement a partial version that gets replaced in Phase 4.

---

## 7. Is Terminology Consistent Throughout?

### **COHESIVE**: Agent ID format
Agent IDs consistently follow the `worker-<short-hash>` pattern (e.g., `worker-a1b2c3`) across the Wave 2 flow, agents.json example, worktree paths, and lock file naming.

### **COHESIVE**: Task status names
The status values `pending`, `claimed`, `done`, `failed`, `requeued` are used consistently. The `tasks.yaml` schema defines them, Wave 2 uses `done`/`failed`, Wave 4 uses `requeued`, and the re-queue logic references the transitions correctly.

### **INCONSISTENCY**: "Wave" vs "Phase" terminology overload
The document uses "Wave" for orchestration stages (Wave 1-4) and "Phase" for both watcher enforcement layers (Phase 1-3) and implementation stages (Phase 1-5). The Verification Plan then references "Watcher test (Phase 1 - hooks)" and "Watcher test (Phase 3 - postcheck)" which uses watcher phase numbers. This creates ambiguity when discussing "Phase 2" -- does it mean the watcher's runtime constraints, or the implementation phase for the watcher system? The three naming systems (waves, watcher phases, implementation phases) are never explicitly disambiguated.

### **INCONSISTENCY**: Verification Plan references "Phase 1" and "Phase 3" for watcher phases but these differ from watcher section naming
Verification Plan item 2 says "Watcher test (Phase 1 - hooks)" and item 3 says "Watcher test (Phase 3 - postcheck)". But the Three-Phase Watcher Enforcement Model labels them "Phase 1: Pre-Execution", "Phase 2: Runtime Constraints", and "Phase 3: Post-Execution". The Verification Plan skips Phase 2 (runtime constraints) in its watcher test labels, jumping from "Phase 1" to "Phase 3". Verification item 4 ("Sandbox test") covers the Phase 2 runtime constraints but does not label it as "Phase 2". This is confusing but not incorrect -- it just breaks the labeling pattern.

### **AMBIGUITY**: Agent ID naming for non-worker agents
The plan specifies `worker-<short-hash>` for worker agent IDs but never defines the ID format for planner, validator, or merger agents. The agents.json example only shows a worker entry. Validator agents need IDs too (for audit logs, watcher hooks in their worktrees, etc.). Are they `validator-<hash>`? `planner-<hash>`? This is never specified.

---

## 8. Does the Verification Plan Test Everything the Design Claims to Support?

### **COHESIVE**: Good coverage of core features
The 12 verification items cover: helper scripts, watcher hooks, watcher postcheck, OS sandbox, end-to-end flow, lifecycle/crash recovery, lock conflicts, re-queue, Beads persistence, token budgets, orphan cleanup, and changeset chaining. These map well to the major design claims.

### **GAP**: No verification test for dependency-ordered task execution
The `tasks.yaml` schema includes a `dependencies` field, and the Wave 2 flow says "identifies ready tasks (no unmet dependencies)." But no verification test checks that a task with unmet dependencies is correctly deferred. This is a core scheduling feature with no explicit test.

### **GAP**: No verification test for cohesion group merge ordering
The design emphasizes cohesion groups for merge semantics (Design Lineage table, tasks.yaml schema, Wave 4 flow, merger prompt). But no verification test checks that the merger correctly groups changes by cohesion group or that merge ordering respects group boundaries.

### **GAP**: No verification test for the plan approval gate
Wave 1 includes a human approval loop ("Human approves, edits, or rejects. Loop until approved."). No verification test covers the plan approval interaction, plan editing, or rejection-and-retry flow.

### **GAP**: No verification test for validation failure handling
Wave 3 step 2 says "Failed tasks: human is notified, can retry (re-queue) or skip." No verification test checks validator-driven failure handling (distinct from changeset rejection in Wave 4). The re-queue test (item 8) only covers Wave 4 changeset rejection.

### **GAP**: No verification test for Superpowers integration
The design references Superpowers skills in the config, planner prompt, and worker prompt. No verification test checks that Superpowers skills are available or usable by agents.

### **GAP**: No verification test for `max_retries` exhaustion
The config defines `max_retries: 2` and the Timeout Enforcement section says "available for retry up to max_retries." No test verifies that a task which fails `max_retries + 1` times is permanently marked as failed rather than infinitely re-queued.

---

## 9. Orphaned Concepts (Mentioned in One Section, Never Referenced Elsewhere)

### **INCONSISTENCY**: `state.yaml` for crash recovery
The Skill Structure lists `.blueflame/state.yaml` as "Crash recovery state." The Graceful Shutdown section says "Persist state to `.blueflame/state.yaml` for crash recovery" (step 5). Phase 4 mentions "Crash recovery: persist state to `.blueflame/state.yaml`, restore on restart." But the actual content and schema of `state.yaml` is never defined. What state is persisted? Current wave number? Active task statuses? The orphan cleanup section (which runs on startup) references `agents.json` for stale entries but not `state.yaml` for recovery. There is no description of how `state.yaml` is consumed on restart.

### **AMBIGUITY**: "Changeset chaining with human approval" in Design Lineage
The Design Lineage table entry says "Multiple changesets reviewed sequentially before next session." But the Wave 4 flow and Session Continuation section describe reviewing changesets before the next *wave cycle*, not the next *session*. A "session" and a "wave cycle" are different concepts in this plan. The Design Lineage wording implies cross-session chaining, but the actual design describes intra-session chaining.

### **GAP**: `history` array on tasks
The `tasks.yaml` schema includes `history: []` described as "Prior attempts (from re-queued tasks)." Wave 4 step 6 says "Rejected changesets: tasks marked `requeued` with rejection notes in `history` array." But the plan never defines the schema of history entries. What fields does a history entry contain? Timestamp? Attempt number? Prior agent ID? Rejection reason? Diff reference? An implementer would need to invent this schema.

---

## 10. Implicit Assumptions Never Stated

### **AMBIGUITY**: How does the orchestrator (a Claude Code Skill) run shell scripts?
The plan says the orchestrator calls shell scripts for mechanical operations via Bash. But a Claude Code Skill is essentially a prompt/context injection -- it instructs the LLM what to do. The LLM would need to use the `Bash` tool to run each shell script. This means every shell script invocation costs tokens (the LLM must generate the tool call and process the result). The plan says shell helpers keep "orchestrator tokens at zero for mechops" but this is only true for the scripts themselves, not for the LLM overhead of invoking them. This is a minor but systematically optimistic cost assumption.

### **AMBIGUITY**: Task tool concurrency model
The plan says the orchestrator spawns multiple workers "up to `concurrency.development`" via Task tool, then "monitors all workers" for liveness and token budgets. But the plan never addresses whether the Task tool supports concurrent dispatch (fire-and-forget with monitoring) or is sequential (blocking until the task completes). If Task tool calls are blocking, the orchestrator cannot spawn 4 workers concurrently -- it would spawn one, wait for it to complete, then spawn the next. The entire concurrency model depends on Task tool behavior that is never specified.

### **AMBIGUITY**: Validator access to worktrees
Wave 3 says validators are spawned "in the task's worktree." But Wave 2 step 4 does not mention preserving worktrees after worker completion -- only Wave 4 step 8 cleans them up. It is implied that worktrees persist from Wave 2 through Wave 4, but this is never explicitly stated as a requirement.

### **GAP**: How does the orchestrator know a worker has completed?
Wave 2 step 4 begins "As each worker completes" but never explains how the orchestrator detects completion. If Task tool calls are blocking, completion is implicit (the call returns). If they are concurrent, the orchestrator needs a polling or callback mechanism. The heartbeat mechanism checks liveness (is the process alive?) but not completion (did it finish successfully?). This is a fundamental orchestration question left unanswered.

### **AMBIGUITY**: Git worktree branching strategy
`worktree-manage.sh create <agent-id> <base-branch>` creates a worktree, but does it create a new branch? What is the branch named? The `tasks.yaml` schema has a `branch` field (populated when claimed), and the validator receives `git diff main...<branch>`, but the branch naming convention is never specified. Is it `blueflame/<agent-id>`? `task-001`? The merger needs to reference branches by name, but the naming is undefined.

---

## 11. Does the "What This Plan Intentionally Omits" Section Cover All Notable Omissions?

### **COHESIVE**: Good coverage of major exclusions
The five listed omissions (Go binary, Python runtime, sub-agent spawning, network access, persistent agents) are genuine design decisions that a reader might question. Each has a brief rationale.

### **GAP**: Missing omission -- no rollback mechanism
The plan has no mechanism to undo a merged changeset. If a changeset is approved and merged to `base_branch` in Wave 4, and the human later realizes it was wrong, there is no blueflame-level rollback. This is a notable omission that should be acknowledged.

### **GAP**: Missing omission -- no cross-task communication
Workers are fully isolated. There is no mechanism for one worker to communicate with another worker during Wave 2 (e.g., to coordinate an interface). The plan implicitly assumes task decomposition eliminates the need for inter-agent communication, but this assumption is never stated.

### **GAP**: Missing omission -- no partial wave execution
If Wave 2 has 4 tasks and 2 succeed while 2 fail, the plan proceeds to Wave 3 only for the successful tasks. But what about the failed tasks? They are marked `failed` and presumably available for retry, but the plan says "repeat from Wave 2" only after Wave 4 (step 9). There is no mechanism to retry failed Wave 2 tasks before proceeding to Wave 3. The omission of intra-wave retry is never acknowledged.

### **GAP**: Missing omission -- no multi-repo support
The design assumes a single git repository. No mention is made of multi-repo projects or monorepo sub-project scoping.

---

## 12. Is the Plan Complete Enough to Implement Without Significant Ambiguity?

### **COHESIVE**: Shell scripts are well-specified
Each script has clear subcommands, argument formats, and expected behaviors. An implementer could write `worktree-manage.sh` and `lock-manage.sh` directly from the plan.

### **AMBIGUITY**: SKILL.md content is not specified
The plan's most critical artifact -- `SKILL.md`, the orchestrator skill definition -- has no content outlined beyond "main orchestrator skill definition with full wave protocol." The Wave Orchestration Flow section describes the logic, but translating that into a SKILL.md that a Claude Code LLM can follow requires careful prompt engineering. The plan provides the "what" but not the "how" for the single most important file.

### **AMBIGUITY**: `watcher-generate.sh` input format
The Wave 2 flow calls `watcher-generate.sh <agent-id>`, passing only the agent ID. But the watcher needs to know: (a) allowed/blocked paths from `blueflame.yaml`, (b) file_locks from the specific task, (c) token budget for this agent type. The script must read both `blueflame.yaml` and `tasks.yaml` (or receive these as additional arguments). The plan does not specify which approach is used or where the scripts look for these files.

### **GAP**: Error handling strategy undefined
The plan describes the happy path and some failure modes (task failure, timeout, crash recovery) but never defines a general error handling strategy. What happens if `worktree-manage.sh create` fails? If `lock-manage.sh acquire` fails due to a filesystem error (not a conflict)? If `tasks.yaml` is malformed? If the `claude` CLI crashes? Each of these would need handling, but no general approach is specified.

---

## 13. Does the Design Lineage Table Match What is Actually in the Plan?

### **COHESIVE**: Strong alignment for most entries
The following lineage entries are fully realized in the plan:
- Claude Code Skill as orchestrator -> SKILL.md, Wave Orchestration Flow
- Shell scripts for mechanical operations -> `scripts/` directory, all 9 scripts
- Three-phase watcher enforcement -> Three-Phase Watcher Enforcement Model section
- Watcher hooks via PreToolUse -> Phase 1 hook config JSON
- Post-execution filesystem diff -> `watcher-postcheck.sh`, Phase 3 section
- OS-level runtime constraints -> `sandbox-setup.sh`, Phase 2 section
- Process groups for orphan prevention -> `lifecycle-manage.sh`, Agent Lifecycle Management
- Beads integration -> `beads-archive.sh`, Beads Integration section
- Token budget tracking -> `token-tracker.sh`, Token Budget Enforcement section
- Two-tier validation -> Wave 2 (mechanical) + Wave 3 (semantic)
- Cohesion groups -> `tasks.yaml` schema, Wave 4 merge logic
- Re-queue logic -> Wave 4 step 6, `requeued` status
- Deterministic controller mindset -> implicit throughout (orchestrator does not create, only coordinates)
- Changeset chaining -> Wave 4 interactive approval flow
- Per-wave configurable concurrency -> `concurrency` config section
- Network isolation -> sandbox config, Phase 2 OS-level, blocked_patterns

### **INCONSISTENCY**: "Changeset chaining with human approval" lineage says "Plan A, Plan B"
The rationale says "Multiple changesets reviewed sequentially before next session." As noted in finding 10.2 above, the actual design reviews changesets before the next *wave cycle*, not the next *session*. The lineage description does not match the plan's actual behavior.

---

## 14. Additional Findings

### **INCONSISTENCY**: Validation failure flow differs between Wave 3 and Wave 4
Wave 3 step 2: "Failed tasks: human is notified, can retry (re-queue) or skip." Wave 4 processes only validated (passed) tasks. But what happens to validation-failed tasks? They do not appear in Wave 4's changeset flow. The re-queue path for validation failures is mentioned but its mechanics are undefined. In contrast, Wave 4 rejection has clear mechanics (task marked `requeued` with rejection notes in `history`). Validation failure re-queuing has no equivalent specification.

### **INCONSISTENCY**: Worker claims task in step 2g, but step 2f spawns the worker first
Wave 2 step 2f spawns the worker via Task tool. Step 2g writes the agent ID to `tasks.yaml` (claiming the task). But the worker is already running by step 2g. If the claim fails for any reason (e.g., another race condition), the worker is already active. Logically, the claim should happen before the spawn. The ordering appears inverted.

### **GAP**: No specification for how the merger accesses all worktree branches
Wave 4 step 2: "Merger sees all validated branches and their diffs." But where does the merger run? It is spawned as a single agent -- in which directory? The main repo? Its own worktree? It needs to see all branches from all workers' worktrees, which requires either access to the main repo (where branches are shared via git's object database) or some other mechanism. This is never specified.

### **AMBIGUITY**: `concurrency.validation` says 2, but Wave 3 says "1 per worker"
The Architecture Overview diagram says "(1 per worker)" for validators. The config sets `concurrency.validation: 2`. If there are 4 workers, there are 4 tasks to validate, but only 2 validators can run concurrently. The "1 per worker" label in the diagram is misleading -- it suggests a 1:1 mapping, but the concurrency limit means validators run in batches of 2. This is not a contradiction but the diagram label is imprecise.

### **GAP**: No specification for how the orchestrator passes model selection to agents
The config specifies `models.planner: "sonnet"`, `models.worker: "sonnet"`, etc. But the plan never describes how the orchestrator passes the model choice to the Task tool when spawning agents. Is it a Task tool parameter? A CLI flag? This is a practical implementation detail that is undefined.

---

## Summary of Findings by Category

| Category | Count |
|----------|-------|
| COHESIVE | 14 |
| INCONSISTENCY | 6 |
| GAP | 15 |
| AMBIGUITY | 10 |

### Critical Items (would block or significantly complicate implementation)

1. **GAP**: Task tool concurrency model undefined -- the entire parallel execution design depends on this
2. **GAP**: `sandbox-setup.sh` ulimit approach may not work with Task tool spawning model
3. **GAP**: How orchestrator detects worker completion is unspecified
4. **GAP**: `tasks.yaml` location never specified despite being the central runtime artifact
5. **AMBIGUITY**: SKILL.md content (the single most important file) has no specification
6. **INCONSISTENCY**: Worker spawn (step 2f) happens before task claim (step 2g) -- ordering is inverted

### Moderate Items (would require implementer judgment calls)

7. **GAP**: `allowed_commands` config field is never referenced by the watcher enforcement model
8. **INCONSISTENCY**: Planner prompt references superpowers skills not listed in config
9. **GAP**: Validation failure re-queue mechanics undefined (Wave 3 vs Wave 4 asymmetry)
10. **GAP**: No verification tests for dependency ordering, cohesion groups, plan approval, or max_retries exhaustion
11. **AMBIGUITY**: Git branch naming convention never specified
12. **GAP**: `state.yaml` schema and recovery consumption never defined
13. **GAP**: `history` array entry schema never defined
14. **AMBIGUITY**: Agent ID format for non-worker agents never specified

### Minor Items (cosmetic or easily resolved)

15. **INCONSISTENCY**: "Wave" / "Phase" / "Phase" terminology overload
16. **AMBIGUITY**: Hook path inconsistency (flat vs. per-agent subdirectory)
17. **INCONSISTENCY**: Design Lineage says "next session" but design means "next wave cycle"
18. **AMBIGUITY**: Diagram says "1 per worker" for validators but concurrency limits apply
