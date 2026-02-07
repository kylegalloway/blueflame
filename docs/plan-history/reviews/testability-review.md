# Testability and Testing Plan Review

**Reviewed document**: `/Users/kylegalloway/src/blueflame/Plan-ADR.md`
**Review date**: 2026-02-07
**Focus**: Testability, test coverage gaps, testing strategy adequacy

---

## Test Coverage Gap Analysis (Summary)

| Area | Planned Tests | Gaps Found | Verdict |
|------|--------------|------------|---------|
| Shell helper scripts | Test 1 (partial) | No isolation strategy, no mocking plan | GAP |
| Watcher Phase 1 (hooks) | Test 2 | Requires live claude agent; no stub plan | HARD TO TEST |
| Watcher Phase 2 (sandbox) | Test 4 | Platform-divergent (Linux vs macOS); no cross-platform strategy | GAP |
| Watcher Phase 3 (postcheck) | Test 3 | Adequately scoped but narrow | TESTABLE |
| Wave orchestration | Test 5 (E2E only) | No unit/integration tests for wave state machine | GAP |
| Task claiming / YAML state | Implicit in Test 5 | No isolated state transition tests | GAP |
| Lock management | Test 7 | Scoped but misses race conditions and stale-lock edge cases | GAP |
| Lifecycle / heartbeat | Test 6 | Good scope but no mock-process strategy | HARD TO TEST |
| Token budget enforcement | Test 10 | Scoped narrowly; no log-parsing unit tests | GAP |
| Beads integration | Test 9 | Requires two full sessions; no unit-level memory test | HARD TO TEST |
| Changeset chaining / approval | Test 12 | Requires human interaction; no automation plan | GAP |
| Re-queue logic | Test 8 | Adequately scoped | TESTABLE |
| Orphan / crash recovery | Test 11 | Good scope but no deterministic crash simulation | HARD TO TEST |
| Regression testing | None | No strategy mentioned | GAP |
| Cross-platform testing | None | No strategy mentioned | GAP |
| Test infrastructure (repos, fixtures, mocks) | None | No strategy mentioned | GAP |
| Failure mode combinatorics | Partially covered | Several failure modes unaddressed | GAP |

**Overall assessment**: The 12 verification tests establish a reasonable acceptance-test skeleton but are almost entirely integration or end-to-end in character. The plan has no unit testing strategy, no mock/stub infrastructure, no fixture plan, no regression strategy, and no cross-platform testing approach. For a system of this complexity -- 9 shell scripts, a skill orchestrator, YAML state management, three-phase enforcement, OS-level sandboxing, and cross-session memory -- 12 tests is insufficient.

---

## Detailed Findings

### 1. Are the 12 verification tests sufficient for the system's complexity?

**GAP**

The 12 tests map roughly to one test per major subsystem, but the system has deep interaction surfaces that are not covered by single-subsystem tests. Specifically:

- There are 9 shell scripts, each with multiple subcommands (e.g., `worktree-manage.sh` has `create`, `remove`, `cleanup`, `list`; `lock-manage.sh` has `acquire`, `release`, `release-all`, `check`). Test 1 mentions "create/remove worktree" and "acquire/release/conflict-detect locks" but does not enumerate subcommand coverage for all 9 scripts. At minimum, every subcommand of every script needs at least one positive and one negative test.
- The watcher system alone has 8 distinct check types in Phase 1 (tool allowlist, path restrictions, bash filtering, file scope, commit format, file naming, test requirements, token budget). Test 2 covers 4 of these. The other 4 (commit format, file naming, test requirements, token budget at hook level) are not mentioned.
- There is no test for the interaction between lock conflicts and wave scheduling (Test 7 checks sequential execution but not the scheduling logic that detects the conflict and defers tasks).
- There is no test for dependency resolution within tasks.yaml (task-002 depends on task-001; what happens if task-001 fails?).
- There is no test for cohesion group merge ordering.

