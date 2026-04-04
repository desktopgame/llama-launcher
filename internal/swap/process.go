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
	logFile *os.File
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

	// write logs to file in the same temp dir as config.yaml
	logPath := filepath.Join(filepath.Dir(configPath), "llama-swap.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}
	p.logFile = logFile
	p.cmd.Stdout = logFile
	p.cmd.Stderr = logFile

	if err := p.cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("failed to start llama-swap: %w", err)
	}

	p.running = true
	p.cfgPath = configPath

	// wait in background and clean up when done
	go func() {
		p.cmd.Wait()
		p.mu.Lock()
		p.running = false
		if p.logFile != nil {
			p.logFile.Close()
			p.logFile = nil
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
	if p.logFile != nil {
		p.logFile.Close()
		p.logFile = nil
	}

	return nil
}

// IsRunning returns whether llama-swap is currently running.
func (p *Process) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

// LogPath returns the path to the log file, or empty if not running.
func (p *Process) LogPath() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cfgPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(p.cfgPath), "llama-swap.log")
}
