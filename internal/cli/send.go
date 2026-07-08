package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"

	"github.com/PineappleBond/xyncra-server/pkg/client"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
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
	dialCtx, dialCancel := context.WithTimeout(ctx, 5*time.Second)
	defer dialCancel()

	url := cliCtx.ServerURLWithUser()
	ws, _, err := websocket.DefaultDialer.DialContext(dialCtx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", cliCtx.ServerURL, err)
	}
	defer ws.Close()

	clientMsgID := uuid.New().String()
	params, err := json.Marshal(map[string]any{
		"conversation_id":   convID,
		"content":           content,
		"client_message_id": clientMsgID,
		"reply_to":          replyTo,
	})
	if err != nil {
		return nil, fmt.Errorf("standalone marshal params: %w", err)
	}

	reqData, err := json.Marshal(protocol.PackageDataRequest{
		ID:     "1",
		Method: "send_message",
		Params: json.RawMessage(params),
	})
	if err != nil {
		return nil, fmt.Errorf("standalone marshal request: %w", err)
	}

	pkg := protocol.Package{
		Version: 1,
		Type:    protocol.PackageTypeRequest,
		Data:    json.RawMessage(reqData),
	}

	if err := ws.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return nil, fmt.Errorf("standalone set write deadline: %w", err)
	}
	if err := ws.WriteJSON(pkg); err != nil {
		return nil, fmt.Errorf("standalone write: %w", err)
	}

	if err := ws.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return nil, fmt.Errorf("standalone set read deadline: %w", err)
	}
	var respPkg protocol.Package
	if err := ws.ReadJSON(&respPkg); err != nil {
		var opErr net.Error
		if errors.As(err, &opErr) && opErr.Timeout() {
			return nil, fmt.Errorf("standalone read: server timed out")
		}
		return nil, fmt.Errorf("standalone read: %w", err)
	}

	var resp protocol.PackageDataResponse
	if err := json.Unmarshal(respPkg.Data, &resp); err != nil {
		return nil, fmt.Errorf("standalone unmarshal response: %w", err)
	}

	if resp.Code != protocol.ResponseCodeOK {
		return nil, &client.ClientError{Code: resp.Code, Message: resp.Msg}
	}

	var result client.SendMessageResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
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
