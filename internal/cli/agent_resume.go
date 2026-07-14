package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// agent-resume (IPC-only, D-085, D-114)
// ---------------------------------------------------------------------------

// newAgentResumeCommand creates the "agent-resume" subcommand.
// D-085: Resume a paused agent after HITL interruption.
// D-114: IPC-only — the daemon forwards the request to the server via
// WebSocket; if the daemon is not running there is nothing to forward.
func newAgentResumeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent-resume",
		Short: "Resume a paused agent after HITL interruption (D-085, IPC-only)",
		Long: `Resume an agent that is waiting for human input after a HITL
(Human-In-The-Loop) interruption. The agent must have been paused by an
ask_user tool call.

This command is IPC-only (D-036, D-114) — it requires the listen daemon
to be running. Start the daemon with 'xyncra-client listen' first.

Typical workflow:
  1. Run 'xyncra-client listen' to receive agent_question events
  2. Note the checkpoint_id and interrupt_id from the event
  3. Run 'xyncra-client agent-resume' with the answer`,
		RunE: runAgentResume,
	}

	cmd.Flags().String("conversation-id", "", "Conversation ID (required)")
	cmd.Flags().String("checkpoint-id", "", "Checkpoint ID from agent_question event (required)")
	cmd.Flags().String("interrupt-id", "", "Interrupt ID from agent_question event (optional)")
	cmd.Flags().String("answer", "", "Answer to the agent's question (required)")
	cmd.Flags().String("agent-id", "", "Agent ID to resume (e.g. agent/xxx, required)")

	_ = cmd.MarkFlagRequired("conversation-id")
	_ = cmd.MarkFlagRequired("checkpoint-id")
	_ = cmd.MarkFlagRequired("answer")
	_ = cmd.MarkFlagRequired("agent-id")

	return cmd
}

// runAgentResume is the entry point for "agent-resume".
func runAgentResume(cmd *cobra.Command, _ []string) error {
	cliCtx, err := NewCLIContext(cmd)
	if err != nil {
		return fmt.Errorf("agent-resume: %w", err)
	}

	convID, _ := cmd.Flags().GetString("conversation-id")
	checkpointID, _ := cmd.Flags().GetString("checkpoint-id")
	interruptID, _ := cmd.Flags().GetString("interrupt-id")
	answer, _ := cmd.Flags().GetString("answer")
	agentID, _ := cmd.Flags().GetString("agent-id")

	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()

	ipcClient := NewIPCClient(cliCtx.SocketPath(), 5*time.Second)

	resp, err := ipcClient.Call(ctx, "agent_resume", map[string]any{
		"conversation_id": convID,
		"checkpoint_id":   checkpointID,
		"interrupt_id":    interruptID,
		"answer":          answer,
		"agent_id":        agentID,
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
	fmt.Printf("  conversation: %s\n", convID)
	fmt.Printf("  checkpoint:   %s\n", checkpointID)
	return nil
}
