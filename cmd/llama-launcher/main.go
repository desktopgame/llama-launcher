package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/desktopgame/llama-launcher/internal/config"
	"github.com/desktopgame/llama-launcher/internal/measure"
	"github.com/desktopgame/llama-launcher/internal/profile"
	"github.com/desktopgame/llama-launcher/internal/runtime"
	"github.com/desktopgame/llama-launcher/internal/swap"
	"github.com/desktopgame/llama-launcher/internal/tui"
	"github.com/desktopgame/llama-launcher/internal/winjob"
	"github.com/desktopgame/llama-launcher/internal/workspace"
)

func main() {
	// Windowsでは自分をkill-on-closeなJob Objectに入れてから子プロセスを起動する。
	// launcherがどんな死に方をしてもllama-swap/llama-serverが道連れになり、
	// GPUメモリを握ったままの孤児プロセスが残らない。
	if err := winjob.SetupKillOnClose(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: job object setup failed: %v\n", err)
	}

	resident := flag.String("resident", "",
		"comma-separated profile names to keep resident; runs without a workspace")
	ttl := flag.Int("ttl", workspace.DefaultTTL,
		"TTL in seconds applied to every model in --resident mode")
	// 値なしで呼べる必要があるので bool。対象プロファイルは位置引数で受ける
	measure := flag.Bool("measure", false,
		"load each profile in turn and record its measured cost; names may follow, default is all")
	flag.Usage = usage
	flag.Parse()

	// --resident は空文字列("常駐なし")と未指定を区別する必要がある
	residentGiven := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "resident" {
			residentGiven = true
		}
	})

	switch {
	case *measure:
		runMeasure(splitNames(strings.Join(flag.Args(), ",")))
	case residentGiven:
		runResident(splitNames(*resident), *ttl)
	case flag.NArg() > 0:
		runHeadless(flag.Arg(0))
	default:
		runTUI()
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `llama-launcher — manage llama.cpp runtimes, models and llama-swap

Usage:
  llama-launcher                        start the TUI
  llama-launcher <workspace>            start llama-swap from a saved workspace
  llama-launcher --resident a,b,c       start llama-swap from all profiles,
                                        keeping the named ones resident
  llama-launcher --measure [a,b]        load each profile in turn and record
                                        its measured cost, then exit

Options:
`)
	flag.PrintDefaults()
}

func splitNames(s string) []string {
	var names []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			names = append(names, part)
		}
	}
	return names
}

func runTUI() {
	proc := &swap.Process{}
	p := tea.NewProgram(tui.NewModel(proc), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// clean up llama-swap if still running when TUI exits
	if proc.IsRunning() {
		proc.Stop()
	}
}

func runHeadless(wsName string) {
	cfg, profMgr, rtMgr := loadEnv()

	wsMgr := workspace.NewManager(cfg.WorkspaceDir)
	ws, err := wsMgr.Load(wsName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading workspace %q: %v\n", wsName, err)
		os.Exit(1)
	}

	// ワークスペースの中身はユーザーが個別に選んだものなので、cost 未設定は警告に留める
	checkCost(ws, profMgr, cfg.CostMax, false)
	startAndWait(cfg, ws, profMgr, rtMgr, fmt.Sprintf("workspace %q", wsName))
}

// runResident starts llama-swap without a workspace: every profile on disk is
// included, and only the named ones are marked resident.
func runResident(names []string, ttl int) {
	cfg, profMgr, rtMgr := loadEnv()

	ws, warnings, err := workspace.BuildResident(profMgr, rtMgr, names, ttl)
	printWarnings(warnings)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// --resident に名前を書いた=意図があるので cost 必須
	checkCost(ws, profMgr, cfg.CostMax, true)

	residentCount := 0
	for _, e := range ws.Entries {
		if e.Resident {
			residentCount++
		}
	}
	fmt.Printf("Resident: %d, on-demand: %d, TTL: %ds\n",
		residentCount, len(ws.Entries)-residentCount, ttl)

	startAndWait(cfg, ws, profMgr, rtMgr, fmt.Sprintf("resident %s", strings.Join(names, ", ")))
}

func loadEnv() (*config.Config, *profile.Manager, *runtime.Manager) {
	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}
	return cfg, profile.NewManager(cfg.ProfileDir), runtime.NewManager(cfg.RuntimeDir)
}

func checkCost(ws *workspace.Workspace, profMgr *profile.Manager, costMax int, strictResident bool) {
	report, warnings, err := workspace.CheckCost(ws, profMgr, costMax, strictResident)
	printWarnings(warnings)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if report.Max > 0 {
		fmt.Printf("Cost: resident %d + peak on-demand %d = %d / %d\n",
			report.ResidentTotal, report.PeakOnDemand, report.Peak(), report.Max)
	}
}

func printWarnings(warnings []string) {
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", w)
	}
}

func startAndWait(
	cfg *config.Config,
	ws *workspace.Workspace,
	profMgr *profile.Manager,
	rtMgr *runtime.Manager,
	label string,
) {
	if err := swap.CheckInstalled(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	configPath, err := swap.GenerateConfig(ws, profMgr, rtMgr, cfg.Port)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating config: %v\n", err)
		os.Exit(1)
	}

	proc := &swap.Process{}
	if err := proc.Start(configPath, cfg.Port); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting llama-swap: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("llama-swap started with %s on port %d\n", label, cfg.Port)
	fmt.Printf("Log: %s\n", proc.LogPath())
	fmt.Println("Press Ctrl+C to stop")

	// wait for interrupt
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	fmt.Println("\nStopping llama-swap...")
	if err := proc.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "Error stopping: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Stopped")
}

// runMeasure loads each profile in turn and records how much memory it needs.
// An empty names slice measures every usable profile.
func runMeasure(names []string) {
	cfg, profMgr, rtMgr := loadEnv()

	if len(names) == 0 {
		fmt.Println("Measuring every usable profile. This loads each model in turn and takes a while.")
	} else {
		fmt.Printf("Measuring: %s\n", strings.Join(names, ", "))
	}
	fmt.Println("Close anything else that uses significant memory — the measurement is a delta.")

	results, err := measure.Run(names, profMgr, rtMgr, measure.Options{
		Port: cfg.Port,
		Log:  func(format string, args ...any) { fmt.Printf(format+"\n", args...) },
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\nResults (cost is in MB):")
	failed := 0
	for _, r := range results {
		if r.Err != nil {
			failed++
			fmt.Printf("  %-32s FAILED: %v\n", r.ProfileName, r.Err)
			continue
		}
		prof, err := profMgr.Load(r.ProfileName)
		note := ""
		// 手で入れた cost があるならそちらが優先されることを明示する
		if err == nil && prof.Cost != nil {
			note = fmt.Sprintf("  (cost %d is set by hand and still wins)", *prof.Cost)
		}
		fmt.Printf("  %-32s %6d%s\n", r.ProfileName, r.CostMB, note)
	}

	if failed > 0 {
		fmt.Fprintf(os.Stderr, "\n%d profile(s) could not be measured\n", failed)
		os.Exit(1)
	}
	fmt.Printf("\nWrote measured_cost to %d profile(s) in %s\n", len(results), profMgr.Dir())
	if cfg.CostMax <= 0 {
		fmt.Println("Set \"cost_max\" in config.json to enable the budget check.")
	}
}
