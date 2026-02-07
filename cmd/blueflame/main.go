package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/kylegalloway/blueflame/internal/agent"
	"github.com/kylegalloway/blueflame/internal/config"
	"github.com/kylegalloway/blueflame/internal/locks"
	"github.com/kylegalloway/blueflame/internal/memory"
	"github.com/kylegalloway/blueflame/internal/orchestrator"
	"github.com/kylegalloway/blueflame/internal/state"
	"github.com/kylegalloway/blueflame/internal/tasks"
	"github.com/kylegalloway/blueflame/internal/ui"
	"github.com/kylegalloway/blueflame/internal/worktree"
)

var (
	version = "dev"
)

func main() {
	// Subcommands
	if len(os.Args) > 1 && os.Args[1] == "cleanup" {
		runCleanup()
		return
	}

	configPath := flag.String("config", "blueflame.yaml", "path to blueflame.yaml config file")
	task := flag.String("task", "", "task description for the planner")
	dryRun := flag.Bool("dry-run", false, "show what would happen without spawning agents")
	decisionsFile := flag.String("decisions-file", "", "path to decisions file for automated testing")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("blueflame %s\n", version)
		os.Exit(0)
	}

	if *task == "" && flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "Usage: blueflame --task 'description' [--config blueflame.yaml]")
		fmt.Fprintln(os.Stderr, "       blueflame 'description'")
		fmt.Fprintln(os.Stderr, "       blueflame cleanup [--config blueflame.yaml]")
		os.Exit(1)
	}

	taskDesc := *task
	if taskDesc == "" {
		taskDesc = flag.Arg(0)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	if *dryRun {
		printDryRun(cfg, taskDesc)
		return
	}

	// Print banner
	fmt.Printf("Blue Flame v%s\n", version)
	fmt.Printf("Project: %s\n", cfg.Project.Name)
	fmt.Printf("Repo: %s\n", cfg.Project.Repo)
	fmt.Printf("Task: %s\n", taskDesc)
	concurrency := agent.EffectiveConcurrency(&cfg.Concurrency)
	fmt.Printf("Workers: %d", concurrency)
	if cfg.Concurrency.Adaptive && concurrency != cfg.Concurrency.Development {
		fmt.Printf(" (reduced from %d due to available RAM)", cfg.Concurrency.Development)
	}
	fmt.Println()
	fmt.Println()

	// Initialize internal state directory
	stateDir := filepath.Join(cfg.Project.Repo, ".blueflame")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		log.Fatalf("create state dir: %v", err)
	}

	// Disk space check
	if err := worktree.CheckDiskSpace(cfg.Project.Repo, worktree.MinDiskSpaceMB); err != nil {
		log.Fatalf("Disk space check: %v", err)
	}

	// Initialize managers
	lockMgr := locks.NewManager(filepath.Join(stateDir, "locks"))
	stateMgr := state.NewManager(stateDir)
	taskStore := tasks.NewTaskStore(filepath.Join(cfg.Project.Repo, cfg.Project.TasksFile))

	lifecycleMgr := agent.NewLifecycleManager(agent.LifecycleConfig{
		PersistPath:       filepath.Join(stateDir, "agents.json"),
		HeartbeatInterval: cfg.Limits.HeartbeatInterval,
		AgentTimeout:      cfg.Limits.AgentTimeout,
		StallThreshold:    2 * cfg.Limits.HeartbeatInterval,
		AuditDir:          filepath.Join(stateDir, "audit"),
	})

	wtMgr := worktree.NewManager(cfg.Project.Repo, cfg.Project.WorktreeDir, cfg.Project.BaseBranch)

	// Startup cleanup
	cleanupResult, err := orchestrator.CleanupStaleState(lifecycleMgr, lockMgr, wtMgr, stateMgr)
	if err != nil {
		log.Printf("Warning: startup cleanup: %v", err)
	}
	if cleanupResult != nil {
		msg := orchestrator.FormatCleanupResult(cleanupResult)
		if msg != "Clean startup, no stale state found." {
			fmt.Println(msg)
		}
	}

	// Choose prompter (before recovery check so it can prompt the user)
	var prompter ui.Prompter
	if *decisionsFile != "" {
		prompter = ui.NewScriptedPrompterFromFile(*decisionsFile)
	} else {
		prompter = ui.NewTerminalPrompter()
	}

	// Handle crash recovery prompt
	var recoveryState *state.OrchestratorState
	if cleanupResult != nil && cleanupResult.RecoveryState != nil {
		decision := prompter.CrashRecoveryPrompt(cleanupResult.RecoveryState)
		switch decision {
		case ui.RecoveryResume:
			recoveryState = cleanupResult.RecoveryState
		case ui.RecoveryFresh:
			stateMgr.Remove()
		}
	}

	// Create spawner and orchestrator
	spawner := &agent.ProductionSpawner{
		PromptRenderer: &agent.DefaultPromptRenderer{},
		HooksDir:       filepath.Join(stateDir, "hooks"),
	}

	orch := orchestrator.New(cfg, spawner, prompter, taskStore, stateMgr)
	orch.SetLifecycleManager(lifecycleMgr)
	orch.SetWorktreeManager(wtMgr)
	orch.SetLockManager(lockMgr)
	orch.SetHooksDir(filepath.Join(stateDir, "hooks"), agent.DefaultWatcherTemplate())
	if recoveryState != nil {
		orch.SetRecoveryState(recoveryState)
	}

	// Wire memory provider
	var memProvider memory.Provider
	if cfg.Beads.Enabled {
		memProvider = memory.NewBeadsProvider(memory.BeadsConfig{
			Enabled:             true,
			ArchiveAfterWave:    cfg.Beads.ArchiveAfterWave,
			IncludeFailureNotes: cfg.Beads.IncludeFailureNotes,
		})
	} else {
		memProvider = &memory.NoopProvider{}
	}
	orch.SetMemoryProvider(memProvider)

	// Setup signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		fmt.Fprintf(os.Stderr, "\nReceived %s, shutting down gracefully...\n", sig)
		orch.HandleShutdown()
		lockMgr.ReleaseAll()
		cancel()
		// If we get a second signal, force exit
		<-sigCh
		fmt.Fprintln(os.Stderr, "Force exit.")
		os.Exit(1)
	}()

	// Run orchestrator
	startTime := time.Now()
	if err := orch.Run(ctx, taskDesc); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		lockMgr.ReleaseAll()
		os.Exit(1)
	}

	// Print cost summary
	summary := orch.SessionSummary()
	summary.Duration = time.Since(startTime)
	summary.CostLimit = cfg.Limits.MaxSessionCostUSD
	summary.TokenLimit = cfg.Limits.MaxSessionTokens
	fmt.Print(ui.FormatCostSummary(summary))

	// Cleanup
	lockMgr.ReleaseAll()
}

