package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/PineappleBond/xyncra-server/pkg/store"
	"github.com/PineappleBond/xyncra-server/pkg/store/model"
)

// ---------------------------------------------------------------------------
// delete-message (RPC — IPC first, standalone fallback — D-032)
// ---------------------------------------------------------------------------

// newDeleteMessageCommand creates the "delete-message" subcommand.
// D-038: --message-id is a string UUID (Message.ID), not a uint32.
func newDeleteMessageCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete-message",
		Short: "Soft-delete a message (sender only — D-014)",
		RunE:  runDeleteMessage,
	}

	cmd.Flags().String("message-id", "", "Message UUID to delete (required)")
	_ = cmd.MarkFlagRequired("message-id")

	return cmd
}

// runDeleteMessage is the entry point for "delete-message". It tries IPC first,
// then falls back to a standalone WebSocket connection (D-032).
func runDeleteMessage(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
	defer cancel()

	cliCtx, err := NewCLIContext(cmd)
	if err != nil {
		return fmt.Errorf("delete-message: %w", err)
	}

	msgID, _ := cmd.Flags().GetString("message-id")
	if msgID == "" {
		return errors.New("delete-message: --message-id is required")
	}

	ipcErr := deleteMessageViaIPC(ctx, cliCtx, msgID)
	if ipcErr == nil {
		fmt.Println("Message deleted.")
		return nil
	}

	wsErr := deleteMessageStandalone(ctx, cliCtx, msgID)
	if wsErr == nil {
		fmt.Println("Message deleted.")
		return nil
	}

	// Both modes failed — produce a unified error message.
	fmt.Fprintln(os.Stderr, "Error: Cannot delete message.")
	fmt.Fprintf(os.Stderr, "  Cause 1: %s\n", ipcErr)
	fmt.Fprintf(os.Stderr, "  Cause 2: %s\n", wsErr)
	fmt.Fprintln(os.Stderr, "Hint: Start the daemon first: xyncra-client listen --user-id <user>")
	return errors.New("delete-message: all delivery methods failed")
}

