# E2E Test: Parallel Workers (2 Tasks, 2 Workers)

This test validates the golden path with parallel agent execution. Two independent tasks run simultaneously in separate git worktrees, then validate and merge.

## What This Test Covers

- Planner decomposes a task into 2 independent sub-tasks
- Scheduler assigns both tasks concurrently (`development: 2`)
- Each worker runs in its own git worktree and branch
- PostCheck verifies commits exist on each branch
- Validators review each task independently
- Merger combines both branches into `main`
- Worktrees and feature branches are cleaned up after merge

## Prerequisites

Same as [e2e-walkthrough.md](e2e-walkthrough.md): Go toolchain, `claude` CLI, `jq`, and Git configured.

## 1. Build the Binary

```bash
cd /path/to/blueflame
go build -o blueflame ./cmd/blueflame
```

## 2. Create a Scratch Repo

```bash
export BF_TEST=/tmp/bf-test
rm -rf "$BF_TEST"
mkdir -p "$BF_TEST"
cd "$BF_TEST"

git init
git commit --allow-empty -m "Initial commit"

mkdir -p scripts
cat > README.md << 'EOF'
# Shell Utilities

A collection of simple shell utility scripts.
EOF

git add README.md
git commit -m "Add README"
```

## 3. Create the Config

Key settings for parallel execution:

- `concurrency.development: 2` -- two workers run simultaneously
- `concurrency.validation: 2` -- both validators can also run in parallel
- `concurrency.adaptive: false` -- don't reduce workers based on available RAM
- `planning.interactive: false` -- planner runs in `--print` mode (non-interactive)

```bash
cat > blueflame.yaml << 'YAML'
schema_version: 1

project:
  name: "shell-utils"
  repo: "/tmp/bf-test"
  base_branch: "main"
  worktree_dir: ".trees"
  tasks_file: ".blueflame/tasks.yaml"

concurrency:
  planning: 1
  development: 2
  validation: 2
  merge: 1
  adaptive: false

limits:
  agent_timeout: 300s
  max_retries: 1
  max_wave_cycles: 2
  max_session_cost_usd: 10.00
  token_budget:
    planner_usd: 1.00
    worker_usd: 2.00
    validator_usd: 0.50
    merger_usd: 1.00

planning:
  interactive: false

models:
  planner: "sonnet"
  worker: "sonnet"
  validator: "haiku"
  merger: "sonnet"

permissions:
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
    - "Task"
    - "NotebookEdit"

validation:
  file_scope:
    enforce: false
YAML
```

## 4. Create the Decisions File

```bash
cat > decisions.txt << 'EOF'
# Plan approval
approve
# Changeset approvals (one per cohesion group)
changeset-approve
changeset-approve
changeset-approve
# Session continuation
continue
stop
EOF
```

## 5. Copy the Binary and Run

```bash
cp /path/to/blueflame/blueflame "$BF_TEST/blueflame"
cd "$BF_TEST"

./blueflame \
  --config blueflame.yaml \
  --decisions-file decisions.txt \
  --task "Create two independent shell utility scripts in the scripts/ directory: (1) scripts/colors.sh - a script that demonstrates printing text in different terminal colors using ANSI escape codes, with functions for red, green, blue, yellow, and a demo that shows all colors, and (2) scripts/sysinfo.sh - a script that displays system information including hostname, OS, kernel version, uptime, disk usage, and memory usage. Each script should be executable, well-commented, and independently testable. These are completely independent tasks with no shared code."
```

## Expected Behavior

### Planning Phase

The planner produces 2 tasks, both priority 1, with no dependencies:

```
Planned 2 task(s), estimated cost: $1.00 - $6.00

  1. [task-001] Create scripts/colors.sh utility script (priority 1)
     deps: none | locks: scripts/, scripts/colors.sh
  2. [task-002] Create scripts/sysinfo.sh system information script (priority 1)
     deps: none | locks: scripts/, scripts/sysinfo.sh
```

No dependencies means both tasks are eligible for parallel scheduling.

### Development Phase

The scheduler selects both tasks (up to `development: 2`). Each gets:

1. A unique agent ID (e.g. `worker-7bb433c8`, `worker-de340f80`)
2. Its own git worktree under `.trees/<agent-id>`
3. Its own branch (`blueflame/task-001`, `blueflame/task-002`)
4. File locks acquired (non-overlapping, so no conflicts)

Both `claude` processes run simultaneously via goroutines and `exec.Command`. Results are collected on a channel.

### Validation Phase

After both workers complete, PostCheck runs on each branch to verify commits exist. Then validators review each task's changes.

### Merge Phase

Tasks are grouped into a changeset (both in the "default" cohesion group). The merger agent merges both branches into `main`. After merge, worktrees and feature branches are removed.

## 6. Verify

```bash
# Two feature commits merged onto main
git log --oneline
# Expected:
#   <hash> feat(task-002): Create system information display script
#   <hash> feat(task-001): Create colors.sh utility script
#   <hash> Add README
#   <hash> Initial commit

# Feature branches cleaned up
git branch -a
# Expected: only "* main"

# Worktrees cleaned up
ls .trees/
# Expected: empty directory

# Only main worktree remains
git worktree list
# Expected: /tmp/bf-test  <hash> [main]

# Scripts exist and are executable
ls -la scripts/
# Expected: colors.sh and sysinfo.sh, both -rwxr-xr-x

# Scripts run correctly
./scripts/colors.sh
./scripts/sysinfo.sh

# Both tasks show "merged" status with "pass" validation
grep -A2 'status:' .blueflame/tasks.yaml
```

### Confirming Parallel Execution

Check that both workers had different agent IDs (spawned as separate processes):

```bash
grep 'agent_id:' .blueflame/tasks.yaml
# Expected:
#   agent_id: worker-XXXXXXXX
#   agent_id: worker-YYYYYYYY   (different from above)
```

## Actual Test Run (2026-02-07)

| Metric | Value |
|--------|-------|
| Duration | 4m 6s |
| Total cost | $0.62 |
| Tokens | 12,101 |
| Tasks planned | 2 |
| Tasks merged | 2 |
| Tasks failed | 0 |
| Wave cycles | 2 |
| Workers (parallel) | 2 |

Session summary output:

```
=== Session Summary ===
Session:    ses-20260207-195219
Duration:   4m6s
Waves:      2

Tasks:
  Completed: 0
  Merged:    2
  Failed:    0

Cost:
  Total:     $0.6214
  Limit:     $10.00 (6.2% used)
  Tokens:    12101
=======================
```

## Differences from the Single-Task Walkthrough

| Aspect | [e2e-walkthrough](e2e-walkthrough.md) | This test |
|--------|---------------------------------------|-----------|
| Tasks | 1 (single script) | 2 (independent scripts) |
| Workers | 1 (serial) | 2 (parallel) |
| Concurrency config | `development: 2` (only 1 task to schedule) | `development: 2` (both tasks scheduled) |
| Adaptive concurrency | default (on) | `adaptive: false` (fixed at 2) |
| Planning mode | interactive | `interactive: false` |
| File lock overlap | n/a | None (separate directories) |
| Validation | 1 validator | 2 validators |
| Merge | 1 branch | 2 branches into `main` |