**Recommendation**: Expand to approximately 40-60 tests organized in a three-tier pyramid: unit tests for each script subcommand (~30), integration tests for subsystem interactions (~15), and end-to-end tests (~5). The current 12 are a reasonable top layer but the pyramid has no base.

---

### 2. Can each shell script be unit tested in isolation? How?

**TESTABLE (with effort)**

Shell scripts are inherently testable using frameworks like `bats-core` (Bash Automated Testing System) or plain shell-based test harnesses. Each of the 9 scripts can be unit tested as follows:

- **blueflame-init.sh**: Mock the `which`/`command -v` calls for prerequisite checking. Create a temp directory as the repo root. Verify the `.blueflame/` directory structure is created correctly. Test both the "all prerequisites present" and "missing prerequisite" paths.
- **worktree-manage.sh**: Initialize a real git repo in a temp directory (fast, no network). Test `create`, `remove`, `cleanup`, `list` subcommands against it. Verify branch naming, directory creation, and cleanup completeness.
- **lock-manage.sh**: Use a temp `.blueflame/locks/` directory. Test acquire (creates file with correct contents), release (removes file), check (detects existing lock), release-all (clears directory), and conflict detection (lock exists with live PID vs dead PID).
- **watcher-generate.sh**: Provide a minimal `blueflame.yaml` and task definition. Verify the generated watcher script contains the correct checks. Verify the generated `.claude/settings.json` has correct hook registration.
- **watcher-postcheck.sh**: Set up a git worktree with known before/after states. Run postcheck and verify pass/fail outcomes.
- **token-tracker.sh**: Create synthetic `.audit.jsonl` log files. Verify token counting, threshold detection, and budget exceeded detection.
- **lifecycle-manage.sh**: Test register/unregister against a temp `agents.json`. For PID/PGID checks, use the test script's own PID as a "live" process and a known-dead PID (e.g., 99999) for stale detection.
- **beads-archive.sh**: This depends on the Beads tool. Either mock the `beads` CLI or test only the YAML/JSON assembly logic in isolation.
- **sandbox-setup.sh**: Test that the script emits correct `ulimit` commands. On Linux, verify `unshare --net` invocation. On macOS, verify `sandbox-exec` invocation. Actual enforcement tests require spawning a child process.

**SUGGESTION**: Adopt `bats-core` as the shell test framework. It provides `setup`/`teardown` functions, temp directory management, assertions, and TAP output. Each script should have a corresponding `tests/scripts/test_<script-name>.bats` file.

---

### 3. How would you integration test the wave orchestration without spending real API tokens?

**GAP**

The plan has no strategy for testing wave orchestration without live API calls. This is a significant gap because:

- Each end-to-end wave cycle costs ~$0.78 per the plan's own estimate.
- Iterating on orchestration logic during development could require dozens of test runs.
- CI/CD environments typically cannot make API calls.

**SUGGESTION**: Implement a **dry-run mode** (mentioned in Phase 5 as a "polish" feature but essential for testing from Phase 3 onward). The dry-run mode should:

1. Replace `Task` tool dispatch with a mock that reads from a predefined "agent response" fixture file.
2. Simulate agent completion after a configurable delay.
3. Generate synthetic task results (pass/fail) from a fixture.
4. Exercise the full state machine (wave transitions, task claiming, re-queuing) without any API calls.

Additionally, the wave orchestration logic in `SKILL.md` should be decomposable into testable units:

- **State machine transitions**: Extract the wave transition logic (pending -> claimed -> done -> validated -> merged/requeued) into a function that takes current state and event, returns next state. Test this purely as data transformation.
- **Dependency resolution**: Extract the "which tasks are ready" logic. Test with various dependency graphs (linear, diamond, circular dependency detection).
- **Concurrency scheduling**: Extract the "how many agents to spawn" logic. Test with different concurrency configs and lock conflicts.

The plan currently treats dry-run as a Phase 5 polish item. It should be promoted to Phase 3 as a testing prerequisite.

