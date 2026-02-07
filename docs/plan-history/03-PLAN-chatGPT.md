# Multi-Agent Wave Orchestration System

**Low-Resource, Permissioned, Git-Native AI Workflow**

---

## 1. Goals

This system orchestrates a small number of tightly controlled AI agents to perform software tasks in structured “waves,” with strict permission enforcement, human oversight, and minimal hardware/token usage.

### Primary Objectives

* Use **≤ 4 worker agents per wave**
* Enforce **strict action permissions** via watchers
* Keep execution **fully local (no network)**
* Use **Git worktrees + branches** as the unit of isolation
* Use **YAML files as the shared state layer**
* Require **explicit human approvals**
* Prevent **agent drift, scope creep, and zombie processes**
* Optimize for **low token usage and modest hardware**

---

## 2. System Overview

This is **not** an autonomous swarm. It is a **controller-driven assembly line**.

```
Human
  ↓
Controller (deterministic orchestrator)
  ↓
Planner Agent
  ↓ (approval)
Worker Wave (≤4)
  ↓
Validator Wave
  ↓
Merger Agent
  ↓ (approval)
Next Wave or Stop
```

Each agent runs behind a **Watcher**, which enforces hard constraints before, during, and after execution.

---

## 3. Core Roles

### 3.1 Controller (Orchestrator)

**Language:** Go
**Type:** Deterministic system process (NOT an AI agent)

Responsibilities:

* Manage system state machine
* Launch and terminate agents
* Assign tasks and enforce wave limits
* Maintain YAML state files
* Handle lockfiles and conflict prevention
* Enforce timeouts and kill orphaned agents
* Present human approval prompts
* Manage Git worktrees and branches

The controller is the **single source of truth** for system state.

---

### 3.2 Planner Agent

Creates a structured plan from a human task description.

Output:

```yaml
plan:
  - id: T001
    description: "Add input validation to login form"
    cohesion_group: auth_small
  - id: T002
    description: "Add tests for failed login attempts"
    cohesion_group: auth_small
```

The plan must be **explicitly approved by a human** before execution begins.

---

### 3.3 Worker Agents (Wave-Based)

* Max **4 concurrent**
* Claim tasks from a shared YAML file
* Work inside **isolated Git worktrees**
* May run:

  * Tests
  * Linters
  * Formatters
  * Build tools

Workers:

* Modify code
* Commit to their task branch
* Produce structured output summaries

They **do not merge**.

---

### 3.4 Validator Agents

Separate from workers.

They:

* Review worker branches
* Confirm:

  * Task intent satisfied
  * No out-of-scope changes
  * Required tests exist
  * Build/test commands pass
* Produce a **pass/fail verdict**

Validators do not modify code.

---

### 3.5 Merger Agent

Single agent responsible for:

* Combining validated branches
* Performing safe automatic merges
* Avoiding conflict chaos
* Producing a single cohesive changeset

Stops if semantic conflict resolution is required.

---

### 3.6 Watcher (Per Agent)

The **Watcher is mandatory** and runs as a controlling parent process.

It enforces:

| Phase            | Enforcement                                  |
| ---------------- | -------------------------------------------- |
| Before execution | Validate allowed commands, paths, tools      |
| During execution | Restrict filesystem, runtime, resources      |
| After execution  | Diff filesystem, verify only allowed changes |

If any violation occurs → task is rejected.

---

## 4. Execution Model: Waves

Work progresses in **controlled waves**.

### Phase Order

1. **Plan Creation**
2. **Plan Approval (Human Gate)**
3. **Worker Wave**
4. **Validator Wave**
5. **Merger Wave**
6. **Merge Approval (Human Gate)**
7. Repeat or stop

Agents **pause at the end of each wave**.

---

## 5. Filesystem Structure

```
/orchestrator
    controller
    config.yaml
    state.yaml
    locks/

/agents
    planner/
    worker/
    validator/
    merger/

/runs/run_<timestamp>/
    plan.yaml
    tasks.yaml
    approvals.yaml
    logs/

/worktrees/
    task_T001/
    task_T002/
    merge_wave_01/
```

Everything is persisted to disk to avoid memory-heavy coordination.

