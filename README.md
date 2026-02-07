# Blue Flame

Wave-based multi-agent orchestration for AI-assisted software development.

## Why "Blue Flame"?

The name nods to Steve Yegge's [Gastown](https://github.com/stevey-xgit/gastown) — both projects channel gas into productive work. Where Gastown lights the furnace and lets it roar, Blue Flame turns the knob to the precise setting.

A blue flame means efficient combustion. No wasted fuel, no excess soot, no billowing orange fireball. It burns hotter with less gas. That's the design philosophy: respect the user's hardware, respect their token budget, and respect their authority over what the agents do.

Gastown proved that multi-agent orchestration works. Blue Flame asks: what if the human stayed in the driver's seat? What if you directed a small, disciplined team — four workers at most — with strict permissions, wave-based execution, and mandatory human checkpoints between phases? What if a watcher enforced the rules on every agent, every task ran in isolation, and every changeset required approval before it touched the main branch?

This project grew from real constraints: a Mac M3 Pro with 18GB of memory and a basic Claude Pro plan. No enterprise API budget, no beefy cloud VM — just a laptop and a subscription. That's why Blue Flame puts the human in control of how many agents run, how much context they consume, and when they stop. User-guided restrictions aren't a limitation; they're the point.

Less gas. More heat. That's Blue Flame.

## Overview

Blue Flame orchestrates AI agents through a structured wave cycle:

1. **Plan** — A planner agent decomposes your task into isolated, lockable units of work. You review and approve the plan.
2. **Develop** — Up to four worker agents execute tasks in parallel, each in its own git worktree, each watched by an enforcer of your permission config.
3. **Validate** — Validator agents review each worker's output against the task spec and run tests.
4. **Merge** — A merger agent combines validated changes into cohesive changesets that you approve one by one.

Then repeat.

**Key dependencies**: Go (orchestrator), `claude` CLI (agents), [Superpowers](https://github.com/anthropics/superpowers) plugin (skills), git worktrees (isolation).

See [PLAN.md](PLAN.md) for the full architecture and implementation details.