---

### 4. Is the watcher system testable without spawning actual claude agents?

**HARD TO TEST**

The three phases have different testability profiles:

**Phase 1 (PreToolUse hooks)**: The generated watcher scripts are plain shell scripts that receive tool invocation data on stdin and return allow/block decisions. These CAN be tested without a claude agent by:
- Feeding synthetic JSON tool-use payloads to the watcher script's stdin.
- Checking the exit code and stdout for allow/block decisions.
- Verifying audit log entries are written correctly.

However, the plan does not document the exact input format that Claude Code sends to PreToolUse hooks. Without this specification, the test fixtures cannot be written. This is a documentation gap that blocks testing.

**Phase 2 (OS sandbox)**: Fully testable without claude agents. Spawn any child process (e.g., a simple shell script that attempts network access or exceeds memory) under the sandbox constraints and verify enforcement.

**Phase 3 (Post-execution diff)**: Fully testable without claude agents. Set up a worktree, make known modifications (some allowed, some not), run `watcher-postcheck.sh`, and verify the outcome.

**SUGGESTION**: Document the exact JSON schema that Claude Code sends to PreToolUse hook scripts. Create a `tests/fixtures/hook-inputs/` directory with sample payloads for every tool type (Read, Write, Edit, Bash, etc.). This makes Phase 1 watcher testing completely independent of the claude CLI.

---

### 5. Can the YAML task claiming and state transitions be tested independently?

**GAP**

The task lifecycle has 5 states: `pending -> claimed -> done -> failed -> requeued`. The plan does not describe any isolated tests for these transitions. Currently, they are only exercised as part of end-to-end tests (Tests 5, 6, 8, 12).

The claiming and state transition logic lives in the orchestrator skill (SKILL.md), which means it is implemented as LLM-directed YAML manipulation. This is inherently harder to test than code because:

- The "logic" is embedded in a natural language prompt, not in executable code.
- The transitions happen via the orchestrator calling shell commands or editing YAML files.
- There is no formal state machine definition that can be unit tested.

**SUGGESTION**: Create a shell script `task-manage.sh` (or extend `blueflame-init.sh`) that handles state transitions deterministically:
- `task-manage.sh claim <task-id> <agent-id>` -- transitions pending -> claimed
- `task-manage.sh complete <task-id>` -- transitions claimed -> done
- `task-manage.sh fail <task-id> <reason>` -- transitions claimed -> failed
- `task-manage.sh requeue <task-id> <notes>` -- transitions failed -> requeued (or done with rejected validation -> requeued)
- `task-manage.sh ready` -- lists tasks with all dependencies met and status=pending

This makes state transitions deterministic shell operations rather than LLM-directed YAML edits, and each subcommand is trivially unit testable. The orchestrator skill then calls these subcommands instead of manipulating YAML directly.

---

### 6. Is the three-phase watcher enforcement testable layer by layer?

**TESTABLE (Phase 2 and 3) / HARD TO TEST (Phase 1 in realistic conditions)**

- **Phase 1**: As noted in finding 4, the hook scripts themselves are testable via synthetic input. However, testing that Claude Code actually invokes the hooks correctly, respects the block decision, and handles hook timeouts (the 5000ms timeout in the settings.json) requires a live claude agent. There is no mock for the Claude Code hook invocation mechanism.

- **Phase 2**: Fully testable layer by layer. Each ulimit/sandbox constraint can be tested independently by spawning a controlled child process. On Linux: test `unshare --net` blocks network. On macOS: test `sandbox-exec` profile blocks network. Test memory limits with a known memory-allocating program. Test CPU limits with a busy-loop.

- **Phase 3**: Fully testable layer by layer. Create synthetic worktree states and run `watcher-postcheck.sh`. Each check (path validation, binary detection, sensitive content scan) can be tested independently by preparing specific filesystem states.