func runCleanup() {
	// Parse flags after "cleanup" subcommand
	cleanupFlags := flag.NewFlagSet("cleanup", flag.ExitOnError)
	configPath := cleanupFlags.String("config", "blueflame.yaml", "path to blueflame.yaml config file")
	cleanupFlags.Parse(os.Args[2:])

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	stateDir := filepath.Join(cfg.Project.Repo, ".blueflame")

	lockMgr := locks.NewManager(filepath.Join(stateDir, "locks"))
	stateMgr := state.NewManager(stateDir)
	wtMgr := worktree.NewManager(cfg.Project.Repo, cfg.Project.WorktreeDir, cfg.Project.BaseBranch)
	lifecycleMgr := agent.NewLifecycleManager(agent.LifecycleConfig{
		PersistPath: filepath.Join(stateDir, "agents.json"),
	})

	fmt.Println("Blue Flame Cleanup")
	fmt.Println()

	result, err := orchestrator.CleanupStaleState(lifecycleMgr, lockMgr, wtMgr, stateMgr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cleanup error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(orchestrator.FormatCleanupResult(result))

	// Remove stale worktrees if found
	if len(result.StaleWorktrees) > 0 {
		fmt.Printf("\nRemoving %d stale worktree(s)...\n", len(result.StaleWorktrees))
		for _, wt := range result.StaleWorktrees {
			fmt.Printf("  Removing: %s\n", wt)
			if err := os.RemoveAll(wt); err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: %v\n", err)
			}
		}
	}

	// Remove recovery state
	if result.RecoveryState != nil {
		stateMgr.Remove()
		fmt.Println("Removed recovery state.")
	}

	// Clean stale locks
	if err := lockMgr.CleanStale(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: stale lock cleanup: %v\n", err)
	} else {
		fmt.Println("Stale locks cleaned.")
	}

	fmt.Println("\nCleanup complete.")
}

