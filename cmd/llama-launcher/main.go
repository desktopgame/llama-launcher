package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/desktopgame/llama-launcher/internal/config"
	"github.com/desktopgame/llama-launcher/internal/profile"
	"github.com/desktopgame/llama-launcher/internal/runtime"
	"github.com/desktopgame/llama-launcher/internal/swap"
	"github.com/desktopgame/llama-launcher/internal/tui"
	"github.com/desktopgame/llama-launcher/internal/workspace"
)

func main() {
	if len(os.Args) > 1 {
		runHeadless(os.Args[1])
		return
	}

	p := tea.NewProgram(tui.NewModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runHeadless(wsName string) {
	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	wsMgr := workspace.NewManager(cfg.WorkspaceDir)
	ws, err := wsMgr.Load(wsName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading workspace %q: %v\n", wsName, err)
		os.Exit(1)
	}

	profMgr := profile.NewManager(cfg.ProfileDir)
	rtMgr := runtime.NewManager(cfg.RuntimeDir)

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

	fmt.Printf("llama-swap started with workspace %q on port %d\n", wsName, cfg.Port)
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