**SUGGESTION**: For Phase 1 realistic testing, create a minimal "canary agent" test: a claude agent with a scripted prompt that attempts one blocked operation and one allowed operation. This single test covers hook invocation, hook response handling, and audit logging. It costs minimal tokens (one agent, one tool call) and validates the integration point that cannot be mocked.

---

### 7. How do you test the Beads integration (cross-session memory) without full end-to-end runs?

**HARD TO TEST**

Test 9 ("Run two sessions -> verify planner in session 2 receives context from session 1") requires two complete wave cycles, which means:
- Two planner runs (API cost)
- Potentially two sets of workers, validators, and mergers (significant API cost)
- Real Beads archive and retrieval operations
- Approximately $1.56+ per test run

This is too expensive for iterative testing.

**SUGGESTION**: Decompose Beads testing into three layers:

1. **Unit test `beads-archive.sh save`**: Create synthetic session results (a fake `tasks.yaml` with completed tasks, fake audit logs). Run `beads-archive.sh save`. Verify the correct beads are created with expected content. This requires Beads CLI to be installed but costs zero API tokens.

2. **Unit test `beads-archive.sh load`**: Pre-populate beads storage with known test beads. Run `beads-archive.sh load`. Verify the output contains the expected prior session context (failure notes, patterns, cost history). Zero API tokens.

3. **Integration test**: Verify that the planner prompt template correctly incorporates the loaded beads context. This can be done by checking the assembled prompt string (the concatenation of `planner-prompt.md` template + beads context) without actually sending it to an LLM.

4. **Thin E2E test** (only if needed): Run one minimal session (1 task, succeed), then verify `beads-archive.sh load` returns that session's data. One API call, not two full cycles.

---

### 8. Are there edge cases not covered by the 12 listed tests?

**GAP**

The following edge cases are not addressed by any of the 12 tests:

| Edge Case | Risk | Suggested Test |
|-----------|------|----------------|
| Circular dependencies in tasks.yaml | Orchestrator hangs waiting for tasks that can never become ready | Planner or orchestrator should detect and reject cycles |
| Empty tasks.yaml (planner produces no tasks) | Orchestrator may crash or enter undefined state | Test graceful handling of zero-task plan |
| All tasks fail in Wave 2 | Wave 3 has nothing to validate; Wave 4 has nothing to merge | Test graceful skip of empty waves |
| Task with file_locks pointing to non-existent paths | Lock acquisition on non-existent directory | Test lock-manage.sh with missing paths |
| Extremely long file paths (lock file naming overflow) | Lock file names use underscore substitution; deep paths may exceed filesystem limits | Test with deeply nested paths |
| Concurrent lock acquisition race condition | Two orchestrator instances or timing issue in single orchestrator | Test with parallel lock-manage.sh invocations |
| YAML parsing failure (malformed tasks.yaml) | Orchestrator crash mid-wave | Test with invalid YAML |
| Git worktree creation failure (disk full, permissions) | Worker cannot start | Test worktree-manage.sh error handling |
| Agent produces no commits | Worker runs but makes no changes | Test postcheck with empty diff |
| Agent produces partial work (commits some files, crashes before others) | Incomplete task state | Test that partial work is handled correctly |
| Validator disagrees with postcheck (postcheck passes, validator fails, or vice versa) | Conflicting signals | Test the precedence/override logic |
| Multiple agents claim the same task (race condition) | Duplicate work, merge conflicts | Test task claiming atomicity |
| Merger encounters unresolvable conflicts between cohesion groups | Merger stops and reports, but what does the orchestrator do? | Test conflict escalation flow |
| Base branch advances between waves (someone else merges to main) | Workers based on stale main | Test rebase/merge handling |
| blueflame.yaml changes between waves in a chained session | Config drift mid-session | Test config reload behavior |

**Recommendation**: Each of these should have at least one test case. Many can be tested as shell script unit tests with synthetic inputs.

---

### 9. How do you test failure modes (agent crash, timeout, token budget exceeded, lock conflicts)?

