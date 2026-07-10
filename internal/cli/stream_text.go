package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// stream-text (IPC-only, D-036)
// ---------------------------------------------------------------------------

// newStreamTextCommand creates the "stream-text" subcommand.
// D-036: IPC-only — streaming is fire-and-forget (D-051); if the daemon is not
// running there is nothing to broadcast, so standalone fallback is pointless.
func newStreamTextCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stream-text",
		Short: "Send streaming text to a conversation (D-051)",
		RunE:  runStreamText,
	}

	cmd.Flags().StringP("conversation-id", "c", "", "Conversation ID (required)")
	cmd.Flags().String("stream-id", "", "Stream ID (required, client-generated UUID)")
	cmd.Flags().String("text", "", "Cumulative text content (required)")
	cmd.Flags().Bool("done", false, "Mark stream as done (is_done=true)")
	_ = cmd.MarkFlagRequired("conversation-id")
	_ = cmd.MarkFlagRequired("stream-id")
	_ = cmd.MarkFlagRequired("text")

	return cmd
}

// runStreamText is the entry point for "stream-text".
func runStreamText(cmd *cobra.Command, _ []string) error {
	cliCtx, err := NewCLIContext(cmd)
	if err != nil {
		return fmt.Errorf("stream-text: %w", err)
	}

	convID, _ := cmd.Flags().GetString("conversation-id")
	streamID, _ := cmd.Flags().GetString("stream-id")
	text, _ := cmd.Flags().GetString("text")
	done, _ := cmd.Flags().GetBool("done")

	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()

	ipcClient := NewIPCClient(cliCtx.SocketPath(), 5*time.Second)

	resp, err := ipcClient.Call(ctx, "stream_text", map[string]any{
		"conversation_id": convID,
		"stream_id":       streamID,
		"text":            text,
		"is_done":         done,
	})
	if err != nil {
		// IPC connection failed — daemon is not running.
		// D-036/D-042: exit code 2 = precondition not met.
		fmt.Fprintln(os.Stderr, "Error: daemon not running.")
		fmt.Fprintln(os.Stderr, "Hint: Start with 'xyncra-client listen --user-id <user>'")
		os.Exit(2)
	}

	if resp.Error != nil {
		return fmt.Errorf("stream-text: %s", resp.Error.Message)
	}

	if done {
		fmt.Printf("Streaming done sent to conversation %s\n", convID)
	} else {
		fmt.Printf("Streaming text sent to conversation %s\n", convID)
	}
	return nil
}