// deleteMessageViaIPC attempts to delete a message through the running daemon
// via the Unix socket IPC channel (D-030).
func deleteMessageViaIPC(ctx context.Context, cliCtx *CLIContext, msgID string) error {
	ipcClient := NewIPCClient(cliCtx.SocketPath(), 5*time.Second)

	resp, err := ipcClient.Call(ctx, "delete_message", map[string]any{
		"message_id": msgID,
	})
	if err != nil {
		return fmt.Errorf("ipc delete-message: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("ipc delete-message: %s", resp.Error.Message)
	}
	return nil
}

// deleteMessageStandalone deletes a message directly over a fresh WebSocket
// connection, bypassing the daemon. This is the fallback when the IPC channel
// is unavailable (D-032).
func deleteMessageStandalone(ctx context.Context, cliCtx *CLIContext, msgID string) error {
	_, err := standaloneRPC(ctx, cliCtx, "delete_message", map[string]any{
		"message_id": msgID,
	})
	if err != nil {
		return fmt.Errorf("standalone delete-message: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// mark-as-read (RPC — IPC first, standalone fallback — D-032)
// ---------------------------------------------------------------------------

// newMarkAsReadCommand creates the "mark-as-read" subcommand.
// D-038: --message-id is uint32 (Message.MessageID), not a string UUID.
// When --message-id is 0 the conversation's LastProcessedMessageID is read
// from the local database so the caller does not need to know the value (D-012).
func newMarkAsReadCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mark-as-read",
		Short: "Mark messages as read in a conversation (D-012)",
		RunE:  runMarkAsRead,
	}

	cmd.Flags().StringP("conversation-id", "c", "", "Conversation ID (required)")
	cmd.Flags().Uint32("message-id", 0, "Message sequence number to mark as read (0 = mark all as read)")

	_ = cmd.MarkFlagRequired("conversation-id")

	return cmd
}

// runMarkAsRead is the entry point for "mark-as-read". When --message-id is 0
// the last-processed sequence number is resolved from the local database (D-012).
// It tries IPC first, then falls back to a standalone WebSocket connection (D-032).
func runMarkAsRead(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
	defer cancel()

	cliCtx, err := NewCLIContext(cmd)
	if err != nil {
		return fmt.Errorf("mark-as-read: %w", err)
	}

	convID, _ := cmd.Flags().GetString("conversation-id")
	msgID, _ := cmd.Flags().GetUint32("message-id")

	if convID == "" {
		return errors.New("mark-as-read: --conversation-id is required")
	}

	// When --message-id is 0 ("mark all as read"), resolve from local DB.
	if msgID == 0 {
		resolved, resolveErr := resolveLastProcessedMessageID(ctx, cliCtx, convID)
		if resolveErr != nil {
			return resolveErr
		}
		msgID = resolved
	}

	confirmedID, ipcErr := markAsReadViaIPC(ctx, cliCtx, convID, msgID)
	if ipcErr == nil {
		fmt.Printf("Marked as read up to message #%d.\n", confirmedID)
		return nil
	}

	confirmedID, wsErr := markAsReadStandalone(ctx, cliCtx, convID, msgID)
	if wsErr == nil {
		fmt.Printf("Marked as read up to message #%d.\n", confirmedID)
		return nil
	}

	// Both modes failed — produce a unified error message.
	fmt.Fprintln(os.Stderr, "Error: Cannot mark as read.")
	fmt.Fprintf(os.Stderr, "  Cause 1: %s\n", ipcErr)
	fmt.Fprintf(os.Stderr, "  Cause 2: %s\n", wsErr)
	fmt.Fprintln(os.Stderr, "Hint: Start the daemon first: xyncra-client listen --user-id <user>")
	return errors.New("mark-as-read: all delivery methods failed")
}

// resolveLastProcessedMessageID reads the conversation's LastProcessedMessageID
// from the local SQLite database. This value is used when --message-id is 0
// (meaning "mark all as read") so the caller does not need to supply it (D-012).
func resolveLastProcessedMessageID(ctx context.Context, cliCtx *CLIContext, convID string) (uint32, error) {
	db, err := store.New(cliCtx.DBPath)
	if err != nil {
		return 0, fmt.Errorf("mark-as-read: open db: %w", err)
	}
	defer db.Close()

	conv, err := db.Conversations.Get(ctx, convID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return 0, fmt.Errorf("mark-as-read: conversation %s not found in local database; run 'xyncra-client listen' first", convID)
		}
		return 0, fmt.Errorf("mark-as-read: %w", err)
	}
	return conv.LastProcessedMessageID, nil
}

// markAsReadViaIPC attempts to mark messages as read through the running daemon
// via the Unix socket IPC channel (D-030). It returns the server-confirmed
// last_read_message_id so the CLI can display the actual cursor value.
func markAsReadViaIPC(ctx context.Context, cliCtx *CLIContext, convID string, msgID uint32) (uint32, error) {
	ipcClient := NewIPCClient(cliCtx.SocketPath(), 5*time.Second)

	resp, err := ipcClient.Call(ctx, "mark_as_read", map[string]any{
		"conversation_id": convID,
		"message_id":      msgID,
	})
	if err != nil {
		return 0, fmt.Errorf("ipc mark-as-read: %w", err)
	}
	if resp.Error != nil {
		return 0, fmt.Errorf("ipc mark-as-read: %s", resp.Error.Message)
	}

	// Parse the server-confirmed cursor from the response.
	var result struct {
		LastReadMessageID uint32 `json:"last_read_message_id"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return 0, fmt.Errorf("ipc mark-as-read unmarshal result: %w", err)
	}
	return result.LastReadMessageID, nil
}

// markAsReadStandalone marks messages as read directly over a fresh WebSocket
// connection, bypassing the daemon. This is the fallback when the IPC channel
// is unavailable (D-032). It returns the server-confirmed last_read_message_id
// so the CLI can display the actual cursor value (D-047).
func markAsReadStandalone(ctx context.Context, cliCtx *CLIContext, convID string, msgID uint32) (uint32, error) {
	raw, err := standaloneRPC(ctx, cliCtx, "mark_as_read", map[string]any{
		"conversation_id": convID,
		"message_id":      msgID,
	})
	if err != nil {
		return 0, fmt.Errorf("standalone mark-as-read: %w", err)
	}

	// Parse the server-confirmed cursor from the response (D-047).
	var result struct {
		LastReadMessageID uint32 `json:"last_read_message_id"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return 0, fmt.Errorf("standalone mark-as-read unmarshal result: %w", err)
	}
	return result.LastReadMessageID, nil
}

// ---------------------------------------------------------------------------
// get-messages (local DB — D-035)
// ---------------------------------------------------------------------------

// newGetMessagesCommand creates the "get-messages" subcommand.
// D-035: reads from the local database rather than issuing an RPC.
func newGetMessagesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get-messages",
		Short: "List messages from local database",
		RunE:  runGetMessages,
	}

	cmd.Flags().StringP("conversation-id", "c", "", "Conversation ID (required)")
	cmd.Flags().Uint32("after-message-id", 0, "Show messages after this sequence number")
	cmd.Flags().Int("limit", 50, "Maximum number of messages to show")

	_ = cmd.MarkFlagRequired("conversation-id")

	return cmd
}