---

## 6. Task Board (Shared YAML)

`tasks.yaml`

```yaml
tasks:
  - id: T001
    description: "Add input validation to login form"
    cohesion_group: auth_small
    claimed_by: null
    status: pending
    branch: null
    workspace: null
```

### Status Lifecycle

```
pending → claimed → completed → validated → merged
```

Agents claim a task by atomically writing their ID.

---

## 7. Git-Native Isolation

Each task gets:

* A dedicated **git worktree**
* A dedicated **task branch**

```
agent/T001
agent/T002
merge/wave_01
```

Agents are only allowed to write within their assigned worktree.

This provides:

* Natural diff tracking
* Easy merge control
* Clean rollback capability

---

## 8. Locking System

Lockfiles prevent collisions.

| Lock                 | Purpose                  |
| -------------------- | ------------------------ |
| `tasks.yaml.lock`    | Prevent double-claiming  |
| `worktree_T001.lock` | Prevent multiple writers |
| `merge.lock`         | Protect merge operations |

Stale locks are cleared by the controller using PID + timeout checks.

---

## 9. Watcher Enforcement Model

### Pre-Execution Checks

* Command allowlist
* Path allowlist
* Tool usage policy
* No network allowed

### Runtime Constraints

* Process time limit
* CPU/memory limits (ulimit)
* Restricted working directory
* No outbound network namespace

### Post-Execution Verification

* Filesystem diff
* Confirm only permitted paths changed
* Validate output structure

Watcher failures immediately halt task progression.

---

## 10. Human Approval Gates

### Plan Approval

After planning:

```
Approve plan? (y/n/details)
```

### Merge Approval

Controller presents:

* Summary of all merged tasks
* Diff statistics
* Risk notes

Human may:

* Approve all
* Reject
* Approve partial

No automatic promotion without approval.

---

## 11. Agent Lifecycle Protection

Controller tracks:

```yaml
agents:
  AGENT_WORKER_01:
    pid: 4421
    start_time: ...
    timeout_minutes: 20
```

Controller:

* Kills over-time agents
* Reclaims stale locks
* Resets tasks to pending if agent dies

Prevents zombie agents and runaway processes.

---

## 12. Permissions Configuration

`config.yaml`

```yaml
permissions:
  writable_roots:
    - worktrees/
  readable_roots:
    - repo/
  forbidden_paths:
    - .git/
    - /etc
  allow_network: false
  max_runtime_minutes: 20
```

This file is loaded by both controller and watchers.

---

## 13. Resource & Token Efficiency

* Workers only receive **task-relevant files**
* No full-repo prompts unless required
* Validators rely heavily on tooling, not LLM tokens
* Agents are **not persistent** between waves

---

## 14. Technology Stack

| Component     | Language                  |
| ------------- | ------------------------- |
| Controller    | Go                        |
| Watcher       | Go                        |
| Agents        | Python                    |
| State Storage | YAML files                |
| Isolation     | Git worktrees + OS limits |

---

## 15. Implementation Plan

### Phase 1 – Foundation

* Build controller skeleton with state machine
* Implement YAML state handling
* Implement lockfile system
* Add Git worktree automation

### Phase 2 – Watcher System

* Build watcher wrapper process
* Implement pre-execution policy checks
* Add post-execution filesystem diff validation
* Add runtime limits

### Phase 3 – Agent Framework

* Build shared Python agent runtime
* Implement Planner agent
* Implement Worker agent (tool-running enabled)
* Implement Validator agent
* Implement Merger agent

### Phase 4 – Wave Orchestration

* Implement worker wave scheduling
* Implement validator wave transition
* Implement merger wave
* Add timeout and zombie cleanup

### Phase 5 – Human Gates

* CLI approval prompts
* Plan review display
* Merge summary display

### Phase 6 – Hardening

* Failure recovery paths
* Partial wave restarts
* Stale lock cleanup
* Logging and audit trail

---

## 16. Key Design Properties

✔ Deterministic controller
✔ Minimal concurrent agents
✔ Hard permission enforcement
✔ Git-native isolation
✔ Human oversight at critical boundaries
✔ No network dependency
✔ Low hardware overhead
