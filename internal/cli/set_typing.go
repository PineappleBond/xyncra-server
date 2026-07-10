package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// set-typing (IPC-only, D-036)
// ---------------------------------------------------------------------------

// newSetTypingCommand creates the "set-typing" subcommand.
// D-036: IPC-only — typing is fire-and-forget (D-050); if the daemon is not
// running there is nothing to broadcast, so standalone fallback is pointless.
func newSetTypingCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-typing",
		Short: "Send a typing indicator to a conversation (D-050)",
		RunE:  runSetTyping,
	}

	cmd.Flags().StringP("conversation-id", "c", "", "Conversation ID (required)")
	cmd.Flags().Bool("stop", false, "Stop typing (default: start typing)")
	_ = cmd.MarkFlagRequired("conversation-id")

	return cmd
}

// runSetTyping is the entry point for "set-typing".
func runSetTyping(cmd *cobra.Command, _ []string) error {
	cliCtx, err := NewCLIContext(cmd)
	if err != nil {
		return fmt.Errorf("set-typing: %w", err)
	}

	convID, _ := cmd.Flags().GetString("conversation-id")
	stop, _ := cmd.Flags().GetBool("stop")

	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()

	ipcClient := NewIPCClient(cliCtx.SocketPath(), 5*time.Second)

	resp, err := ipcClient.Call(ctx, "set_typing", map[string]any{
		"conversation_id": convID,
		"is_typing":       !stop,
	})
	if err != nil {
		// IPC connection failed — daemon is not running.
		// D-036/D-042: exit code 2 = precondition not met.
		fmt.Fprintln(os.Stderr, "Error: daemon not running.")
		fmt.Fprintln(os.Stderr, "Hint: Start with 'xyncra-client listen --user-id <user>'")
		os.Exit(2)
	}

	if resp.Error != nil {
		return fmt.Errorf("set-typing: %s", resp.Error.Message)
	}

	if stop {
		fmt.Printf("Typing indicator cleared for conversation %s\n", convID)
	} else {
		fmt.Printf("Typing indicator sent to conversation %s\n", convID)
	}
	return nil
}
