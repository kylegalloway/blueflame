# Extensibility and Maintainability Review: Blue Flame Plan-ADR

**Reviewer**: Claude Opus 4.6 (Automated Maintainability Analysis)
**Date**: 2026-02-07
**Document Reviewed**: `/Users/kylegalloway/src/blueflame/Plan-ADR.md`

---

## Overall Maintainability Assessment

**Rating: GOOD -- with targeted improvements needed in schema evolution and component coupling**

The Blue Flame plan exhibits strong architectural instincts for maintainability. The separation of concerns between shell scripts (mechanical operations), prompt templates (agent behavior), YAML configuration (policy), and the orchestrator skill (coordination logic) creates natural boundaries that will serve the project well. The choice of plain shell scripts over compiled binaries lowers the barrier to modification, and the template-based watcher generation is a sound pattern for extensibility.

However, the plan has notable gaps in three areas: (1) there is no schema versioning or migration strategy for `blueflame.yaml` and `tasks.yaml`, which will cause pain as the system evolves; (2) the orchestrator skill (`SKILL.md`) is implicitly coupled to the exact set of four agent roles, making new roles harder to add than they should be; and (3) the Beads integration is woven into the orchestrator flow in ways that would make replacing it non-trivial.

The phased delivery model is well-structured and each phase produces independently testable artifacts, which is excellent for incremental adoption. The plan would benefit from making its implicit extension points explicit -- it has good seams, but they are accidental rather than intentional.

---

## Detailed Findings

### 1. Adding a New Agent Role (e.g., "reviewer" or "documenter")

**Category: RIGID**

Adding a new agent role currently requires modifications in at least five locations:

1. **`blueflame.yaml`**: Add a new entry under `concurrency` (e.g., `review: 2`), a new entry under `limits.token_budget` (e.g., `reviewer: 40000`), and a new entry under `models` (e.g., `reviewer: "haiku"`).
2. **`templates/`**: Create a new prompt template file (e.g., `reviewer-prompt.md`).
3. **`SKILL.md`**: The orchestrator's wave logic must be updated to include a new wave or sub-phase that dispatches the new role. The wave sequence (plan -> develop -> validate -> merge) is hardcoded into the skill's procedural logic.
4. **Shell scripts**: `watcher-generate.sh` and `sandbox-setup.sh` may need updates if the new role has different permission requirements than existing roles.
5. **`tasks.yaml`**: The task schema may need new fields (e.g., a `review_result` alongside `result`).

The core problem is that the four-wave structure (plan, develop, validate, merge) is baked into the orchestrator's procedural flow rather than being data-driven. There is no concept of a "role registry" or "wave definition" that could be extended without modifying the orchestrator itself.

**SUGGESTION**: Define waves and roles declaratively in `blueflame.yaml`:

```yaml
waves:
  - name: planning
    roles: [planner]
    concurrency: 1
  - name: development
    roles: [worker]
    concurrency: 4
  - name: review          # New wave -- added purely through config
    roles: [reviewer]
    concurrency: 2
  - name: validation
    roles: [validator]
    concurrency: 2
  - name: merge
    roles: [merger]
    concurrency: 1

roles:
  planner:
    model: sonnet
    token_budget: 50000
    prompt_template: templates/planner-prompt.md
  worker:
    model: sonnet
    token_budget: 100000
    prompt_template: templates/worker-prompt.md
  reviewer:               # New role -- added purely through config
    model: haiku
    token_budget: 40000
    prompt_template: templates/reviewer-prompt.md
  # ...
```

This makes the orchestrator a generic wave executor rather than a four-phase state machine, and new roles become a configuration change plus a new prompt template.

---

### 2. Adding New Watcher Rules or Validation Checks

**Category: EXTENSIBLE**

The three-phase watcher model is well-designed for extensibility:

- **Phase 1 (Pre-execution hooks)**: Generated from `watcher.sh.tmpl`, which is a template. Adding a new check means adding a new clause to the template. Since the template reads its constraints from `blueflame.yaml`, many new checks can be expressed purely as new configuration fields (e.g., adding a `max_line_length` validation) without touching the template at all.
- **Phase 2 (OS-level sandbox)**: Adding new `ulimit` or sandbox constraints is straightforward -- add a new field to `blueflame.yaml.sandbox` and a corresponding line in `sandbox-setup.sh`.
- **Phase 3 (Post-execution filesystem diff)**: `watcher-postcheck.sh` runs a sequence of independent checks. Adding a new check is appending a new function or check block to the script.

