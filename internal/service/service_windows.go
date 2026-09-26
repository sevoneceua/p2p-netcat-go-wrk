//go:build windows

package service

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/santaklouse/go-p2p-netcat/internal/daemon"
	"github.com/santaklouse/go-p2p-netcat/internal/tunnelconfig"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// Install registers Name with the Windows Service Control Manager, set
// to start automatically at boot, running:
//
//	<this executable> service run-service --config <configPath>
//
// configPath is resolved to an absolute path first (a service can start
// with almost any working directory, so a relative path given at install
// time would silently break the moment Windows doesn't launch it from
// the directory the person happened to be in when they ran "install").
func Install(configPath string) error {
	if _, err := loadConfig(configPath); err != nil {
		return fmt.Errorf("config is invalid, not installing the service: %w", err)
	}
	absoluteConfigPath, err := absPath(configPath)
	if err != nil {
		return err
	}
	exePath, err := resolveExecutablePath()
	if err != nil {
		return err
	}

	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to the Service Control Manager (try running as Administrator): %w", err)
	}
	defer manager.Disconnect()

	if existing, err := manager.OpenService(Name); err == nil {
		existing.Close()
		return fmt.Errorf("service %q is already installed (uninstall it first)", Name)
	}

	created, err := manager.CreateService(Name, exePath, mgr.Config{
		DisplayName: DisplayName,
		Description: Description,
		StartType:   mgr.StartAutomatic,
	}, "service", "run-service", "--config", absoluteConfigPath)
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer created.Close()
	return nil
}

// Uninstall stops (if running) and removes Name from the Service Control
// Manager. It does not touch the config file, identity, or anything
// under oobe -- only the service registration.
func Uninstall() error {
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to the Service Control Manager (try running as Administrator): %w", err)
	}
	defer manager.Disconnect()

	service, err := manager.OpenService(Name)
	if err != nil {
		return fmt.Errorf("service %q is not installed: %w", Name, err)
	}
	defer service.Close()

	if status, err := service.Query(); err == nil && status.State != svc.Stopped {
		_, _ = service.Control(svc.Stop)
	}
	if err := service.Delete(); err != nil {
		return fmt.Errorf("delete service: %w", err)
	}
	return nil
}

// Start starts the already-installed service.
func Start() error {
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to the Service Control Manager (try running as Administrator): %w", err)
	}
	defer manager.Disconnect()

	service, err := manager.OpenService(Name)
	if err != nil {
		return fmt.Errorf("service %q is not installed: %w", Name, err)
	}
	defer service.Close()
	if err := service.Start(); err != nil {
		return fmt.Errorf("start service: %w", err)
	}
	return nil
}

// Stop stops the running service; it is not an error to call Stop on an
// already-stopped service.
func Stop() error {
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to the Service Control Manager (try running as Administrator): %w", err)
	}
	defer manager.Disconnect()

	service, err := manager.OpenService(Name)
	if err != nil {
		return fmt.Errorf("service %q is not installed: %w", Name, err)
	}
	defer service.Close()

	status, err := service.Query()
	if err != nil {
		return fmt.Errorf("query service status: %w", err)
	}
	if status.State == svc.Stopped {
		return nil
	}
	if _, err := service.Control(svc.Stop); err != nil {
		return fmt.Errorf("stop service: %w", err)
	}
	return nil
}

// StatusText reports the service's current SCM state as a short word
// ("running", "stopped", "start_pending", ...), or an error if it isn't
// installed.
func StatusText() (string, error) {
	manager, err := mgr.Connect()
	if err != nil {
		return "", fmt.Errorf("connect to the Service Control Manager (try running as Administrator): %w", err)
	}
	defer manager.Disconnect()

	service, err := manager.OpenService(Name)
	if err != nil {
		return "", fmt.Errorf("service %q is not installed: %w", Name, err)
	}
	defer service.Close()

	status, err := service.Query()
	if err != nil {
		return "", fmt.Errorf("query service status: %w", err)
	}
	return stateText(status.State), nil
}

func stateText(state svc.State) string {
	switch state {
	case svc.Stopped:
		return "stopped"
	case svc.StartPending:
		return "start_pending"
	case svc.StopPending:
		return "stop_pending"
	case svc.Running:
		return "running"
	case svc.ContinuePending:
		return "continue_pending"
	case svc.PausePending:
		return "pause_pending"
	case svc.Paused:
		return "paused"
	default:
		return "unknown"
	}
}

// RunAsService is the actual Windows service entry point: the hidden
// "p2p-nc service run-service --config <path>" command calls this
// instead of the ordinary daemon.Run + block-on-context-done that the
// plain "run" command uses, because a plain foreground process does not
// answer the Service Control Manager's start/stop control protocol --
// only svc.Run's Execute callback does. It builds and tears down the
// daemon exactly like "run" does; the difference is entirely in how
// stop is signaled (SCM control request here, Ctrl+C/SIGTERM there).
func RunAsService(configPath string) error {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return fmt.Errorf("determine whether running as a Windows service: %w", err)
	}
	if !isService {
		return fmt.Errorf("run-service must be started by the Service Control Manager, not run directly (use 'p2p-nc run --config <path>' instead)")
	}
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	return svc.Run(Name, &windowsHandler{cfg: cfg})
}

type windowsHandler struct {
	cfg *tunnelconfig.Config
}

func (h *windowsHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runningDaemon, err := daemon.Run(ctx, h.cfg, nil)
	if err != nil {
		changes <- svc.Status{State: svc.Stopped}
		return true, 1
	}

	changes <- svc.Status{State: svc.Running, Accepts: accepted}
	for request := range requests {
		switch request.Cmd {
		case svc.Interrogate:
			changes <- request.CurrentStatus
			// SCM expects an interrogate reply to be echoed back
			// promptly; a brief pause avoids flooding it if it
			// interrogates repeatedly in a tight loop.
			time.Sleep(100 * time.Millisecond)
			changes <- request.CurrentStatus
		case svc.Stop, svc.Shutdown:
			changes <- svc.Status{State: svc.StopPending}
			cancel()
			_ = runningDaemon.Close()
			changes <- svc.Status{State: svc.Stopped}
			return false, 0
		}
	}
	return false, 0
}

func absPath(path string) (string, error) {
	return filepath.Abs(path)
}