**HARD TO TEST (for agent-level failures) / TESTABLE (for script-level failures)**

The plan covers some failure modes in Tests 6, 10, and 11, but the testing methodology is underspecified.

**Agent crash (Test 6)**:
- The test says "Kill a worker mid-execution" but does not specify how. Sending SIGKILL to the claude process? What if it is a child of the Task tool and the PID is not directly accessible?
- **SUGGESTION**: Create a test helper `test-crash-agent.sh` that spawns a long-running process, registers it with lifecycle-manage.sh, then kills it after a delay. This tests the detection and cleanup logic without requiring a real claude agent.

**Timeout (partially in Test 6)**:
- The timeout enforcement relies on the orchestrator monitoring agent start times. This requires the orchestrator to be running, which means this is an integration test, not a unit test.
- **SUGGESTION**: Test `lifecycle-manage.sh` timeout detection independently. Register an agent with a start time in the past (beyond the timeout), run the timeout check function, verify it returns "timed out."

**Token budget exceeded (Test 10)**:
- The test says "Set a low token budget -> verify agent is warned at 80%, killed at 100%." This requires a real claude agent that consumes tokens.
- **SUGGESTION**: Test `token-tracker.sh` with synthetic audit log files of known sizes. Create a log file representing 79% usage (verify no warning), 81% usage (verify warning), and 101% usage (verify kill signal). The kill action itself can be tested by checking that `token-tracker.sh` returns the correct exit code or outputs the correct command, without actually killing a process.

**Lock conflicts (Test 7)**:
- The test says "Two tasks with overlapping file_locks -> verify sequential execution within wave." This requires wave orchestration to be running.
- **SUGGESTION**: Test `lock-manage.sh acquire` with an already-held lock. Verify it returns a conflict error. Test the orchestrator's scheduling logic separately: given two tasks with overlapping locks, verify it schedules them sequentially (this requires the scheduling logic to be in a testable shell function).

---

### 10. Is there a strategy for regression testing after changes to shell scripts or the skill?

**GAP**

The plan contains no mention of:
- A CI/CD pipeline for running tests
- Automated test execution on changes
- A test suite runner
- Version pinning for test dependencies
- Baseline test results to compare against

This is a significant omission for a system where shell script bugs could cause agents to escape their sandboxes, leak tokens, or corrupt the repository.

**SUGGESTION**:

1. **Adopt a test runner**: `bats-core` for shell script tests. Run all tests with a single `make test` or `./run-tests.sh` command.
2. **Layer the test suite**:
   - `tests/unit/` -- shell script unit tests (fast, no API, no git repo needed for most)
   - `tests/integration/` -- multi-script interaction tests (needs temp git repo, no API)
   - `tests/e2e/` -- full wave cycle tests (needs API, run manually or with budget cap)
3. **Git pre-commit hook**: Run unit tests before allowing commits to blueflame scripts. This is ironic given that the system itself uses hooks, but it is effective.
4. **Snapshot testing for watcher generation**: Store the expected output of `watcher-generate.sh` given a known config. On any change to the template or generator, diff against the snapshot. This catches unintended changes to watcher behavior.
5. **Regression markers**: When a bug is found and fixed, add a test specifically for that bug, named `test_regression_<issue>`.

---

### 11. Can the changeset chaining and human approval flow be tested (or does it require manual testing)?

**GAP**

Test 12 covers "verify merger produces reviewable changesets, human can approve/reject individually, rejected tasks are re-queued" but does not specify how the human interaction is automated in tests.

The approval flow uses interactive prompts:
```
(a)pprove / (r)eject / (v)iew diff / (s)kip?
```

This is inherently interactive and requires either:
- A human at the keyboard (not automatable)
- Scripted stdin input (fragile, depends on exact prompt formatting)
- An abstraction layer that separates the decision logic from the I/O

**SUGGESTION**:

1. **Extract the approval logic**: The orchestrator should have a function that takes (changeset, decision) and produces the next state. The decision can come from interactive input OR from a test fixture. This is the "Humble Object" pattern: keep the I/O thin, test the logic.

