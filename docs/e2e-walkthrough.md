# End-to-End Walkthrough: Running Blue Flame on a Sample Project

This guide reproduces the working test at `/tmp/bf-test` from scratch. By the end you will have a tiny Git repo, a `blueflame.yaml`, a scripted-decisions file, and a successful plan-develop-validate-merge cycle.

## Prerequisites

- Go toolchain (1.25+)
- `claude` CLI installed and authenticated (`claude --version`)
- `jq` installed (used by watcher hooks)
- Git configured (`user.name` / `user.email`)

## 1. Build the Binary

```bash
cd /path/to/blueflame          # the blueflame source repo
go build -o blueflame ./cmd/blueflame
```

## 2. Create a Scratch Repo

```bash
export BF_TEST=/tmp/bf-test    # change to wherever you like
rm -rf "$BF_TEST"
mkdir -p "$BF_TEST"
cd "$BF_TEST"

git init
git commit --allow-empty -m "Initial commit"
```

Create a seed file so the repo isn't totally bare:

```bash
cat > README.md << 'EOF'
# US Timezones

A shell script that displays US timezone information.
EOF

git add README.md
git commit -m "Initial commit"
```

## 3. Create the Config

```bash
cat > blueflame.yaml << 'YAML'
schema_version: 1

project:
  name: "us-timezones"
  repo: "/tmp/bf-test"
  base_branch: "main"
  worktree_dir: ".trees"
  tasks_file: ".blueflame/tasks.yaml"

concurrency:
  development: 2
  validation: 1

limits:
  max_retries: 1
  max_wave_cycles: 5
  max_session_cost_usd: 5.00
  token_budget:
    planner_usd: 0.50
    worker_usd: 1.50
    validator_usd: 0.30
    merger_usd: 0.50

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
YAML
```

Adjust `project.repo` if you chose a different directory.

## 4. Create a Decisions File

The decisions file feeds pre-scripted answers to every human gate so the run is fully automated. Each line corresponds to one prompt, consumed in order:

```bash
cat > decisions.txt << 'EOF'
# Plan approval
approve
# Changeset approval
changeset-approve
changeset-approve
changeset-approve
# Session decisions
continue
continue
stop
EOF
```

**How many lines do you need?**

| Decision type | When consumed |
|---|---|
| `approve` | After the planner produces a task plan |
| `changeset-approve` | Once per changeset in the merge phase (one per cohesion group) |
| `continue` / `stop` | After each wave cycle completes, to continue or end |

Provide more `changeset-approve` and `continue` lines than you expect to need; extras are ignored. The final `stop` ends the session.

## 5. Copy the Binary

```bash
cp /path/to/blueflame/blueflame "$BF_TEST/blueflame"
```

## 6. Run It

```bash
cd "$BF_TEST"
./blueflame \
  --config blueflame.yaml \
  --decisions-file decisions.txt \
  --task "Create a shell script called us_timezones.sh that prints a formatted table of all US timezones with their UTC offsets and a representative city for each"
```

Blue Flame will:

1. **Plan** -- the planner agent decomposes the task (here, a single sub-task)
2. **Develop** -- a worker agent creates `us_timezones.sh` in an isolated worktree, commits the result
3. **Validate** -- a validator agent reviews the diff
4. **Merge** -- after `changeset-approve`, a merger agent merges the branch into `main`
5. **Session end** -- `stop` from the decisions file terminates the session

## 7. Verify

```bash
# The script should exist on main
git log --oneline
cat us_timezones.sh
bash us_timezones.sh

# Task store shows "merged" status
cat .blueflame/tasks.yaml
```

Expected output from `us_timezones.sh`:

```
Timezone             UTC Offset      City
--------             ----------      ----
Eastern (EST/EDT)    UTC-5/-4        New York
Central (CST/CDT)    UTC-6/-5        Chicago
Mountain (MST/MDT)   UTC-7/-6        Denver
Pacific (PST/PDT)    UTC-8/-7        Los Angeles
Alaska (AKST/AKDT)   UTC-9/-8        Anchorage
Hawaii (HST)         UTC-10          Honolulu
```

## 8. Clean Up

```bash
./blueflame cleanup --config blueflame.yaml
# Or just:
rm -rf "$BF_TEST"
```

## Customizing the Test

### Different task

Change the `--task` argument to anything you like. Adjust `decisions.txt` if the planner produces multiple tasks (add more `changeset-approve` lines).

### Interactive mode

Omit `--decisions-file` to get interactive prompts at each gate where you can approve, reject, edit, or re-plan.

### Dry run

```bash
./blueflame --dry-run --config blueflame.yaml --task "..."
```

Prints configuration and budget info without spawning agents.

### Larger projects

For real repos, point `project.repo` at your checkout, tighten `permissions.allowed_paths` / `blocked_paths`, and add `bash_rules` and `validation` sections. See [docs/user-guide.md](user-guide.md) for the full config reference.

## Troubleshooting

| Symptom | Fix |
|---|---|
| `error: branch 'blueflame/task-001' already exists` | Run `./blueflame cleanup` or `git branch -D blueflame/task-001` before rerunning |
| Worker produces no commits | The worker system prompt requires explicit `git add && git commit`. Check `.blueflame/hooks/` logs for blocked tool calls |
| `jq: command not found` | Install jq (`brew install jq` on macOS) |
| Lock conflicts defer all tasks | Reduce `concurrency.development` to 1, or narrow `file_locks` in the plan |
| Session exceeds budget | Lower `max_session_cost_usd` or per-role budgets |