func printDryRun(cfg *config.Config, taskDesc string) {
	fmt.Println("=== BLUE FLAME: Dry Run ===")
	fmt.Println()
	fmt.Printf("Config: %s (schema v%d)\n", cfg.Project.Name, cfg.SchemaVersion)
	fmt.Printf("Repo: %s (branch: %s)\n", cfg.Project.Repo, cfg.Project.BaseBranch)
	fmt.Printf("Task: %s\n", taskDesc)
	fmt.Println()

	concurrency := agent.EffectiveConcurrency(&cfg.Concurrency)
	fmt.Println("Wave Configuration:")
	fmt.Printf("  Planning: %d agent(s), model=%s, interactive=%v\n",
		cfg.Concurrency.Planning, cfg.Models.Planner, cfg.Planning.Interactive)
	fmt.Printf("  Development: up to %d workers, model=%s\n",
		concurrency, cfg.Models.Worker)
	if cfg.Concurrency.Adaptive {
		fmt.Printf("    (adaptive: configured=%d, effective=%d based on available RAM)\n",
			cfg.Concurrency.Development, concurrency)
	}
	fmt.Printf("  Validation: up to %d validators, model=%s\n",
		cfg.Concurrency.Validation, cfg.Models.Validator)
	fmt.Printf("  Merge: %d merger, model=%s\n",
		cfg.Concurrency.Merge, cfg.Models.Merger)
	fmt.Println()
	fmt.Println("Budget Limits:")
	if cfg.Limits.MaxSessionCostUSD > 0 {
		fmt.Printf("  Session: $%.2f USD\n", cfg.Limits.MaxSessionCostUSD)
	} else if cfg.Limits.MaxSessionTokens > 0 {
		fmt.Printf("  Session: %d tokens\n", cfg.Limits.MaxSessionTokens)
	} else {
		fmt.Println("  Session: unlimited")
	}
	fmt.Printf("  Max wave cycles: %d\n", cfg.Limits.MaxWaveCycles)
	fmt.Printf("  Max retries per task: %d\n", cfg.Limits.MaxRetries)
	fmt.Printf("  Agent timeout: %v\n", cfg.Limits.AgentTimeout)
	fmt.Println()

	// Per-role budgets
	fmt.Println("Per-Agent Budgets:")
	printBudget("  Planner", cfg.Limits.TokenBudget.PlannerBudget())
	printBudget("  Worker", cfg.Limits.TokenBudget.WorkerBudget())
	printBudget("  Validator", cfg.Limits.TokenBudget.ValidatorBudget())
	printBudget("  Merger", cfg.Limits.TokenBudget.MergerBudget())
	fmt.Println()

	fmt.Println("Permissions:")
	fmt.Printf("  Allowed paths: %v\n", cfg.Permissions.AllowedPaths)
	fmt.Printf("  Blocked paths: %v\n", cfg.Permissions.BlockedPaths)
	fmt.Printf("  Allowed tools: %v\n", cfg.Permissions.AllowedTools)
	fmt.Printf("  Blocked tools: %v\n", cfg.Permissions.BlockedTools)
	fmt.Println()

	// Disk space check
	if err := worktree.CheckDiskSpace(cfg.Project.Repo, worktree.MinDiskSpaceMB); err != nil {
		fmt.Printf("Disk space: INSUFFICIENT (%v)\n", err)
	} else {
		fmt.Println("Disk space: OK")
	}

	fmt.Println()
	fmt.Println("(Dry run: no agents will be spawned)")
}

func printBudget(label string, budget config.BudgetSpec) {
	if budget.Value > 0 {
		if budget.Unit == config.USD {
			fmt.Printf("%s: $%.2f USD\n", label, budget.Value)
		} else {
			fmt.Printf("%s: %.0f tokens\n", label, budget.Value)
		}
	} else {
		fmt.Printf("%s: unlimited\n", label)
	}
}