The two-tier validation design (cheap mechanical checks first, expensive LLM checks second) creates a natural decision framework for where to put new validation logic.

**Minor concern**: The `permissions` section of `blueflame.yaml` mixes several conceptually different things (path ACLs, tool ACLs, bash command ACLs) into a single flat namespace. As the number of permission types grows, this section could become unwieldy.

**SUGGESTION**: Consider grouping permissions by enforcement phase:

```yaml
permissions:
  filesystem:
    allowed_paths: [...]
    blocked_paths: [...]
  tools:
    allowed: [...]
    blocked: [...]
  bash:
    allowed_commands: [...]
    blocked_patterns: [...]
```

This is partially done already (with `bash_rules` nested), but the inconsistency between `allowed_paths` (top-level) and `bash_rules.allowed_commands` (nested) will confuse contributors.

---

### 3. Adding New Shell Helper Scripts

**Category: EXTENSIBLE**

The shell script architecture is well-suited for adding new helpers:

- Each script has a focused responsibility and communicates through well-defined interfaces (files in `.blueflame/`, exit codes, stdout/stderr).
- Scripts use a subcommand pattern (e.g., `worktree-manage.sh create|remove|cleanup|list`) that can be extended with new subcommands without modifying existing ones.
- The orchestrator calls scripts by name via Bash tool invocations, so adding a new script does not require modifying other scripts.

However, there is an implicit coupling: the orchestrator (`SKILL.md`) must know about each script and when to call it. Adding a new helper script requires updating the orchestrator's procedural logic to call it at the right point in the wave lifecycle.

**SUGGESTION**: Consider a hook/event model where the orchestrator emits lifecycle events (e.g., `pre-agent-spawn`, `post-agent-complete`, `wave-transition`) and scripts can register to be called at specific events. This would decouple "what scripts exist" from "when they run." However, this may be premature optimization for the current scope -- the direct-call approach is simpler and probably sufficient for the foreseeable future.

---

### 4. Configuration Format Extensibility

**Category: EXTENSIBLE (with caveats)**

YAML is inherently extensible -- new fields can be added to `blueflame.yaml` without breaking existing parsers, since YAML parsers ignore unknown keys by default. The configuration structure is logical and well-organized.

**However, there is a significant gap: the plan specifies no schema version field for `blueflame.yaml`.** The `tasks.yaml` file has `version: 1`, but `blueflame.yaml` does not. This means:

- There is no way to detect whether a config file is compatible with the current version of the orchestrator.
- There is no migration path when the schema changes.
- There is no way for the orchestrator to provide helpful error messages like "your config uses the old format, here is what changed."

The `tasks.yaml` file does have a `version: 1` field, which is good, but the plan does not describe what happens when the version is incremented or how old-format files are handled.

**SUGGESTION**: Add explicit schema versioning to both files:

```yaml
# blueflame.yaml
schema_version: 1
project:
  name: "my-project"
  # ...
```

And define a policy in the orchestrator: "If `schema_version` is missing, treat as version 1. If `schema_version` is newer than the orchestrator supports, fail with a clear message. If `schema_version` is older, attempt automatic migration or print a migration guide."

---

### 5. Shell Script Modularity and Independent Replacement

**Category: EXTENSIBLE**

The shell scripts are well-modularized. Each script:

- Has a single, clear responsibility (worktree management, lock management, watcher generation, etc.).
- Communicates through the filesystem (`.blueflame/` directory) rather than through shared in-memory state.
- Uses a subcommand interface that could be reimplemented in another language without changing callers.
- Has no inter-script dependencies (e.g., `lock-manage.sh` does not import or call `worktree-manage.sh`).

This means any individual script can be replaced, rewritten in a different language, or upgraded without affecting the others, as long as it maintains the same CLI interface and filesystem conventions.

**One risk**: The `.blueflame/` directory structure is an implicit contract between all scripts. If one script changes the format of `agents.json` or the naming convention of lock files, other scripts will break silently. This contract is not documented separately from the plan -- it exists only in the narrative description and code.

**SUGGESTION**: Create a brief "Runtime State Contract" document that specifies the exact format of each file in `.blueflame/` (agents.json schema, lock file format, audit log JSONL schema, state.yaml schema). This serves as the interface contract between scripts and makes it safe for different people to maintain different scripts.

---

### 6. Prompt Template Flexibility

**Category: EXTENSIBLE**

