# Budget & Cost Review: Blue Flame Multi-Agent Orchestration System

**Reviewer**: Cost Optimization & Budget Management Reviewer
**Document Reviewed**: Plan-ADR.md (Hybrid Plan)
**Date**: 2026-02-07

---

## Cost Risk Summary

| Risk Level | Count | Summary |
|------------|-------|---------|
| COST-EFFECTIVE | 6 | Haiku validators, shell mechops, two-tier validation, focused prompts, per-agent budgets, Beads decay |
| CONCERN | 5 | Cost estimate accuracy, token tracking reliability, orchestrator hidden overhead, prompt caching gaps, merger budget |
| RISK | 4 | Retry cost escalation, complex merge ballooning, dependency chain cascading, token budget kill accuracy |
| SUGGESTION | 5 | Prompt caching strategy, dynamic model selection, session-level circuit breaker, batch API for validators, cost observability dashboard |

**Overall Assessment**: The plan demonstrates strong cost awareness and is directionally sound. The ~$0.78 estimate for a 3-task session is plausible for a happy-path scenario but likely understates real-world costs by 2-5x when accounting for retries, orchestrator overhead, tool definition tokens, and edge cases. The biggest risk is unbounded retry loops and complex merges silently ballooning costs. The plan needs explicit session-level cost caps and better cost observability to be production-safe.

---

## Detailed Findings

### 1. Is the cost profile estimate (~$0.78 per 3-task session) realistic?

**CONCERN**: The estimate is optimistic and likely understates real-world costs.

The plan estimates the following for a 3-task session:

| Agent | Model | Count | Est. Cost |
|-------|-------|-------|-----------|
| Planner | Sonnet | 1 | ~$0.15 |
| Workers | Sonnet | 3 | ~$0.45 (~$0.15 each) |
| Validators | Haiku | 3 | ~$0.03 (~$0.01 each) |
| Merger | Sonnet | 1 | ~$0.15 |

Using current Sonnet 4.5 pricing ($3/$15 per million input/output tokens), a $0.15 Sonnet session implies roughly:
- If 80% input / 20% output: ~40K input tokens + ~8K output tokens = $0.12 + $0.12 = $0.24, which already exceeds $0.15
- If the estimate assumes 50K input tokens only: $0.15 is correct for input but ignores output tokens entirely
- A realistic worker session doing TDD on a moderately complex task (reading files, writing code, running tests, iterating) would likely consume 60-100K input tokens and 15-30K output tokens, putting per-worker cost at $0.18 - $0.75

The $0.78 estimate appears to account for only input tokens or assumes very small tasks. A more realistic estimate for a 3-task session with moderate complexity:

| Agent | Realistic Low | Realistic High |
|-------|--------------|----------------|
| Planner | $0.20 | $0.40 |
| Workers (3x) | $0.60 | $2.25 |
| Validators (3x) | $0.03 | $0.09 |
| Merger | $0.20 | $0.50 |
| **Total** | **$1.03** | **$3.24** |

The happy-path cost is likely $1-2, not $0.78. With retries and re-queues, $2-5 per session is realistic. This is still excellent compared to alternatives, but the plan should present a range rather than a single-point estimate to set accurate expectations.

---

### 2. Are the model choices per role optimal for cost?

**COST-EFFECTIVE**: The tiered model selection is well-reasoned.

- **Sonnet for planner**: Appropriate. Planning requires strong reasoning and decomposition. Using Haiku here would risk poor task breakdowns that cause downstream failures (much more expensive than the planner cost difference).
- **Sonnet for workers**: Appropriate. Code generation quality directly impacts success rate. Cheaper models produce more bugs, more failed validations, and more retries -- all of which cost more than the Sonnet premium.
- **Haiku for validators**: Excellent choice. Code review against a focused diff with clear criteria is well within Haiku's capabilities. At $1/$5 per million tokens vs. Sonnet's $3/$15, this is a 3x savings on input and 3x on output. For a validation task that mostly reads a diff and writes a short verdict, Haiku is sufficient.
- **Sonnet for merger**: Appropriate. Merge conflict resolution requires context understanding. However, see Finding #9 regarding merger cost risk.