2. **Create an `--auto-approve` mode**: For testing, the orchestrator accepts a decision file:
   ```yaml
   # test-decisions.yaml
   changesets:
     - id: 1
       decision: approve
     - id: 2
       decision: reject
       reason: "Test rejection"
     - id: 3
       decision: approve
   ```
   The orchestrator reads decisions from this file instead of prompting. This makes the entire approval flow testable end-to-end without human interaction.

3. **Test the re-queue path independently**: Given a task in state `done` with validation `pass`, apply a `reject` decision. Verify the task transitions to `requeued` with the rejection reason in its `history` array. This can be a pure YAML manipulation test.

---

### 12. What about testing on different platforms (Linux vs macOS, especially for sandbox differences)?

**GAP**

The plan explicitly calls out platform-divergent behavior in Phase 2 (sandbox):
- Linux: `unshare --net` for network isolation
- macOS: `sandbox-exec` for network isolation
- `ulimit` behavior differs between platforms (e.g., macOS ignores `ulimit -v`)

None of the 12 tests mention platform-specific testing. This is a significant gap because:

- `sandbox-setup.sh` must have platform-specific code paths
- `ulimit -v` (virtual memory limit) is not effective on macOS; the plan does not address this
- `sandbox-exec` on macOS uses Seatbelt profiles (`.sb` files) which have their own syntax and semantics
- Process group behavior (`setsid`, `kill -<pgid>`) differs slightly between Linux and macOS
- File locking semantics (flock vs advisory) differ

**SUGGESTION**:

1. **Platform detection in tests**: Each test should detect the current platform and run platform-appropriate assertions. Example: on macOS, skip the `ulimit -v` enforcement test and instead test the `sandbox-exec` profile.

2. **CI matrix**: If CI is adopted, run the test suite on both Linux (Ubuntu) and macOS runners.

3. **Platform abstraction in sandbox-setup.sh**: Use a function dispatch pattern:
   ```bash
   apply_network_isolation() {
     case "$(uname)" in
       Linux) unshare --net "$@" ;;
       Darwin) sandbox-exec -f "$SANDBOX_PROFILE" "$@" ;;
       *) echo "Unsupported platform" >&2; exit 1 ;;
     esac
   }
   ```
   Each branch is independently testable on its respective platform.

4. **Document known platform limitations**: macOS cannot enforce memory limits via ulimit. The plan should state what happens on macOS: is memory limiting simply skipped? Is there an alternative (e.g., `ulimit -m` for RSS on some macOS versions)?

---

### 13. Are there any components that are inherently hard to test? How should those be handled?

**HARD TO TEST**

The following components are inherently difficult to test:

**A. The SKILL.md orchestrator logic**

The orchestrator is a Claude Code skill -- a natural language document that instructs an LLM on how to behave. Its "logic" cannot be unit tested because it is not executable code. The only way to test it is to run it (expensive) or to review it manually.

Mitigation:
- Extract as much logic as possible into shell scripts (testable).
- Make the skill as thin as possible: it should be a "controller" that calls shell script "services."
- Use the dry-run mode to test the orchestration flow without API costs.
- Maintain a "skill acceptance test" checklist that is manually verified after skill changes.

**B. Claude Code hook integration**

The exact behavior of Claude Code when a PreToolUse hook returns a block decision is an external dependency. If Claude Code changes its hook protocol, the watcher system breaks. This cannot be tested without the claude CLI.

Mitigation:
- Pin the claude CLI version in `blueflame-init.sh` prerequisites.
- Create a minimal "hook smoke test" that verifies the hook protocol still works (one API call).
- Document the expected hook input/output format so changes are detectable.

**C. Beads memory decay behavior**

The memory decay feature is owned by Beads, not by Blue Flame. Testing that old beads are correctly summarized requires either Beads internals knowledge or long-running multi-session tests.