The prompt template system is straightforward: each role has a Markdown file in `templates/`, and the orchestrator interpolates task-specific values into it before dispatching the agent. This is simple and effective.

However, the plan does not specify:

- **What variables are available** in each template (e.g., `{{task.id}}`, `{{task.description}}`, `{{beads_context}}`). Without a documented variable vocabulary, template authors must read the orchestrator source to know what they can use.
- **Whether templates support conditionals or loops**. For example, can a template conditionally include Beads context only if Beads is enabled? Or must the orchestrator handle this before interpolation?
- **Whether users can override the default templates** per-project. The plan shows templates in the skill's `templates/` directory, but there is no mention of a project-local template override (e.g., `blueflame.yaml` pointing to custom templates).

The inline prompt examples in the "Agent System Prompts" section are minimal and effective, but they appear to duplicate content that should live in the template files, creating a potential drift between documentation and implementation.

**SUGGESTION**:
1. Document the available template variables for each role.
2. Allow `blueflame.yaml` to specify custom template paths:
   ```yaml
   templates:
     worker: "./my-custom-templates/worker-prompt.md"
     validator: "./my-custom-templates/validator-prompt.md"
   ```
3. Define a fallback chain: project-local template -> skill default template.

---

### 7. Schema Versioning for tasks.yaml and blueflame.yaml

**Category: FRAGILE**

As noted in Finding 4, this is one of the weakest areas of the plan:

- `tasks.yaml` has `version: 1` but no description of what happens when the schema evolves.
- `blueflame.yaml` has no version field at all.
- There is no migration tooling or strategy described.
- The `beads-archive.sh` script archives `tasks.yaml` content to Beads, meaning old schema versions will persist in the memory system and may be loaded by future sessions.

This creates a fragile situation: as soon as the schema changes (which it will -- the plan is version 1 of a system that will evolve), archived Beads data may have incompatible field structures, and older `blueflame.yaml` files in users' projects will silently misbehave.

**SUGGESTION**:
1. Add `schema_version` to `blueflame.yaml`.
2. Document the version evolution policy: what constitutes a breaking vs. non-breaking change.
3. In `beads-archive.sh save`, include the schema version in each archived bead.
4. In `beads-archive.sh load`, validate schema version compatibility before injecting archived data into the planner's context.
5. In `blueflame-init.sh`, validate the schema version of `blueflame.yaml` and warn/fail on incompatibility.

---

### 8. Tightly-Coupled Components

**Category: RIGID**

Several components have tighter coupling than the architecture diagram suggests:

**a) Orchestrator <-> Wave Structure**: The four-wave sequence is procedural logic in `SKILL.md`. Changing the wave structure (adding, removing, or reordering waves) requires rewriting the orchestrator's core control flow. (See Finding 1 for the suggested fix.)

**b) Orchestrator <-> Shell Script Invocations**: The orchestrator knows every shell script by name and calls them at specific points. While this is acceptable at the current scale (8 scripts), it means the orchestrator is the integration hub and must be updated whenever a new script is added or an existing script's interface changes.

**c) Watcher Template <-> blueflame.yaml Schema**: The `watcher.sh.tmpl` template must understand the exact structure of `blueflame.yaml`'s `permissions` and `validation` sections. If new permission types are added to the config, the template must be updated to enforce them. These two artifacts evolve together and must stay in sync.

**d) tasks.yaml <-> Multiple Consumers**: The `tasks.yaml` file is read and written by the orchestrator, planner, and `beads-archive.sh`. Its schema is an implicit contract between all of these. A schema change in `tasks.yaml` has a blast radius across the orchestrator, the planner prompt, and the Beads archive script.

**e) Agent Process Lifecycle <-> Multiple Scripts**: An agent's lifecycle touches `worktree-manage.sh`, `lock-manage.sh`, `sandbox-setup.sh`, `watcher-generate.sh`, `lifecycle-manage.sh`, and `token-tracker.sh`. The orchestrator must call these in the correct order. If the lifecycle sequence changes, the orchestrator's procedural code must be updated.

**SUGGESTION for (e)**: Consider a composite `agent-launch.sh` script that encapsulates the full agent setup sequence (create worktree, acquire locks, setup sandbox, generate watcher, register lifecycle). This reduces the orchestrator's coupling to a single script call per agent lifecycle event, and the internal sequence can be modified without touching the orchestrator.

---

### 9. Replacing Beads with Another Memory System

**Category: RIGID**