**SUGGESTION**: Consider using Haiku for the planner in a "draft + refine" pattern: Haiku generates an initial plan, a lightweight Sonnet pass refines it. This could save 30-50% on planning costs for straightforward tasks. The plan already has human approval as a gate, so a slightly lower-quality initial plan is acceptable.

---

### 3. Is the token budget enforcement mechanism (warn at 80%, kill at 100%) practical and reliable?

**CONCERN**: The mechanism is sound in principle but has latency and accuracy issues.

The plan specifies per-agent token budgets:
- Worker: 100,000 tokens
- Validator: 30,000 tokens
- Planner: 50,000 tokens
- Merger: 80,000 tokens

Practical issues:

1. **Polling lag**: The `token-tracker.sh` script must parse audit logs to estimate usage. This is necessarily a polling mechanism, not real-time. If an agent makes a large request (e.g., reading a large file, generating a long code block), it could blow through the remaining 20% budget in a single API call before the tracker runs again. The heartbeat interval is 30 seconds -- an agent can consume 20-50K tokens in 30 seconds.

2. **Kill granularity**: Killing an agent at 100% means the task is marked `failed` and potentially retried. The retry itself costs tokens against the *same* budget category. The plan does not clarify whether retries get a fresh token budget or share the original budget. If retries get fresh budgets, the effective cap is `token_budget * (1 + max_retries)` = 300K tokens per task, which is 3x the stated budget.

3. **Pre-Tool hook approach**: The watcher hook is invoked via `PreToolUse`, which means it runs *before* each tool call. This is actually a better enforcement point than polling -- the hook can estimate whether the next call will exceed the budget based on cumulative usage. However, the hook itself does not know the cost of the *upcoming* call, only the cost incurred so far. A call that reads a 5,000-line file will consume tokens that the hook cannot predict.

4. **Budget sizing**: The 100K worker budget is reasonable for a focused task. However, TDD workflows (write test, run test, write code, run test, iterate) can consume tokens quickly due to repeated tool calls. If a worker needs 3-4 iterations to get tests passing, 100K tokens may be tight, leading to frequent budget kills and retries.

**SUGGESTION**: Add a "soft reserve" mechanism: at 80% warn AND reduce the agent's available tools (e.g., disable Read for large files, switch to targeted Grep). At 90%, send a "wrap up now" instruction. This gives the agent a chance to commit partial progress rather than being killed with nothing to show.

---

### 4. How accurate can token tracking from audit logs actually be?

**CONCERN**: Tracking is feasible but has meaningful accuracy gaps.

Claude Code stores conversation data in JSONL files with fields like `input_tokens`, `cache_creation_input_tokens`, and `cache_read_input_tokens`. Third-party tools like `ccusage` demonstrate that parsing these logs for token data is viable.

Accuracy issues:

1. **Log write delay**: JSONL entries are written after each API response. If the tracker polls between calls, it sees stale data. The gap could be 5-15 seconds per call.

2. **Cached tokens**: Claude Code uses prompt caching by default. Cached input tokens cost 0.1x normal price. If the tracker counts raw token counts without distinguishing cached vs. uncached, cost estimates will be wrong. A session with heavy caching could show 100K "input tokens" but only cost what 20K uncached tokens would cost.

3. **Tool definition overhead**: Each agent session includes tool definitions in context. With the allowed tools (Read, Write, Edit, Glob, Grep, Bash), plus the watcher hook, this is roughly 5-15K tokens of overhead per API call that gets cached after the first call. The tracker needs to account for this.

4. **Deduplication**: Message IDs must be used for deduplication to avoid double-counting. The plan does not mention this.

5. **Output vs. input distinction**: Output tokens cost 5x more than input tokens for Sonnet ($15 vs. $3 per million). A tracker that counts total tokens without distinguishing input from output will produce inaccurate cost estimates. The plan's budgets are in raw token counts, not cost-weighted.

**SUGGESTION**: Express budgets in estimated *cost* (e.g., $0.20 per worker) rather than raw token counts. This accounts for the asymmetric pricing of input vs. output tokens and cache discounts. The tracker should compute `(uncached_input * input_price) + (cached_input * 0.1 * input_price) + (output * output_price)` and compare against a dollar budget.