Mitigation:
- Test only the Blue Flame-Beads interface (`beads-archive.sh save` and `load`).
- Trust Beads' own test suite for decay behavior.
- Create a contract test: given beads of age X, verify that `load` returns summarized content (not full content).

**D. Token usage estimation**

`token-tracker.sh` estimates token usage from audit logs. The accuracy of this estimate depends on Claude Code's audit log format, which is an external dependency that may change.

Mitigation:
- Store sample audit log fixtures from known claude CLI versions.
- When the claude CLI is upgraded, regenerate fixtures and verify token-tracker.sh still parses correctly.
- Add a tolerance margin to budget tests (e.g., test that "approximately 80%" triggers the warning).

---

### 14. Is there a test infrastructure plan (test repos, fixtures, mocks)?

**GAP**

The plan mentions no test infrastructure. For a system that operates on git repositories, YAML task files, and generated shell scripts, the following infrastructure is needed:

**A. Test git repositories**

Multiple tests require a git repository (worktree tests, postcheck tests, merger tests, E2E tests). Creating these ad-hoc in each test is fragile and slow.

**SUGGESTION**: Create a `tests/fixtures/repos/` directory with scripts to initialize standardized test repos:
- `create-minimal-repo.sh` -- bare repo with one commit, one file
- `create-multi-file-repo.sh` -- repo with src/, tests/, docs/ structure matching the default blueflame.yaml allowed_paths
- `create-conflict-repo.sh` -- repo with branches that have merge conflicts

Each test calls the appropriate setup script in its `setup()` function and tears down in `teardown()`.

**B. YAML fixtures**

Tests for task claiming, state transitions, dependency resolution, and re-queuing all need tasks.yaml files in various states.

**SUGGESTION**: Create `tests/fixtures/yaml/` with:
- `tasks-pending.yaml` -- all tasks pending
- `tasks-claimed.yaml` -- some tasks claimed
- `tasks-mixed.yaml` -- tasks in various states (pending, claimed, done, failed)
- `tasks-circular-deps.yaml` -- invalid: circular dependencies
- `tasks-all-done.yaml` -- all tasks completed
- `blueflame-minimal.yaml` -- minimal valid config
- `blueflame-full.yaml` -- config exercising all options
- `blueflame-invalid.yaml` -- invalid config (for error handling tests)

**C. Hook input fixtures**

As noted in finding 4, tests for Phase 1 watcher hooks need synthetic PreToolUse payloads.

**SUGGESTION**: Create `tests/fixtures/hook-inputs/` with:
- `write-allowed-path.json` -- Write tool targeting an allowed path
- `write-blocked-path.json` -- Write tool targeting a blocked path
- `bash-allowed-command.json` -- Bash tool with allowed command
- `bash-blocked-command.json` -- Bash tool with blocked pattern match
- `blocked-tool.json` -- Tool not in allowed_tools list
- One file per check type in the watcher

**D. Mock scripts**

Several tests need to simulate external tools (beads CLI, claude CLI).

**SUGGESTION**: Create `tests/mocks/` with:
- `mock-beads.sh` -- simulates `beads save` and `beads load` commands, reads/writes to a temp directory
- `mock-claude.sh` -- simulates `claude` CLI for dry-run testing, returns canned responses
- `mock-process.sh` -- a script that runs for a configurable duration (for lifecycle/timeout testing), optionally allocates memory or attempts network access (for sandbox testing)

---

## Additional Findings

### 15. Test execution time and cost budget

**SUGGESTION**

The plan should establish a testing cost budget:
- Unit tests: zero API cost, < 30 seconds total
- Integration tests: zero API cost, < 2 minutes total
- E2E smoke test: < $1.00 per run, < 10 minutes
- Full E2E suite: < $5.00 per run, < 30 minutes

Without these targets, there is a risk that testing becomes prohibitively expensive or slow, discouraging developers from running tests.

### 16. Audit log testing

**GAP**