// runGetMessages reads messages from the local SQLite database (D-035) and
// prints them in chronological order.
func runGetMessages(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()

	cliCtx, err := NewCLIContext(cmd)
	if err != nil {
		return fmt.Errorf("get-messages: %w", err)
	}

	convID, _ := cmd.Flags().GetString("conversation-id")
	afterMsgID, _ := cmd.Flags().GetUint32("after-message-id")
	limit, _ := cmd.Flags().GetInt("limit")
	if limit <= 0 {
		return fmt.Errorf("get-messages: --limit must be a positive integer")
	}

	if convID == "" {
		return errors.New("get-messages: --conversation-id is required")
	}

	db, err := store.New(cliCtx.DBPath)
	if err != nil {
		return fmt.Errorf("get-messages: open db: %w", err)
	}
	defer db.Close()

	// Fetch limit+1 to detect hasMore.
	msgs, err := db.Messages.ListByConversation(ctx, convID, afterMsgID, limit+1)
	if err != nil {
		return fmt.Errorf("get-messages: %w", err)
	}

	hasMore := len(msgs) > limit
	if hasMore {
		msgs = msgs[:limit]
	}

	printMessageList(msgs, hasMore)
	return nil
}

// ---------------------------------------------------------------------------
// search-messages (local DB — D-035)
// ---------------------------------------------------------------------------

// newSearchMessagesCommand creates the "search-messages" subcommand.
// D-035: reads from the local database rather than issuing an RPC.
func newSearchMessagesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search-messages",
		Short: "Search messages in local database",
		RunE:  runSearchMessages,
	}

	cmd.Flags().StringP("conversation-id", "c", "", "Conversation ID (required)")
	cmd.Flags().StringP("query", "q", "", "Search query (required)")
	cmd.Flags().Uint32("after-message-id", 0, "Pagination cursor: show messages with sequence number lower than this value (search returns DESC order)")
	cmd.Flags().Int("limit", 50, "Maximum number of messages to show")

	_ = cmd.MarkFlagRequired("conversation-id")
	_ = cmd.MarkFlagRequired("query")

	return cmd
}

// runSearchMessages searches messages from the local SQLite database (D-035)
// and prints them. Note: SearchByConversation returns results in DESC order
// (newest first).
func runSearchMessages(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()

	cliCtx, err := NewCLIContext(cmd)
	if err != nil {
		return fmt.Errorf("search-messages: %w", err)
	}

	convID, _ := cmd.Flags().GetString("conversation-id")
	query, _ := cmd.Flags().GetString("query")
	afterMsgID, _ := cmd.Flags().GetUint32("after-message-id")
	limit, _ := cmd.Flags().GetInt("limit")

	if convID == "" {
		return errors.New("search-messages: --conversation-id is required")
	}
	if query == "" {
		return errors.New("search-messages: --query is required")
	}

	db, err := store.New(cliCtx.DBPath)
	if err != nil {
		return fmt.Errorf("search-messages: open db: %w", err)
	}
	defer db.Close()

	// Fetch limit+1 to detect hasMore.
	msgs, err := db.Messages.SearchByConversation(ctx, convID, query, afterMsgID, limit+1)
	if err != nil {
		return fmt.Errorf("search-messages: %w", err)
	}

	hasMore := len(msgs) > limit
	if hasMore {
		msgs = msgs[:limit]
	}

	printMessageList(msgs, hasMore)
	return nil
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// printMessageList prints a list of messages in a consistent format.
// Each line: [#MessageID] SenderID (HH:MM): Content
func printMessageList(msgs []*model.Message, hasMore bool) {
	if len(msgs) == 0 {
		fmt.Println("No messages found.")
		return
	}

	for _, msg := range msgs {
		t := msg.CreatedAt.Format("15:04")
		fmt.Printf("[#%d] %s (%s): %s\n", msg.MessageID, msg.SenderID, t, msg.Content)
	}

	if hasMore {
		fmt.Println("(Use --after-message-id to see more)")
	}
}