Beads is referenced in multiple locations throughout the plan:

- `blueflame.yaml`: `beads` configuration section with specific Beads features (memory decay, archive after wave).
- `beads-archive.sh`: Script dedicated to Beads operations.
- `blueflame-init.sh`: Checks for Beads installation as a prerequisite.
- Planner prompt: Receives Beads-loaded context.
- Wave 4 post-merge: Calls `beads-archive.sh save`.
- The architectural narrative references Beads-specific features (hash IDs, memory decay, git-backed storage).

Replacing Beads would require:

1. Rewriting or replacing `beads-archive.sh`.
2. Modifying `blueflame-init.sh` to check for the new system.
3. Updating the `blueflame.yaml` configuration section.
4. Potentially updating the planner prompt format (if the new system provides context differently).
5. Updating the orchestrator's Wave 1 (load context) and Wave 4 (save results) logic.

The plan does note that Beads is a configurable feature (`beads.enabled: true`), which provides a basic on/off toggle. But there is no abstraction layer -- the system either uses Beads or uses nothing. There is no "memory provider" interface.

**SUGGESTION**: Abstract the persistent memory system behind a script-level interface:

```
scripts/memory-save.sh   # Save session results (currently calls beads)
scripts/memory-load.sh   # Load prior session context (currently calls beads)
```

The orchestrator calls these generic scripts. The scripts internally delegate to Beads (or whatever system is configured). This makes the replacement surface area exactly two scripts instead of five+ locations.

---

### 10. Incremental Delivery and Phase Independence

**Category: EXTENSIBLE**

The five implementation phases are well-structured for incremental delivery:

- **Phase 1 (Shell Helpers)**: Produces independently usable scripts. These can be tested and used manually even without the orchestrator. The milestone is explicit: "Shell helpers work standalone."
- **Phase 2 (Watcher System)**: Builds on Phase 1 but produces independently testable artifacts. A manually spawned Claude agent can be constrained by watchers without the full orchestrator.
- **Phase 3 (Orchestrator Skill)**: Depends on Phases 1 and 2 but is the first phase that delivers the full user-facing experience.
- **Phase 4 (Lifecycle Hardening)**: Enhances robustness. The system works without it (just less reliably). This is a true "hardening" phase that does not change the happy path.
- **Phase 5 (Polish)**: Pure UX improvements. Fully independent.

Each phase has a clear milestone that defines "done" and can be verified independently. This is excellent for incremental delivery.

**One concern**: Phase 3 is the largest and most complex phase. It contains the entire orchestrator skill, all four prompt templates, the full wave protocol, human approval gates, task claiming, wave transitions, re-queue logic, and session continuation. This is a lot of interrelated functionality to deliver in a single phase.

**SUGGESTION**: Consider splitting Phase 3 into sub-phases:
- **3a**: Single-worker flow (one planner, one worker, one validator, one merger -- no concurrency).
- **3b**: Multi-worker concurrency and task dependency resolution.
- **3c**: Human approval gates and changeset chaining.
- **3d**: Re-queue logic and session continuation.

This would allow early end-to-end testing with a single worker before tackling the concurrency complexity.

---

### 11. Plugin/Extension Points and Natural Seams

**Category: EXTENSIBLE (implicit, not explicit)**

The architecture has several natural seams that could serve as extension points, but none are explicitly designed as such:

**a) Shell Script Interface (Good Seam)**
Each shell script is an independent executable with a CLI interface. This is a natural plugin boundary -- anyone can add a new script without understanding the internals of existing scripts. The `.blueflame/` directory serves as a shared state bus.

**b) Prompt Templates (Good Seam)**
The `templates/` directory with role-based Markdown files is a clean extension point. New roles need new templates, and existing templates can be customized without touching code.

**c) Watcher Template (Good Seam)**
The `watcher.sh.tmpl` template is a natural point for adding new enforcement rules. Since it generates per-agent scripts, modifications affect all future agents without retroactive changes.

**d) blueflame.yaml (Good Seam)**
YAML configuration is inherently open to new fields. The configuration is the primary mechanism for customizing behavior without code changes.

**e) Audit Log Format (Weak Seam)**
The JSONL audit logs in `.blueflame/logs/` could serve as an extension point for external monitoring, alerting, or analysis tools. However, the plan does not describe the log schema in enough detail for third-party consumers to rely on it.

**Missing Extension Points**:

