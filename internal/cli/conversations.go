package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/PineappleBond/xyncra-server/pkg/client"
	"github.com/PineappleBond/xyncra-server/pkg/store"
	"github.com/PineappleBond/xyncra-server/pkg/store/model"
)

// ---------------------------------------------------------------------------
// create-conversation
// ---------------------------------------------------------------------------

// newCreateConversationCommand creates the "create-conversation" subcommand.
// D-037: uses --peer-id instead of --user-id to avoid shadowing the global flag.
func newCreateConversationCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-conversation",
		Short: "Create a 1-on-1 conversation with another user",
		RunE:  runCreateConversation,
	}

	cmd.Flags().String("peer-id", "", "Peer user ID (required)")
	cmd.Flags().String("title", "", "Conversation title")

	_ = cmd.MarkFlagRequired("peer-id")

	return cmd
}

// runCreateConversation is the entry point for "create-conversation". It tries
// IPC first, then falls back to a standalone WebSocket connection (D-032).
func runCreateConversation(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
	defer cancel()

	cliCtx, err := NewCLIContext(cmd)
	if err != nil {
		return fmt.Errorf("create-conversation: %w", err)
	}

	peerID, _ := cmd.Flags().GetString("peer-id")
	title, _ := cmd.Flags().GetString("title")

	if peerID == "" {
		return errors.New("create-conversation: --peer-id is required")
	}

	result, ipcErr := createConversationViaIPC(ctx, cliCtx, peerID, title)
	if ipcErr == nil {
		printCreateConversationResult(result)
		return nil
	}

	result, wsErr := createConversationStandalone(ctx, cliCtx, peerID, title)
	if wsErr == nil {
		printCreateConversationResult(result)
		return nil
	}

	// Both modes failed — produce a unified error message.
	fmt.Fprintln(os.Stderr, "Error: Cannot create conversation.")
	fmt.Fprintf(os.Stderr, "  Cause 1: %s\n", ipcErr)
	fmt.Fprintf(os.Stderr, "  Cause 2: %s\n", wsErr)
	fmt.Fprintln(os.Stderr, "Hint: Start the daemon first: xyncra-client listen --user-id <user>")
	return errors.New("create-conversation: all delivery methods failed")
}

// createConversationViaIPC attempts to create a conversation through the
// running daemon via the Unix socket IPC channel (D-030).
func createConversationViaIPC(ctx context.Context, cliCtx *CLIContext, peerID, title string) (*client.CreateConversationResult, error) {
	ipcClient := NewIPCClient(cliCtx.SocketPath(), 5*time.Second)

	params := map[string]any{
		"user_id2": peerID,
		"title":    title,
	}

	resp, err := ipcClient.Call(ctx, "create_conversation", params)
	if err != nil {
		return nil, fmt.Errorf("ipc create-conversation: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("ipc create-conversation: %s", resp.Error.Message)
	}

	var result client.CreateConversationResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("ipc create-conversation unmarshal result: %w", err)
	}
	return &result, nil
}

// createConversationStandalone creates a conversation directly over a fresh
// WebSocket connection, bypassing the daemon. This is the fallback when the
// IPC channel is unavailable (D-032).
func createConversationStandalone(ctx context.Context, cliCtx *CLIContext, peerID, title string) (*client.CreateConversationResult, error) {
	data, err := standaloneRPC(ctx, cliCtx, "create_conversation", map[string]any{
		"user_id": peerID,
		"title":   title,
	})
	if err != nil {
		return nil, fmt.Errorf("standalone create-conversation: %w", err)
	}
	var result client.CreateConversationResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("standalone create-conversation unmarshal result: %w", err)
	}

	// RPC成功后同步本地DB（与IPC handler保持一致 — D-035）。
	if result.Conversation != nil {
		db, dbErr := store.New(cliCtx.DBPath)
		if dbErr == nil {
			defer db.Close()
			if err := db.Conversations.Upsert(ctx, result.Conversation); err != nil {
				fmt.Fprintf(os.Stderr, "[xyncra] warning: failed to persist created conversation locally: %v\n", err)
			}
		}
	}

	return &result, nil
}

// printCreateConversationResult prints the result of a successful
// create-conversation operation.
func printCreateConversationResult(result *client.CreateConversationResult) {
	if result.Duplicate {
		fmt.Println("Conversation already exists (find-or-create).")
	} else {
		fmt.Println("Conversation created.")
	}
	if result.Conversation != nil {
		fmt.Printf("  Conversation ID: %s\n", result.Conversation.ID)
		fmt.Printf("  Peer: %s\n", result.Conversation.UserID2)
		if result.Conversation.Title != "" {
			fmt.Printf("  Title: %s\n", result.Conversation.Title)
		}
	}
}

