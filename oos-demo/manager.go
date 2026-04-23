package main

// manager.go — Process Manager.
//
// Starts all services as native processes in the correct order,
// watches them and restarts on crash, and stops them cleanly on StopAll.
//
// PostgreSQL is NOT started here — it must already be running
// (brew services start postgresql, systemctl start postgresql, etc.).

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/pterm/pterm"
	"onisin.com/oos-demo/iam"
)

// Service describes a single running process.
type Service struct {
	Name    string
	Cmd     *exec.Cmd
	running bool
	mu      sync.Mutex
}

// Manager manages all running services.
type Manager struct {
	cfg      *Config
	services []*Service
	iam      *iam.Server
	mu       sync.Mutex
}

// NewManager creates a new Manager.
func NewManager(cfg *Config) *Manager {
	return &Manager{cfg: cfg}
}

// StartAll starts all managed services in the correct order.
// PostgreSQL must already be running (brew/apt/system service).
// Ollama must be running independently — oos-demo does not manage it.
func (m *Manager) StartAll() error {
	if err := m.checkPostgreSQL(); err != nil {
		return err
	}

	steps := []struct {
		name string
		fn   func() error
		wait time.Duration
	}{
		{"iam",  m.startIAM,  200 * time.Millisecond},
		{"oosp", m.startOOSP, 1 * time.Second},
	}

	for _, step := range steps {
		pterm.Info.Printf("Starting %s...\n", step.name)
		if err := step.fn(); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
		time.Sleep(step.wait)
	}

	return nil
}

// checkPostgreSQL verifies that PostgreSQL is reachable before starting services.
func (m *Manager) checkPostgreSQL() error {
	addr := fmt.Sprintf("localhost:%d", m.cfg.PostgreSQL.Port)

	for i := range 10 {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			conn.Close()
			pterm.Success.Printf("✅ PostgreSQL ready on port %d\n", m.cfg.PostgreSQL.Port)
			return nil
		}
		if i == 0 {
			pterm.Warning.Printf("Waiting for PostgreSQL (port %d)...\n", m.cfg.PostgreSQL.Port)
		}
		time.Sleep(time.Second)
	}

	return fmt.Errorf(
		"PostgreSQL not reachable on port %d\n"+
			"   macOS: brew services start postgresql@16\n"+
			"   Linux: sudo systemctl start postgresql",
		m.cfg.PostgreSQL.Port,
	)
}

// StopAll stops all services in reverse order: subprocesses first,
// then the embedded IAM. Subprocesses get SIGINT and 2s to exit
// cleanly before the method returns.
func (m *Manager) StopAll() {
	m.mu.Lock()
	services := make([]*Service, len(m.services))
	copy(services, m.services)
	iamSrv := m.iam
	m.mu.Unlock()

	for i := len(services) - 1; i >= 0; i-- {
		svc := services[i]
		svc.mu.Lock()
		if svc.running && svc.Cmd != nil && svc.Cmd.Process != nil {
			pterm.Info.Printf("Stopping %s...\n", svc.Name)
			svc.running = false
			_ = svc.Cmd.Process.Signal(os.Interrupt)
		}
		svc.mu.Unlock()
	}

	time.Sleep(2 * time.Second)

	if iamSrv != nil {
		pterm.Info.Println("Stopping iam...")
		iamSrv.Stop()
	}
}

// startProcess starts a binary as a managed process with automatic restart on crash.
//
// The binary is looked up in three steps:
//  1. ./dist/<bin>_<platform>   — our own binaries produced by `make compile`
//  2. ./dist/<bin>               — unsuffixed fallback for third-party binaries
//     copied into dist
//  3. $PATH lookup               — system-installed third-party binaries (dex)
func (m *Manager) startProcess(name, bin string, args, env []string) error {
	binPath := filepath.Join(m.cfg.BinDir(), bin+"_"+platformSuffix())
	if _, err := os.Stat(binPath); err != nil {
		binPath = filepath.Join(m.cfg.BinDir(), bin)
		if _, err := os.Stat(binPath); err != nil {
			if found, err := exec.LookPath(bin); err == nil {
				binPath = found
			} else {
				return fmt.Errorf("binary not found: %s", bin)
			}
		}
	}

	logPath := filepath.Join(m.cfg.LogDir(), name+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("log file open: %w", err)
	}

	svc := &Service{Name: name}
	cmd := buildCmd(binPath, args, env, logFile)

	if err := cmd.Start(); err != nil {
		return err
	}

	svc.Cmd     = cmd
	svc.running = true

	m.mu.Lock()
	m.services = append(m.services, svc)
	m.mu.Unlock()

	pterm.Success.Printf("✅ %s started (PID %d) → %s\n", name, cmd.Process.Pid, logPath)

	go m.watchProcess(svc, binPath, args, env, logFile)

	return nil
}

// watchProcess monitors a service and restarts it automatically on exit.
func (m *Manager) watchProcess(svc *Service, binPath string, args, env []string, logFile *os.File) {
	for {
		err := svc.Cmd.Wait()

		svc.mu.Lock()
		if !svc.running {
			svc.mu.Unlock()
			return
		}
		svc.mu.Unlock()

		log.Printf("[%s] exited (%v) — restarting in 3s", svc.Name, err)
		time.Sleep(3 * time.Second)

		cmd := buildCmd(binPath, args, env, logFile)
		if err := cmd.Start(); err != nil {
			log.Printf("[%s] restart failed: %v", svc.Name, err)
			return
		}

		svc.mu.Lock()
		svc.Cmd = cmd
		svc.mu.Unlock()

		log.Printf("[%s] restarted (PID %d)", svc.Name, cmd.Process.Pid)
	}
}

// buildCmd builds an exec.Cmd that writes stdout and stderr to the given log file.
func buildCmd(binPath string, args, env []string, logFile *os.File) *exec.Cmd {
	cmd := exec.Command(binPath, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	return cmd
}
