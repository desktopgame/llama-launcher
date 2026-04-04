package swap

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// Process manages a running llama-swap instance.
type Process struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	running bool
	cfgPath string // temp config.yaml path
}

// CheckInstalled returns nil if llama-swap is found in PATH.
func CheckInstalled() error {
	_, err := exec.LookPath("llama-swap")
	if err != nil {
		return fmt.Errorf("llama-swap not found in PATH. Install it from https://github.com/mostlygeek/llama-swap")
	}
	return nil
}

// Start launches llama-swap with the given config file and listen port.
func (p *Process) Start(configPath string, port int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return fmt.Errorf("llama-swap is already running")
	}

	p.cmd = exec.Command("llama-swap", "--config", configPath, "--listen", fmt.Sprintf(":%d", port))
	// don't pipe to os.Stdout/Stderr — it corrupts Bubble Tea's alt screen

	if err := p.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start llama-swap: %w", err)
	}

	p.running = true
	p.cfgPath = configPath

	// wait in background and clean up when done
	go func() {
		p.cmd.Wait()
		p.mu.Lock()
		p.running = false
		// clean up temp config
		if p.cfgPath != "" {
			os.RemoveAll(filepath.Dir(p.cfgPath))
			p.cfgPath = ""
		}
		p.mu.Unlock()
	}()

	return nil
}

// Stop terminates the running llama-swap process.
func (p *Process) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return fmt.Errorf("llama-swap is not running")
	}

	if err := p.cmd.Process.Kill(); err != nil {
		return fmt.Errorf("failed to stop llama-swap: %w", err)
	}

	p.running = false
	// clean up temp config dir
	if p.cfgPath != "" {
		os.RemoveAll(filepath.Dir(p.cfgPath))
		p.cfgPath = ""
	}

	return nil
}

// IsRunning returns whether llama-swap is currently running.
func (p *Process) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}