// ---------------------------------------------------------------------------
// delete-conversation
// ---------------------------------------------------------------------------

// newDeleteConversationCommand creates the "delete-conversation" subcommand.
func newDeleteConversationCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete-conversation",
		Short: "Soft-delete a conversation and all its messages",
		RunE:  runDeleteConversation,
	}

	cmd.Flags().StringP("conversation-id", "c", "", "Conversation ID (required)")

	_ = cmd.MarkFlagRequired("conversation-id")

	return cmd
}

// runDeleteConversation is the entry point for "delete-conversation". It tries
// IPC first, then falls back to a standalone WebSocket connection (D-032).
func runDeleteConversation(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
	defer cancel()

	cliCtx, err := NewCLIContext(cmd)
	if err != nil {
		return fmt.Errorf("delete-conversation: %w", err)
	}

	convID, _ := cmd.Flags().GetString("conversation-id")

	if convID == "" {
		return errors.New("delete-conversation: --conversation-id is required")
	}

	result, ipcErr := deleteConversationViaIPC(ctx, cliCtx, convID)
	if ipcErr == nil {
		printDeleteConversationResult(result)
		return nil
	}

	wsResult, wsErr := deleteConversationStandalone(ctx, cliCtx, convID)
	if wsErr == nil {
		printDeleteConversationResult(wsResult)
		return nil
	}

	// Both modes failed — produce a unified error message.
	fmt.Fprintln(os.Stderr, "Error: Cannot delete conversation.")
	fmt.Fprintf(os.Stderr, "  Cause 1: %s\n", ipcErr)
	fmt.Fprintf(os.Stderr, "  Cause 2: %s\n", wsErr)
	fmt.Fprintln(os.Stderr, "Hint: Start the daemon first: xyncra-client listen --user-id <user>")
	return errors.New("delete-conversation: all delivery methods failed")
}

// printDeleteConversationResult prints the result of a successful
// delete-conversation operation, including the cascade-deleted message count.
func printDeleteConversationResult(result *client.DeleteConversationResult) {
	fmt.Printf("Conversation deleted. %d message(s) removed.\n", result.DeletedMessageCount)
}

// deleteConversationViaIPC attempts to delete a conversation through the
// running daemon via the Unix socket IPC channel (D-030).
func deleteConversationViaIPC(ctx context.Context, cliCtx *CLIContext, convID string) (*client.DeleteConversationResult, error) {
	ipcClient := NewIPCClient(cliCtx.SocketPath(), 5*time.Second)

	params := map[string]any{
		"conversation_id": convID,
	}

	resp, err := ipcClient.Call(ctx, "delete_conversation", params)
	if err != nil {
		return nil, fmt.Errorf("ipc delete-conversation: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("ipc delete-conversation: %s", resp.Error.Message)
	}

	var result client.DeleteConversationResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("ipc delete-conversation unmarshal result: %w", err)
	}
	return &result, nil
}

// deleteConversationStandalone deletes a conversation directly over a fresh
// WebSocket connection, bypassing the daemon. This is the fallback when the
// IPC channel is unavailable (D-032).
func deleteConversationStandalone(ctx context.Context, cliCtx *CLIContext, convID string) (*client.DeleteConversationResult, error) {
	data, err := standaloneRPC(ctx, cliCtx, "delete_conversation", map[string]any{
		"conversation_id": convID,
	})
	if err != nil {
		return nil, fmt.Errorf("standalone delete-conversation: %w", err)
	}
	var result client.DeleteConversationResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("standalone delete-conversation unmarshal result: %w", err)
	}

	// RPC成功后同步本地DB（与IPC handler保持一致 — D-035）。
	db, dbErr := store.New(cliCtx.DBPath)
	if dbErr == nil {
		defer db.Close()
		if err := db.Conversations.Delete(ctx, convID); err != nil && !errors.Is(err, store.ErrNotFound) {
			fmt.Fprintf(os.Stderr, "[xyncra] warning: failed to delete conversation locally: %v\n", err)
		}
	}

	return &result, nil
}

// ---------------------------------------------------------------------------
// restore-conversation
// ---------------------------------------------------------------------------

// newRestoreConversationCommand creates the "restore-conversation" subcommand.
func newRestoreConversationCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore-conversation",
		Short: "Restore a previously soft-deleted conversation",
		RunE:  runRestoreConversation,
	}

	cmd.Flags().StringP("conversation-id", "c", "", "Conversation ID (required)")

	_ = cmd.MarkFlagRequired("conversation-id")

	return cmd
}