---

### 5. Does the Beads memory decay actually save meaningful tokens across sessions?

**COST-EFFECTIVE**: Yes, this is a genuinely valuable cost-saving mechanism.

Cross-session context is one of the biggest hidden costs in multi-agent systems. Without memory decay:
- Session 1 produces 5K tokens of context
- Session 2 receives 5K + produces 5K = 10K cumulative
- Session 10 receives 45K of prior context before doing any work

With memory decay (summarizing old entries), the growth is sub-linear. A well-designed decay function could keep cross-session context at 5-15K tokens regardless of session count. At Sonnet pricing, this saves roughly:
- Session 10 without decay: 45K input tokens = $0.135 in context cost alone
- Session 10 with decay: 10K input tokens = $0.03 in context cost
- Savings: ~$0.10 per session, growing with session count

Over a project with 50+ sessions, this compounds to $5-10 in savings. Not enormous in absolute terms, but proportionally significant when sessions cost $1-3 each.

The real value is not just token cost but also quality: smaller context means less noise, better planner focus, and fewer confused agents -- all of which reduce downstream retry costs.

**CONCERN**: The plan relies on Beads' "built-in memory decay feature" but does not specify the decay policy. Aggressive decay loses valuable failure context. Conservative decay barely saves tokens. The optimal policy depends on the project and should be configurable. The plan should specify a default decay strategy (e.g., "summarize closed tasks older than 3 sessions to a single line each; preserve failure context for 5 sessions").

---

### 6. Are the shell helpers truly "zero token cost" or is there hidden overhead?

**CONCERN**: Shell helpers are low cost but not truly zero cost.

The plan correctly identifies that shell scripts for mechanical operations (worktree management, lock management, sandbox setup) avoid consuming tokens on deterministic work. This is one of the plan's strongest cost optimizations.

However, "zero token cost" is misleading:

1. **Orchestrator context growth**: Every time the orchestrator calls a shell helper via the Bash tool, the command and its output become part of the orchestrator's conversation context. Over a 3-task session with 4 waves, the orchestrator might make 30-50 Bash calls (init, create 3 worktrees, acquire 3 lock sets, generate 3 watchers, setup 3 sandboxes, register 3 agents, 3+ heartbeat cycles, 3 post-checks, release 3 locks, cleanup). Each call adds ~200-500 tokens of command + output to the orchestrator context. That is 6K-25K tokens of orchestrator context from "zero cost" helpers.

2. **Task tool dispatch overhead**: Each `Task` tool call to spawn an agent includes the full prompt template in the orchestrator's context. With 8 agents (1 planner + 3 workers + 3 validators + 1 merger), this is 8 Task calls, each with a prompt of 500-2000 tokens, adding 4K-16K tokens to orchestrator context.

3. **Orchestrator is the parent context**: The plan lists orchestrator cost as "minimal (shell helpers do mechops)" but the orchestrator is a Claude Code session itself. By the end of a wave cycle, its context could be 50-100K tokens, costing $0.15-$0.30 in Sonnet input alone, plus output tokens for its coordination logic.

**SUGGESTION**: The cost table should include an explicit orchestrator row. Estimated orchestrator cost for a 3-task session: $0.20-$0.50. This raises the realistic total from $1.03-$3.24 to $1.23-$3.74.

---

### 7. Does the two-tier validation reduce cost compared to a single-tier approach?

**COST-EFFECTIVE**: Yes, meaningfully.

