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
	flag.Usage = usage
	flag.Parse()

	residentGiven := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "resident" {
			residentGiven = true
		}
	})

	switch {
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
