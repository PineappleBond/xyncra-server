package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// agent-resume (IPC-only, D-085, D-137)
// ---------------------------------------------------------------------------

// newAgentResumeCommand creates the "agent-resume" subcommand.
// D-085: Resume a paused agent after HITL interruption.
// D-137: Updated to use RemoteCalling unified model.
// D-114: IPC-only — the daemon forwards the request to the server via
// WebSocket; if the daemon is not running there is nothing to forward.
func newAgentResumeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent-resume",
		Short: "Resume a paused agent after HITL interruption (D-085, D-137, IPC-only)",
		Long: `Resume an agent that is waiting for human input after a HITL
(Human-In-The-Loop) interruption. The agent must have been paused by an
ask_user tool call.

This command is IPC-only (D-036, D-114) — it requires the listen daemon
to be running. Start the daemon with 'xyncra-client listen' first.

Typical workflow:
  1. Run 'xyncra-client listen' to receive HITL notifications
  2. Note the remote_calling_id from the notification
  3. Run 'xyncra-client agent-resume' with the result`,
		RunE: runAgentResume,
	}

	cmd.Flags().String("id", "", "RemoteCalling ID (required)")
	cmd.Flags().Bool("success", true, "Whether the call succeeded (default true)")
	cmd.Flags().String("result", "", "Result on success")
	cmd.Flags().String("error-message", "", "Error message on failure")
	cmd.Flags().String("agent-id", "", "Agent user ID to resume (must match a registered agent ID, e.g. agent/my-bot; required)")
	cmd.Flags().String("socket-path", "", "Explicit path to daemon IPC socket (overrides auto-discovery)")

	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("agent-id")

	return cmd
}

// runAgentResume is the entry point for "agent-resume".
func runAgentResume(cmd *cobra.Command, _ []string) error {
	// Resolve the IPC socket path. Try multiple strategies:
	// 1. Explicit --socket-path flag (highest priority).
	// 2. Derive from CLIContext (requires --user-id and --device-id).
	// 3. Derive from --db-path parent directory.
	// 4. Auto-discover by scanning ~/.xyncra/*/xyncra.sock.
	socketPath, err := resolveAgentResumeSocketPath(cmd)
	if err != nil {
		return fmt.Errorf("agent-resume: %w", err)
	}

	id, _ := cmd.Flags().GetString("id")
	success, _ := cmd.Flags().GetBool("success")
	result, _ := cmd.Flags().GetString("result")
	errorMessage, _ := cmd.Flags().GetString("error-message")
	agentID, _ := cmd.Flags().GetString("agent-id")

	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()

	ipcClient := NewIPCClient(socketPath, 5*time.Second)

	resp, err := ipcClient.Call(ctx, "agent_resume", map[string]any{
		"id":            id,
		"success":       success,
		"result":        result,
		"error_message": errorMessage,
		"agent_id":      agentID,
	})
	if err != nil {
		// IPC connection failed — daemon is not running.
		// D-036/D-042: exit code 2 = precondition not met.
		fmt.Fprintln(os.Stderr, "Error: daemon not running.")
		fmt.Fprintln(os.Stderr, "Hint: Start with 'xyncra-client listen --user-id <user>'")
		fmt.Fprintf(os.Stderr, "Socket path tried: %s\n", socketPath)
		os.Exit(2)
	}

	if resp.Error != nil {
		return fmt.Errorf("agent-resume: %s", resp.Error.Message)
	}

	fmt.Println("Agent resume queued successfully")
	fmt.Printf("  id:      %s\n", id)
	fmt.Printf("  success: %v\n", success)
	return nil
}

// resolveAgentResumeSocketPath determines the IPC socket path using multiple
// fallback strategies. This fixes the issue where agent-resume requires
// explicit --db-path to find the daemon socket (BUG-003).
func resolveAgentResumeSocketPath(cmd *cobra.Command) (string, error) {
	// Strategy 1: Explicit --socket-path flag.
	if cmd.Flags().Changed("socket-path") {
		p, _ := cmd.Flags().GetString("socket-path")
		if p != "" {
			return p, nil
		}
	}

	// Strategy 2: Derive from CLIContext (requires --user-id and --device-id).
	cliCtx, err := NewCLIContext(cmd)
	if err == nil {
		sockPath := cliCtx.SocketPath()
		// Verify the socket file exists before returning.
		if _, statErr := os.Stat(sockPath); statErr == nil {
			return sockPath, nil
		}
		// Socket not at expected path — fall through to other strategies.
	}

	// Strategy 3: Derive from --db-path parent directory.
	// The db is at UserDir/xyncra.db, so the socket is at UserDir/xyncra.sock.
	if cmd.Flags().Changed("db-path") {
		dbPath, _ := cmd.Flags().GetString("db-path")
		if dbPath != "" {
			sockPath := filepath.Join(filepath.Dir(dbPath), "xyncra.sock")
			if _, statErr := os.Stat(sockPath); statErr == nil {
				return sockPath, nil
			}
		}
	}

	// Strategy 4: Auto-discover by scanning ~/.xyncra/*/xyncra.sock.
	home, err := os.UserHomeDir()
	if err == nil {
		matches, _ := filepath.Glob(filepath.Join(home, ".xyncra", "*", "*", "xyncra.sock"))
		for _, m := range matches {
			if _, statErr := os.Stat(m); statErr == nil {
				return m, nil
			}
		}
	}

	return "", fmt.Errorf("cannot find daemon socket; ensure the daemon is running and provide --user-id/--device-id or --socket-path")
}