1. **No event/hook system in the orchestrator**: There is no way to run custom logic at specific points in the wave lifecycle (e.g., "after all workers complete but before validators start") without modifying the orchestrator itself.
2. **No custom validation provider**: The validation system (Tier 1 mechanical + Tier 2 semantic) is closed. There is no way to add a "Tier 1.5" custom validation step (e.g., running a project-specific linter) without modifying `watcher-postcheck.sh`.
3. **No post-merge hook**: After changesets are merged, there is no extension point for running project-specific actions (e.g., triggering a CI build, sending a notification).

**SUGGESTION**: Add explicit lifecycle hooks in `blueflame.yaml`:

```yaml
hooks:
  post_plan: "./scripts/custom/notify-team.sh"
  pre_validation: "./scripts/custom/run-linter.sh"
  post_merge: "./scripts/custom/trigger-ci.sh"
  on_failure: "./scripts/custom/alert-oncall.sh"
```

This makes the orchestrator's lifecycle events explicit and extensible without requiring changes to the orchestrator itself.

---

## Summary Table

| Finding | Category | Severity | Effort to Fix |
|---------|----------|----------|---------------|
| Adding new agent roles requires changes in 5+ places | RIGID | High | Medium -- requires refactoring orchestrator to be role-agnostic |
| Adding new watcher rules is straightforward | EXTENSIBLE | N/A | N/A |
| New shell scripts can be added without modifying other scripts | EXTENSIBLE | N/A | N/A |
| YAML config is extensible but lacks schema versioning | FRAGILE | High | Low -- add version fields and validation |
| Shell scripts are modular and independently replaceable | EXTENSIBLE | N/A | N/A |
| Prompt templates are flexible but lack documented variables | EXTENSIBLE | Low | Low -- document the variable vocabulary |
| No schema versioning or migration strategy | FRAGILE | High | Medium -- design version evolution policy |
| Orchestrator is tightly coupled to wave structure | RIGID | Medium | Medium -- make waves data-driven |
| Beads replacement would touch 5+ locations | RIGID | Medium | Low -- abstract behind generic memory scripts |
| Phases are well-structured for incremental delivery | EXTENSIBLE | N/A | N/A |
| Extension points exist but are implicit | EXTENSIBLE | Low | Low -- make hooks explicit in config |
| Agent lifecycle touches 5 scripts in specific order | RIGID | Medium | Low -- create composite launch script |
| tasks.yaml is read/written by multiple components | FRAGILE | Medium | Low -- document schema as explicit contract |
| Phase 3 is too large for a single delivery phase | SUGGESTION | Low | Low -- split into sub-phases |
| No custom lifecycle hooks in orchestrator | SUGGESTION | Low | Low -- add hook config section |
| Permission config structure is inconsistent | SUGGESTION | Low | Low -- normalize nesting |

---

## Top 5 Recommendations (Prioritized by Impact)

1. **Add schema versioning to both `blueflame.yaml` and `tasks.yaml`** -- This is the single highest-risk gap. Without it, every schema evolution will be a breaking change with no migration path. Low effort, high impact.

2. **Make the wave/role structure data-driven** -- Move from a hardcoded four-wave state machine to a configurable wave sequence defined in `blueflame.yaml`. This is the single biggest improvement for extensibility. Medium effort, high impact.

3. **Abstract Beads behind a generic memory interface** -- Replace direct Beads references in the orchestrator with calls to `memory-save.sh` and `memory-load.sh`. Low effort, medium impact.

4. **Create a composite `agent-launch.sh` script** -- Encapsulate the multi-script agent setup sequence to reduce orchestrator coupling. Low effort, medium impact.

5. **Add explicit lifecycle hooks to `blueflame.yaml`** -- Enable project-specific customization at wave boundaries without modifying the orchestrator. Low effort, medium impact.

---

## Conclusion

The Blue Flame plan is architecturally sound for its intended scope. The separation between shell scripts, YAML configuration, prompt templates, and orchestrator logic creates genuinely useful boundaries. The phased delivery plan is realistic and each phase has clear, testable milestones.

The primary maintainability risks are: (1) the implicit coupling between the orchestrator and the fixed four-wave structure, which will become a bottleneck as soon as someone wants a fifth wave or a different role; and (2) the absence of schema versioning, which will cause silent breakage as the system evolves.

Both of these are addressable with moderate effort and should be considered before Phase 3 implementation begins, since Phase 3 is where the orchestrator's control flow is defined and where the cost of changing it later is highest.