// runRestoreConversation is the entry point for "restore-conversation". It
// tries IPC first, then falls back to a standalone WebSocket connection (D-032).
func runRestoreConversation(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
	defer cancel()

	cliCtx, err := NewCLIContext(cmd)
	if err != nil {
		return fmt.Errorf("restore-conversation: %w", err)
	}

	convID, _ := cmd.Flags().GetString("conversation-id")

	if convID == "" {
		return errors.New("restore-conversation: --conversation-id is required")
	}

	result, ipcErr := restoreConversationViaIPC(ctx, cliCtx, convID)
	if ipcErr == nil {
		printRestoreConversationResult(result)
		return nil
	}

	wsResult, wsErr := restoreConversationStandalone(ctx, cliCtx, convID)
	if wsErr == nil {
		printRestoreConversationResult(wsResult)
		return nil
	}

	// Both modes failed — produce a unified error message.
	fmt.Fprintln(os.Stderr, "Error: Cannot restore conversation.")
	fmt.Fprintf(os.Stderr, "  Cause 1: %s\n", ipcErr)
	fmt.Fprintf(os.Stderr, "  Cause 2: %s\n", wsErr)
	fmt.Fprintln(os.Stderr, "Hint: Start the daemon first: xyncra-client listen --user-id <user>")
	return errors.New("restore-conversation: all delivery methods failed")
}

// printRestoreConversationResult prints the result of a successful
// restore-conversation operation, including the cascade-restored message count.
func printRestoreConversationResult(result *client.RestoreConversationResult) {
	fmt.Printf("Conversation restored. %d message(s) recovered.\n", result.RestoredMessageCount)
}

// restoreConversationViaIPC attempts to restore a conversation through the
// running daemon via the Unix socket IPC channel (D-030).
func restoreConversationViaIPC(ctx context.Context, cliCtx *CLIContext, convID string) (*client.RestoreConversationResult, error) {
	ipcClient := NewIPCClient(cliCtx.SocketPath(), 5*time.Second)

	params := map[string]any{
		"conversation_id": convID,
	}

	resp, err := ipcClient.Call(ctx, "restore_conversation", params)
	if err != nil {
		return nil, fmt.Errorf("ipc restore-conversation: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("ipc restore-conversation: %s", resp.Error.Message)
	}

	var result client.RestoreConversationResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("ipc restore-conversation unmarshal result: %w", err)
	}
	return &result, nil
}

// restoreConversationStandalone restores a conversation directly over a fresh
// WebSocket connection, bypassing the daemon. This is the fallback when the
// IPC channel is unavailable (D-032).
func restoreConversationStandalone(ctx context.Context, cliCtx *CLIContext, convID string) (*client.RestoreConversationResult, error) {
	data, err := standaloneRPC(ctx, cliCtx, "restore_conversation", map[string]any{
		"conversation_id": convID,
	})
	if err != nil {
		return nil, fmt.Errorf("standalone restore-conversation: %w", err)
	}
	var result client.RestoreConversationResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("standalone restore-conversation unmarshal result: %w", err)
	}

	// RPC成功后同步本地DB（与IPC handler保持一致 — D-035）。
	db, dbErr := store.New(cliCtx.DBPath)
	if dbErr == nil {
		defer db.Close()
		if err := db.Conversations.Restore(ctx, convID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				// 本地记录不存在，跳过（standalone路径无法从服务器拉取补录，仅警告）。
				fmt.Fprintf(os.Stderr, "[xyncra] warning: conversation %s not found in local DB, skipping local restore\n", convID)
			} else {
				fmt.Fprintf(os.Stderr, "[xyncra] warning: failed to restore conversation locally: %v\n", err)
			}
		}
	}

	return &result, nil
}

// ---------------------------------------------------------------------------
// list-conversations (local DB — D-035)
// ---------------------------------------------------------------------------

// newListConversationsCommand creates the "list-conversations" subcommand.
// D-035: reads from the local database rather than issuing an RPC.
func newListConversationsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-conversations",
		Short: "List conversations from local database",
		RunE:  runListConversations,
	}

	cmd.Flags().Int("offset", 0, "Pagination offset")
	cmd.Flags().Int("limit", 20, "Maximum number of conversations to show")

	return cmd
}