Every watcher decision is logged to `.blueflame/logs/<agent-id>.audit.jsonl`. The 12 tests mention checking that "all blocks logged to audit JSONL" (Test 2) but there is no specification of the audit log schema. Without a schema:
- Tests cannot assert on specific log fields.
- Log parsing in `token-tracker.sh` may break on format changes.
- Cross-script log consumption (postcheck reads audit logs, token-tracker reads audit logs) has no contract.

**SUGGESTION**: Define the audit log JSONL schema explicitly. Create a validation function (or script) that checks log entries against the schema. Include schema validation in the test suite.

### 17. Idempotency testing

**GAP**

Several operations should be idempotent but are not tested for idempotency:
- Running `blueflame-init.sh` twice (should not fail or duplicate state)
- Releasing an already-released lock (should be a no-op)
- Cleaning up already-cleaned worktrees (should not error)
- Archiving to Beads when there are no new results (should be a no-op)

**SUGGESTION**: For each idempotent operation, add a test that runs the operation twice and asserts the same outcome.

### 18. Concurrent access to tasks.yaml

**GAP**

The plan describes the orchestrator writing agent IDs to tasks.yaml to claim tasks. If the orchestrator is managing multiple agents, it may need to update tasks.yaml multiple times in quick succession. There is no file-level locking on tasks.yaml itself (the lock-manage.sh locks are for agent file scopes, not for the task file).

This is not strictly a testability issue, but it surfaces during testing: if tests run any parallel operations that touch tasks.yaml, they may encounter race conditions. The plan should specify whether tasks.yaml access is always serialized through the orchestrator (single writer) or requires its own locking.

---

## Summary of Recommendations (Prioritized)

| Priority | Recommendation | Impact |
|----------|---------------|--------|
| P0 | Adopt `bats-core` and create unit tests for all shell script subcommands | Foundational: everything else depends on script correctness |
| P0 | Promote dry-run mode from Phase 5 to Phase 3 | Enables integration testing without API cost |
| P0 | Define audit log JSONL schema and PreToolUse hook input JSON schema | Unblocks fixture creation and contract testing |
| P1 | Create test fixture infrastructure (repos, YAML files, hook inputs, mocks) | Enables all non-E2E testing |
| P1 | Extract task state transitions into a testable shell script (`task-manage.sh`) | Makes YAML state machine deterministic and testable |
| P1 | Add `--auto-approve` / decision-file mode for approval flow testing | Enables automated E2E testing |
| P2 | Add edge case tests (circular deps, empty plans, all-fail waves, etc.) | Prevents production surprises |
| P2 | Establish cross-platform testing approach (CI matrix or manual checklist) | Critical for sandbox correctness on macOS vs Linux |
| P2 | Create regression test framework with snapshot testing for watcher generation | Prevents unintended watcher behavior changes |
| P3 | Define test execution time and cost budgets | Keeps testing practical and sustainable |
| P3 | Add idempotency tests for all setup/teardown operations | Prevents state corruption on retries |

---

## Final Verdict

The Blue Flame plan describes a sophisticated multi-layered system with strong security properties (defense in depth, three-phase enforcement, OS-level sandboxing). However, its testing plan is underweight for the complexity it introduces. The 12 verification tests are acceptance-level checks that validate the happy path of each major subsystem. They do not constitute a testing strategy.

The most critical gaps are:
1. **No unit testing layer** for the 9 shell scripts that form the system's mechanical foundation.
2. **No mock/dry-run infrastructure** for testing orchestration logic without API costs.
3. **No cross-platform testing plan** despite explicit platform-divergent behavior in the sandbox layer.
4. **No regression testing strategy** for a system where shell script bugs have security implications.

The system is designable-for-testability -- most components CAN be tested in isolation with the right infrastructure. The gap is that the plan does not invest in that infrastructure. Adding a test infrastructure phase (fixtures, mocks, test runner) before or concurrent with Phase 1 would dramatically improve confidence in each subsequent phase.
