package cli

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

// Package-level function variables for testability.
var (
	osFindProcess   = os.FindProcess
	osSignalProcess = func(p *os.Process, sig os.Signal) error { return p.Signal(sig) }
)

// errKillTimeout is returned when the process does not exit within the timeout.
var errKillTimeout = errors.New("process did not respond to SIGTERM")

func newKillCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kill",
		Short: "Terminate the running listen daemon",
		RunE:  runKill,
	}
	cmd.Flags().Bool("force", false, "Force kill with SIGKILL instead of SIGTERM")
	cmd.Flags().Duration("timeout", 5*time.Second, "Timeout to wait for process to exit")
	return cmd
}

func runKill(cmd *cobra.Command, args []string) error {
	cliCtx, err := NewCLIContext(cmd)
	if err != nil {
		return fmt.Errorf("kill: %w", err)
	}

	force, _ := cmd.Flags().GetBool("force")
	timeout, _ := cmd.Flags().GetDuration("timeout")

	// 1. Read lock file
	info, err := readLockInfo(cliCtx.LockPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Lock file does not exist — no daemon running.
			fmt.Fprintln(os.Stderr, "No running daemon found.")
			return fmt.Errorf("kill: no running daemon found")
		}
		return fmt.Errorf("kill: %w", err)
	}

	// 2. Check if process is alive
	if !isProcessAlive(info.PID) {
		fmt.Fprintf(os.Stderr, "Daemon process (PID: %d) is not running. Cleaning up stale files.\n", info.PID)
		cleanupDaemonFiles(cliCtx)
		return nil
	}

	// 3. Determine signal
	var sig os.Signal
	if force {
		sig = syscall.SIGKILL
	} else {
		sig = syscall.SIGTERM
	}

	// 4. Send signal and wait
	if err := terminateProcess(info.PID, sig, timeout); err != nil {
		if errors.Is(err, errKillTimeout) {
			fmt.Fprintf(os.Stderr, "Error: %v within %s. Use --force to force kill\n", err, timeout)
			os.Exit(3)
		}
		return fmt.Errorf("kill: %w", err)
	}

	// 5. Cleanup files
	cleanupDaemonFiles(cliCtx)
	fmt.Fprintf(os.Stderr, "Daemon terminated (PID: %d). Files cleaned up.\n", info.PID)
	return nil
}

// terminateProcess sends sig to the process and waits up to timeout for it to exit.
func terminateProcess(pid int, sig os.Signal, timeout time.Duration) error {
	p, err := osFindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}

	if err := osSignalProcess(p, sig); err != nil {
		return fmt.Errorf("signal process: %w", err)
	}

	// Poll for process exit
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !isProcessAlive(pid) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Timeout
	if sig == syscall.SIGTERM {
		return errKillTimeout
	}

	// SIGKILL timeout — this shouldn't normally happen.
	fmt.Fprintf(os.Stderr, "Warning: process %d did not exit after SIGKILL\n", pid)
	return nil
}