// runListConversations reads conversations from the local SQLite database
// (D-035) and prints a summary list.
func runListConversations(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()

	cliCtx, err := NewCLIContext(cmd)
	if err != nil {
		return fmt.Errorf("list-conversations: %w", err)
	}

	offset, _ := cmd.Flags().GetInt("offset")
	limit, _ := cmd.Flags().GetInt("limit")

	db, err := store.New(cliCtx.DBPath)
	if err != nil {
		return fmt.Errorf("list-conversations: open db: %w", err)
	}
	defer db.Close()

	// Fetch limit+1 to detect hasMore.
	convs, err := db.Conversations.GetByUser(ctx, cliCtx.UserID, offset, limit+1)
	if err != nil {
		return fmt.Errorf("list-conversations: %w", err)
	}

	hasMore := len(convs) > limit
	if hasMore {
		convs = convs[:limit]
	}

	printConversationList(convs, cliCtx.UserID, hasMore)
	return nil
}

// printConversationList prints a summary list of conversations.
func printConversationList(convs []*model.Conversation, currentUserID string, hasMore bool) {
	if len(convs) == 0 {
		fmt.Println("No conversations found. Run 'xyncra-client listen' first to sync data.")
		return
	}

	fmt.Printf("%-38s  %-20s  %-30s  %s\n", "ID", "Peer", "Title", "Last Message")
	fmt.Println("-------------------------------------------------------------------------------------------")
	for _, conv := range convs {
		peer := conv.UserID2
		if conv.UserID2 == currentUserID {
			peer = conv.UserID1
		}
		title := conv.Title
		if title == "" {
			title = "-"
		}
		lastMsg := "-"
		if !conv.LastMessageAt.IsZero() {
			lastMsg = conv.LastMessageAt.Format("2006-01-02 15:04:05")
		}
		fmt.Printf("%-38s  %-20s  %-30s  %s\n", conv.ID, peer, title, lastMsg)
	}

	if hasMore {
		fmt.Println("... more conversations available (use --offset to paginate)")
	}
}

// ---------------------------------------------------------------------------
// get-conversation (local DB — D-035)
// ---------------------------------------------------------------------------

// newGetConversationCommand creates the "get-conversation" subcommand.
// D-035: reads from the local database rather than issuing an RPC.
func newGetConversationCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get-conversation",
		Short: "Show conversation details from local database",
		RunE:  runGetConversation,
	}

	cmd.Flags().StringP("conversation-id", "c", "", "Conversation ID (required)")

	_ = cmd.MarkFlagRequired("conversation-id")

	return cmd
}

// runGetConversation reads a single conversation from the local SQLite database
// (D-035), computes the unread count, and prints the details.
func runGetConversation(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()

	cliCtx, err := NewCLIContext(cmd)
	if err != nil {
		return fmt.Errorf("get-conversation: %w", err)
	}

	convID, _ := cmd.Flags().GetString("conversation-id")

	if convID == "" {
		return errors.New("get-conversation: --conversation-id is required")
	}

	db, err := store.New(cliCtx.DBPath)
	if err != nil {
		return fmt.Errorf("get-conversation: open db: %w", err)
	}
	defer db.Close()

	conv, err := db.Conversations.Get(ctx, convID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("get-conversation: conversation %s not found", convID)
		}
		return fmt.Errorf("get-conversation: %w", err)
	}

	// Determine the current user's read cursor position (D-012).
	var readCursor uint32
	if conv.UserID1 == cliCtx.UserID {
		readCursor = conv.LastReadMessageID1
	} else {
		readCursor = conv.LastReadMessageID2
	}

	unreadCount, err := db.Messages.CountUnread(ctx, convID, readCursor)
	if err != nil {
		return fmt.Errorf("get-conversation: count unread: %w", err)
	}

	printConversationDetail(conv, cliCtx.UserID, unreadCount)
	return nil
}

// printConversationDetail prints detailed information about a single
// conversation, including the unread message count.
func printConversationDetail(conv *model.Conversation, currentUserID string, unreadCount int64) {
	peer := conv.UserID2
	if conv.UserID2 == currentUserID {
		peer = conv.UserID1
	}
	fmt.Println("Conversation Details")
	fmt.Printf("  ID:           %s\n", conv.ID)
	fmt.Printf("  Type:         %s\n", conv.Type)
	fmt.Printf("  User 1:       %s\n", conv.UserID1)
	fmt.Printf("  User 2:       %s\n", conv.UserID2)
	fmt.Printf("  Peer:         %s\n", peer)
	if conv.Title != "" {
		fmt.Printf("  Title:        %s\n", conv.Title)
	}
	fmt.Printf("  Created:      %s\n", conv.CreatedAt.Format("2006-01-02 15:04:05"))
	lastMsg := "-"
	if !conv.LastMessageAt.IsZero() {
		lastMsg = conv.LastMessageAt.Format("2006-01-02 15:04:05")
	}
	fmt.Printf("  Last Message: %s\n", lastMsg)
	fmt.Printf("  Unread:       %d\n", unreadCount)
}
