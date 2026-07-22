package cli

import (
	"context"
	"fmt"
	"os"
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

	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("agent-id")

	return cmd
}

// runAgentResume is the entry point for "agent-resume".
func runAgentResume(cmd *cobra.Command, _ []string) error {
	cliCtx, err := NewCLIContext(cmd)
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

	ipcClient := NewIPCClient(cliCtx.SocketPath(), 5*time.Second)

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