The two-tier approach is:
- **Tier 1 (mechanical)**: Shell-based watcher hooks + post-execution diff checks. Deterministic, runs during Wave 2, catches formatting, path violations, scope violations. True zero incremental token cost (runs in the agent's existing process).
- **Tier 2 (semantic)**: Haiku-based validator agents checking correctness, applicability, regressions. Costs ~$0.01 per validation.

Alternative: single-tier LLM validation doing both mechanical and semantic checks.
- A single Sonnet validator doing everything: ~$0.10-$0.20 per validation
- 3 validations: $0.30-$0.60

The two-tier approach costs: ~$0.00 (Tier 1) + ~$0.03 (Tier 2) = ~$0.03 total for 3 validations. This is a **10-20x cost reduction** compared to single-tier Sonnet validation.

Additionally, Tier 1 catches many issues *before* the agent finishes, meaning the agent can self-correct during execution rather than failing in a separate validation pass. This avoids the cost of a re-queued task entirely for mechanical violations.

The only concern is that Tier 1 pre-tool hooks add ~50-100ms latency per tool call. Over hundreds of tool calls per agent, this could add 5-10 seconds of wall time. This is not a cost issue but a throughput consideration.

---

### 8. Are there hidden costs not accounted for?

**RISK**: Several cost categories are unaccounted for.

1. **Retry costs**: The plan allows `max_retries: 2` per task. A failed task that retries twice means 3x the worker cost for that task. With 3 workers, worst case is 9 worker sessions instead of 3, tripling the worker budget from $0.45 to $1.35 (plan estimate) or $0.60-$2.25 to $1.80-$6.75 (realistic estimate). The plan's cost table does not include retries.

2. **Re-queued tasks from rejected changesets**: When a human rejects a changeset in Wave 4, the task is re-queued for the next wave cycle. This means the task goes through Worker + Validator + Merger again -- a full second pass. The plan does not account for re-queue costs. If 1 of 3 tasks is re-queued, it adds ~$0.30-$1.00 to the session.

3. **Failed validation re-work**: If a validator marks a task as `fail`, the human can retry it. This means another Worker + Validator cycle. Not the same as a retry (which is automatic from crashes/timeouts), this is a quality failure requiring re-execution.

4. **Planner iteration cost**: The plan says "Human approves, edits, or rejects. Loop until approved." Each rejection and re-generation of the plan costs another Planner session ($0.20-$0.40). If the human rejects the plan twice, that is $0.40-$0.80 in planning alone.

5. **Context window overflow**: If an agent's context fills up, Claude Code may automatically summarize or truncate, potentially causing the agent to lose important context and produce poor results -- leading to validation failures and retries.

6. **Watcher hook execution cost**: Each PreToolUse hook invocation runs a shell script with a 5-second timeout. While the token cost is zero, there is CPU time cost, and if the watcher script is slow (e.g., due to regex matching against many patterns), it could cause agent timeouts more frequently, triggering retries.

**SUGGESTION**: Add a "cost contingency multiplier" to the estimate. Present the cost as: `base_estimate * 1.5` for typical sessions and `base_estimate * 3.0` for sessions with retries/re-queues. The plan approval screen showing "Estimated cost: ~$0.78" should show a range: "Estimated cost: $0.78 - $2.34 (depending on retries)".

---

### 9. Could the cost profile balloon in edge cases?

**RISK**: Yes, several scenarios can cause 5-10x cost escalation.

**Scenario A: Retry Storm**
- 3 tasks, each fails twice before succeeding
- 9 worker sessions + 9 validator sessions + 1 merger
- Estimated cost: $1.35-$6.75 (workers) + $0.09 (validators) + $0.20-$0.50 (merger) = $1.64-$7.34
- vs. base estimate of $0.78 -- a **2-9x increase**

**Scenario B: Complex Merge Conflicts**
- 3 tasks with cross-cutting changes
- Merger encounters semantic conflicts, reports them, human resolves and re-runs merger
- Multiple merger iterations: 3 merger sessions at $0.20-$0.50 each = $0.60-$1.50
- Plus the human's time cost to understand and guide resolution
- The merger has an 80K token budget which is the second-highest. A complex merge with many diffs could consume this quickly.

**Scenario C: Large Codebase Context**
- Workers need to Read many files to understand context
- Each file read adds tokens to the agent's context
- A worker in a large codebase might spend 50K tokens just on file reads before writing any code
- This halves the effective budget for actual work, increasing the likelihood of budget kills and retries

**Scenario D: Dependency Chain Cascade**
- task-002 depends on task-001
- task-001 fails, gets retried, fails again -- exhausts max_retries
- task-002 can never run because its dependency failed
- But the tokens spent on task-001's 3 attempts are consumed with no deliverable
- If this cascades through a dependency chain of 5 tasks, only the first task's retries consume tokens but all downstream tasks are blocked

**Scenario E: Re-queue Loop**
- Human rejects a changeset
- Task is re-queued with history context (which grows each iteration)
- Worker receives growing context from prior failures, consuming more tokens each attempt
- History context is unbounded in the plan -- no max history size is specified

**SUGGESTION**: Add a session-level cost circuit breaker: "If total session cost exceeds $X, pause and require human approval to continue." This prevents all runaway scenarios regardless of cause. A sensible default might be 5x the initial estimate.

---

### 10. Are there additional cost-saving opportunities the plan misses?

**SUGGESTION**: Five opportunities identified.

**A. Prompt Caching Strategy**
Claude Code uses prompt caching by default, with cache reads costing 0.1x normal input price. The plan does not explicitly discuss caching strategy. For multi-agent orchestration, cache-friendly prompt design could yield significant savings:
- Shared system prompt prefix across all agents of the same role would hit cache
- Tool definitions are already cached, but the plan should ensure consistent tool ordering across agents
- Estimated saving: 20-40% on input token costs across all agents if prompts are cache-optimized

**B. Batch API for Validators**
Validators do not need real-time results. The plan could use Claude's Batch API for validation, which offers a 50% discount on both input and output tokens. All 3 validators could be submitted as a batch after Wave 2 completes. This would reduce validation costs from ~$0.03 to ~$0.015. The absolute savings are small, but it is essentially free money. Note: this only works if the validators are called via the API directly rather than via `claude` CLI Task tool.

**C. Dynamic Model Selection Based on Task Complexity**
Not all tasks are equal. A simple "update API docs" task could use Haiku as the worker model, while a complex "implement JWT middleware with TDD" task needs Sonnet. The planner could annotate each task with a `complexity` field (low/medium/high), and the orchestrator could select the worker model accordingly. Estimated saving: 30-50% on worker costs for projects with a mix of simple and complex tasks.

**D. Early Termination for Obvious Failures**
If a worker fails its first test run and the error is a compilation error, the watcher could detect this pattern and suggest early termination rather than letting the agent continue burning tokens on a fundamentally broken approach. The plan has no mechanism for "smart early termination" beyond the blunt token budget kill.

**E. Incremental Diff for Validator Context**
The plan sends validators `git diff main...<branch>`, which could be large for tasks that touch many files. Sending only the changed hunks relevant to the task's `file_locks` scope would reduce validator input tokens. For a Haiku validator with a 30K budget, every token saved on diff input is a token available for reasoning.

---

### 11. How does this compare to alternatives?

**COST-EFFECTIVE**: Blue Flame is dramatically cheaper than alternatives.

| System | Monthly Cost (est.) | Cost Per Feature | Notes |
|--------|-------------------|-----------------|-------|
| **Gastown** | $1,000-$3,000+ | $10-50+ | 20-30 parallel agents, $100/hr burn rate reported, high waste from lost/duplicate work |
| **Blue Flame** | $50-150 (est. 50-100 sessions) | $1-5 | 4 max parallel workers, controlled budgets, deterministic enforcement |
| **Manual development** | $10,000-20,000 (1 senior dev) | Varies | Higher quality, better judgment, slower throughput |
| **Single Claude Code session** | $5-15 per complex feature | $5-15 | No parallelism, no isolation, context window limits |

Blue Flame's key cost advantages over Gastown:
1. **Controlled concurrency**: Max 4 workers vs. 20-30 agents. Linear cost scaling vs. exponential.
2. **Per-agent budgets**: Prevents individual agents from runaway consumption. Gastown reportedly has minimal budget controls.
3. **Deterministic enforcement**: Watcher hooks catch violations in real-time, preventing wasted work. Gastown relies more on agent self-policing.
4. **Human gates**: Plan approval and changeset approval prevent the system from spending money on the wrong work. Gastown is more autonomous.
5. **Haiku for validation**: Gastown reportedly uses the same model for all roles.

At $1-5 per feature session, Blue Flame is 10-50x cheaper than Gastown for comparable work. Even at the upper bound of $5-10 per session (with retries and complexity), it remains highly cost-effective.

The comparison to manual development is nuanced: Blue Flame produces lower quality output that requires human review, but at 1/1000th the cost per feature. For well-decomposed, well-specified tasks, this tradeoff is excellent.

---

### 12. Is the per-agent budget approach better than a per-session budget?

**COST-EFFECTIVE**: Per-agent budgets are the right choice, with one caveat.

**Advantages of per-agent budgets:**
1. **Isolation**: One runaway agent cannot consume the entire session budget, starving other agents.
2. **Predictability**: Each agent type has a known cost ceiling. Workers are capped at ~$0.30-$1.50 each (100K tokens), validators at ~$0.03-$0.15 (30K tokens).
3. **Debugging**: When a budget kill occurs, you know exactly which agent and task caused it.
4. **Fairness**: All workers get the same budget regardless of execution order.

**Advantages of per-session budgets (what the plan misses):**
1. **Flexibility**: A session with 2 easy tasks and 1 hard task could allocate more budget to the hard task. Per-agent budgets treat all tasks equally.
2. **Retry accounting**: A per-session budget naturally accounts for retry costs. Per-agent budgets make retries look "free" because each retry gets a fresh budget.
3. **Global cost cap**: The human cares about total session cost, not individual agent cost. Per-agent budgets sum to `(100K*3 workers + 30K*3 validators + 50K planner + 80K merger) * (1 + max_retries)` = potentially 780K-2.34M tokens without any global cap.

**SUGGESTION**: Use BOTH. Per-agent budgets for isolation and per-session budgets as a global circuit breaker. The session budget should be the sum of all expected agent budgets plus a contingency (e.g., 1.5x). Example: session budget of $5.00 with per-agent budgets as currently specified. If total spending across all agents reaches $5.00, the session pauses for human approval regardless of individual agent status.

---

## Summary of Recommendations

### Critical (implement before production use)

1. **Add a session-level cost circuit breaker** that pauses execution when total cost exceeds a configurable threshold (e.g., 3-5x the initial estimate). This is the single most important missing cost control.

2. **Clarify retry budget policy**: Do retries get fresh per-agent budgets? If yes, document that the effective per-task ceiling is `budget * (1 + max_retries)`. If no, document that retries share the original budget.

3. **Include orchestrator cost in the cost table**. The current table omits what is likely a $0.20-$0.50 cost center.

### Important (implement soon after launch)

4. **Express budgets in dollar amounts, not raw token counts**, to account for input/output token price asymmetry and cache discounts.

5. **Present cost estimates as a range** (optimistic/typical/pessimistic) rather than a single point estimate.

6. **Specify Beads memory decay policy** with configurable retention periods for different context types.

7. **Cap task history growth** to prevent re-queued tasks from accumulating unbounded context.

### Nice to Have (optimization opportunities)

8. **Dynamic model selection** based on planner-annotated task complexity.
9. **Prompt caching optimization** with consistent prompt prefixes across same-role agents.
10. **Batch API for validators** if architecture permits direct API calls.
11. **Early termination heuristics** for obviously failing agents.
12. **Incremental diff scoping** for validator context reduction.

---

## Final Verdict

The plan is **cost-conscious and fundamentally sound**. The tiered model selection, shell-based mechanical operations, two-tier validation, and per-agent budgets demonstrate genuine cost engineering, not just afterthought budgeting. At $1-5 per feature session (realistic range), Blue Flame is an order of magnitude cheaper than alternatives like Gastown.

The primary gap is the absence of a session-level cost ceiling. Per-agent budgets are necessary but not sufficient -- they prevent individual runaway but not systemic cost escalation from retries, re-queues, and complex merges. Adding a session-level circuit breaker and presenting honest cost ranges (not optimistic point estimates) would make this plan production-safe from a budget perspective.

The $0.78 estimate should be reframed as: **"$0.78 base, $1.50-$3.00 typical, $5-10 worst case per 3-task session."** This is still excellent economics for automated multi-agent development work.
