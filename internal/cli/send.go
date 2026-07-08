package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/PineappleBond/xyncra-server/pkg/client"
)

// newSendCommand creates the "send" subcommand for sending a message.
func newSendCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "send",
		Short: "Send a message to a conversation",
		RunE:  runSend,
	}

	cmd.Flags().StringP("conversation-id", "c", "", "Conversation ID (required)")
	cmd.Flags().StringP("content", "m", "", "Message content (required)")
	cmd.Flags().Uint32("reply-to", 0, "Message ID to reply to")

	_ = cmd.MarkFlagRequired("conversation-id")
	_ = cmd.MarkFlagRequired("content")

	return cmd
}

// runSend is the entry point for the "send" subcommand. It tries IPC first,
// then falls back to a standalone WebSocket connection (D-032).
func runSend(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
	defer cancel()

	cliCtx, err := NewCLIContext(cmd)
	if err != nil {
		return fmt.Errorf("send: %w", err)
	}

	convID, _ := cmd.Flags().GetString("conversation-id")
	content, _ := cmd.Flags().GetString("content")
	replyTo, _ := cmd.Flags().GetUint32("reply-to")

	if convID == "" {
		return errors.New("send: --conversation-id is required")
	}
	if content == "" {
		return errors.New("send: --content is required")
	}

	result, ipcErr := sendViaIPC(ctx, cliCtx, convID, content, replyTo)
	if ipcErr == nil {
		printSendResult(result)
		return nil
	}

	result, wsErr := sendStandalone(ctx, cliCtx, convID, content, replyTo)
	if wsErr == nil {
		printSendResult(result)
		return nil
	}

	// Both modes failed — produce a unified error message.
	fmt.Fprintln(os.Stderr, "Error: Cannot send message.")
	fmt.Fprintf(os.Stderr, "  Cause 1: %s\n", ipcErr)
	fmt.Fprintf(os.Stderr, "  Cause 2: %s\n", wsErr)
	fmt.Fprintln(os.Stderr, "Hint: Start the daemon first: xyncra-client listen --user-id <user>")
	return errors.New("send: all delivery methods failed")
}

// sendViaIPC attempts to send a message through the running daemon via the
// Unix socket IPC channel (D-030).
func sendViaIPC(ctx context.Context, cliCtx *CLIContext, convID, content string, replyTo uint32) (*client.SendMessageResult, error) {
	ipcClient := NewIPCClient(cliCtx.SocketPath(), 5*time.Second)

	params := map[string]any{
		"conversation_id": convID,
		"content":         content,
		"reply_to":        replyTo,
	}

	resp, err := ipcClient.Call(ctx, "send_message", params)
	if err != nil {
		return nil, fmt.Errorf("ipc send: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("ipc send: %s", resp.Error.Message)
	}

	var result client.SendMessageResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("ipc send unmarshal result: %w", err)
	}
	return &result, nil
}

// sendStandalone sends a message directly over a fresh WebSocket connection,
// bypassing the daemon. This is the fallback when the IPC channel is
// unavailable (D-032).
func sendStandalone(ctx context.Context, cliCtx *CLIContext, convID, content string, replyTo uint32) (*client.SendMessageResult, error) {
	clientMsgID := uuid.New().String()
	data, err := standaloneRPC(ctx, cliCtx, "send_message", map[string]any{
		"conversation_id":   convID,
		"content":           content,
		"client_message_id": clientMsgID,
		"reply_to":          replyTo,
	})
	if err != nil {
		return nil, fmt.Errorf("standalone send: %w", err)
	}
	var result client.SendMessageResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("standalone unmarshal result: %w", err)
	}
	return &result, nil
}

// printSendResult prints the result of a successful send operation.
func printSendResult(result *client.SendMessageResult) {
	fmt.Println("Message sent.")
	if result.Message != nil {
		fmt.Printf("  Message ID: %d\n", result.Message.MessageID)
		fmt.Printf("  Conversation: %s\n", result.Message.ConversationID)
		fmt.Printf("  Client Msg ID: %s\n", result.Message.ClientMessageID)
	}
	fmt.Printf("  Duplicate: %t\n", result.Duplicate)
}
