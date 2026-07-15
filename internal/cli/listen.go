package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"gorm.io/gorm"

	"github.com/PineappleBond/xyncra-server/pkg/client"
	"github.com/PineappleBond/xyncra-server/pkg/store"
	"github.com/PineappleBond/xyncra-server/pkg/store/model"
)

// defaultLogRetention is the default retention period for client-side logs
// (RPC logs and notification logs). Records older than this are hard-deleted
// by the periodic cleanup goroutine (D-040).
const defaultLogRetention = 7 * 24 * time.Hour // 7 days

// defaultCleanupInterval is the interval between automatic log cleanup runs.
const defaultCleanupInterval = 1 * time.Hour

// ---------------------------------------------------------------------------
// cliUpdateHandler — implements client.UpdateHandler
// ---------------------------------------------------------------------------

// cliUpdateHandler is an UpdateHandler that prints received updates to stdout.
type cliUpdateHandler struct{}

// newCLIUpdateHandler creates a new cliUpdateHandler.
func newCLIUpdateHandler() *cliUpdateHandler {
	return &cliUpdateHandler{}
}

// OnMessage prints a new or updated message to stdout.
func (h *cliUpdateHandler) OnMessage(_ context.Context, msg *model.Message) error {
	if msg == nil {
		return nil
	}
	fmt.Fprintf(os.Stdout, "[new message] seq=%d from=%s conv=%s %q\n",
		msg.MessageID, msg.SenderID, msg.ConversationID, msg.Content)
	return nil
}

// OnDeleteMessage prints a message deletion event to stdout.
func (h *cliUpdateHandler) OnDeleteMessage(_ context.Context, messageID string, conversationID string) error {
	fmt.Fprintf(os.Stdout, "[delete message] conv=%s msg=%s\n", conversationID, messageID)
	return nil
}

// OnMarkRead prints a read-cursor advance event to stdout.
func (h *cliUpdateHandler) OnMarkRead(_ context.Context, conversationID string, messageID uint32) error {
	fmt.Fprintf(os.Stdout, "[mark read] conv=%s msg_id=%d\n", conversationID, messageID)
	return nil
}

// OnConversation prints a conversation state change to stdout.
func (h *cliUpdateHandler) OnConversation(_ context.Context, conv *model.Conversation) error {
	if conv == nil {
		return nil
	}
	fmt.Fprintf(os.Stdout, "[conversation] id=%s title=%q\n", conv.ID, conv.Title)
	return nil
}

// OnGap prints a sequence gap notification to stdout.
func (h *cliUpdateHandler) OnGap(_ context.Context, seq uint32) error {
	fmt.Fprintf(os.Stdout, "[gap] seq=%d\n", seq)
	return nil
}

// OnTyping prints a typing indicator event to stdout (D-050 ephemeral push).
// D-065: agent typing shows as "thinking", human typing as "typing".
func (h *cliUpdateHandler) OnTyping(_ context.Context, userID, conversationID string, isTyping bool) error {
	action := "started typing"
	label := "typing"
	if !isTyping {
		action = "stopped typing"
	}
	if client.IsAgentUser(userID) {
		label = "thinking"
		if !isTyping {
			action = "stopped thinking"
		}
	}
	fmt.Fprintf(os.Stdout, "[%s] user=%s conv=%s %s\n", label, userID, conversationID, action)
	return nil
}

// OnStreaming prints a streaming text event to stdout (D-051 ephemeral push).
func (h *cliUpdateHandler) OnStreaming(_ context.Context, userID, conversationID, streamID, text string, isDone bool) error {
	status := "streaming"
	if isDone {
		status = "done"
	}
	prefix := "streaming"
	if client.IsAgentUser(userID) {
		prefix = "agent"
	}
	fmt.Fprintf(os.Stdout, "[%s] user=%s conv=%s stream=%s status=%s text=%q\n",
		prefix, userID, conversationID, streamID, status, text)
	return nil
}

// OnAgentQuestion prints an agent HITL question event to stdout (D-087).
func (h *cliUpdateHandler) OnAgentQuestion(_ context.Context, userID, conversationID, question, checkpointID, interruptID string) error {
	fmt.Fprintf(os.Stdout, "[agent_question] agent=%s conv=%s checkpoint_id=%s interrupt_id=%s question=%q\n",
		userID, conversationID, checkpointID, interruptID, question)
	return nil
}

// OnAgentCheckpointCreated prints a checkpoint created event to stdout (D-087).
func (h *cliUpdateHandler) OnAgentCheckpointCreated(_ context.Context, userID, conversationID, checkpointID string) error {
	fmt.Fprintf(os.Stdout, "[agent_checkpoint] agent=%s conv=%s checkpoint_id=%s\n",
		userID, conversationID, checkpointID)
	return nil
}

// OnAgentStatus prints an agent status change event to stdout (D-087).
func (h *cliUpdateHandler) OnAgentStatus(_ context.Context, userID, conversationID, status string) error {
	fmt.Fprintf(os.Stdout, "[agent_status] agent=%s conv=%s status=%s\n",
		userID, conversationID, status)
	return nil
}

// OnAgentTimeout prints an agent timeout event to stdout (D-087).
func (h *cliUpdateHandler) OnAgentTimeout(_ context.Context, userID, conversationID, reason string) error {
	fmt.Fprintf(os.Stdout, "[agent_timeout] agent=%s conv=%s reason=%q\n",
		userID, conversationID, reason)
	return nil
}

// ---------------------------------------------------------------------------
// cliLogger — implements client.Logger
// ---------------------------------------------------------------------------

// cliLogger is a Logger implementation that writes structured log lines to
// stderr. Debug output is suppressed unless XYNCRA_DEBUG is set to "1" or
// "true".
//
// NOTE: File-based logging (writing to CLIContext.LogDir) is reserved for
// Phase 2. The current Phase 1 implementation writes only to stderr.
type cliLogger struct {
	debug bool
}

// newCLILogger creates a cliLogger. It reads XYNCRA_DEBUG at construction time.
func newCLILogger() *cliLogger {
	v := os.Getenv("XYNCRA_DEBUG")
	return &cliLogger{
		debug: v == "1" || v == "true",
	}
}

// logTimestamp returns the current time formatted for log output.
func logTimestamp() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

// formatLogArgs converts a variadic key-value slice into a " key=value ..." string.
func formatLogArgs(args []any) string {
	if len(args) == 0 {
		return ""
	}
	var b strings.Builder
	for i := 0; i < len(args); i += 2 {
		fmt.Fprintf(&b, " %v", args[i])
		if i+1 < len(args) {
			fmt.Fprintf(&b, "=%v", args[i+1])
		} else {
			b.WriteString("=MISSING")
		}
	}
	return b.String()
}

// Info logs an informational message to stderr.
func (l *cliLogger) Info(msg string, args ...any) {
	fmt.Fprintf(os.Stderr, "[%s] [INFO] %s%s\n", logTimestamp(), msg, formatLogArgs(args))
}

// Error logs an error message to stderr.
func (l *cliLogger) Error(msg string, args ...any) {
	fmt.Fprintf(os.Stderr, "[%s] [ERROR] %s%s\n", logTimestamp(), msg, formatLogArgs(args))
}

// Debug logs a debug message to stderr, only when XYNCRA_DEBUG is enabled.
func (l *cliLogger) Debug(msg string, args ...any) {
	if !l.debug {
		return
	}
	fmt.Fprintf(os.Stderr, "[%s] [DEBUG] %s%s\n", logTimestamp(), msg, formatLogArgs(args))
}

// ---------------------------------------------------------------------------
// listen subcommand
// ---------------------------------------------------------------------------

// newListenCommand creates the "listen" subcommand for the CLI.
func newListenCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "listen",
		Short: "Start listening for message updates",
		RunE:  runListen,
	}
	cmd.Flags().String("device-name", "xyncra-cli", "Human-readable device name for function registration")
	cmd.Flags().String("device-type", "cli", "Device type (e.g. cli, browser) for function registration")
	return cmd
}

// runListen is the entry point for the "listen" subcommand. It acquires the
// process lock, initialises the database, IPC server and Xyncra client, then
// blocks until a termination signal is received.
func runListen(cmd *cobra.Command, _ []string) error {
	cliCtx, err := NewCLIContext(cmd)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	// Acquire exclusive process lock (D-020, D-031).
	unlock, err := acquireLock(cliCtx.LockPath(), &LockInfo{
		PID:       os.Getpid(),
		StartedAt: time.Now(),
		DeviceID:  cliCtx.DeviceID,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "listen: %v\n", err)
		os.Exit(2)
	}
	defer func() { _ = unlock() }()

	// Open the client database.
	db, err := store.New(cliCtx.DBPath)
	if err != nil {
		return fmt.Errorf("listen: open db: %w", err)
	}
	defer db.Close()

	// Create the IPC server for the Unix socket (D-030).
	ipcServer := NewIPCServer(cliCtx.SocketPath())

	// Create the update handler and logger.
	handler := newCLIUpdateHandler()
	logger := newCLILogger()

	// Build the server URL with user_id query parameter (D-005).
	serverURL := cliCtx.ServerURLWithUser()

	// Read device metadata flags for function registration (D-115).
	deviceName, _ := cmd.Flags().GetString("device-name")
	deviceType, _ := cmd.Flags().GetString("device-type")

	// Build client options.
	clientOpts := []client.ClientOption{
		client.WithServerURL(serverURL),
		client.WithUserID(cliCtx.UserID),
		client.WithDeviceID(cliCtx.DeviceID),
		client.WithDB(db),
		client.WithUpdateHandler(handler),
		client.WithLogger(logger),
		client.WithDeviceName(deviceName),
		client.WithDeviceType(deviceType),
		client.WithFunctions(builtinFunctionInfos()),
	}

	// Testing hooks: allow overriding reconnect delays via env vars (D-048).
	// These are ONLY effective when set in test environments; production code
	// does not read XYNCRA_TEST_* variables.
	if v := os.Getenv("XYNCRA_TEST_RECONNECT_BASE_DELAY"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			clientOpts = append(clientOpts, client.WithReconnectBaseDelay(d))
		}
	}
	if v := os.Getenv("XYNCRA_TEST_RECONNECT_MAX_DELAY"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			clientOpts = append(clientOpts, client.WithReconnectMaxDelay(d))
		}
	}

	// Create the XyncraClient.
	xc, err := client.New(clientOpts...)
	if err != nil {
		return fmt.Errorf("listen: create client: %w", err)
	}
	defer xc.Stop()

	// Register built-in function handlers (D-115).
	registerBuiltinHandlers(xc)

	// Register IPC method handlers.
	registerIPCHandlers(ipcServer, xc, db, cliCtx.UserID)

	// Start the IPC server in the background.
	ctx := context.Background()
	if err := ipcServer.Start(ctx); err != nil {
		return fmt.Errorf("listen: start ipc server: %w", err)
	}
	defer func() {
		_ = ipcServer.Stop()
		_ = os.Remove(cliCtx.SocketPath())
	}()

	// Print startup banner to stderr.
	fmt.Fprintf(os.Stderr, "[xyncra] Starting listener daemon...\n")
	fmt.Fprintf(os.Stderr, "[xyncra] Device: %s\n", cliCtx.DeviceID)
	fmt.Fprintf(os.Stderr, "[xyncra] Connecting to %s ...\n", cliCtx.ServerURLWithUser())
	fmt.Fprintf(os.Stderr, "[xyncra] IPC server listening at %s\n", cliCtx.SocketPath())
	fmt.Fprintf(os.Stderr, "[xyncra] Listening for updates... (Ctrl+C to stop)\n")

	// Set up signal handling for graceful shutdown.
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Start automatic log cleanup goroutine (D-040).
	go startLogCleanup(ctx, db, defaultCleanupInterval, defaultLogRetention, logger)

	// Start the client (blocks until context is cancelled).
	return xc.Start(ctx)
}

// ---------------------------------------------------------------------------
// IPC handler registration
// ---------------------------------------------------------------------------

// registerIPCHandlers binds JSON-RPC method handlers on the IPC server that
// forward requests to the corresponding XyncraClient methods. The db parameter
// is used by handlers that need to update the local database (e.g.
// create_conversation writes the new conversation for D-035 local reads).
// The userID parameter is required by mark_as_read to update the correct
// read-cursor column in the local database (D-012, D-047).
func registerIPCHandlers(s *IPCServer, xc *client.XyncraClient, db *store.ClientDB, userID string) {
	s.Register("send_message", func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
		var params struct {
			ConversationID  string `json:"conversation_id"`
			Content         string `json:"content"`
			ReplyTo         uint32 `json:"reply_to"`
			ClientMessageID string `json:"client_message_id"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewIPCErrorResponse(req.ID, -32602, fmt.Sprintf("invalid params: %v", err)), nil
		}

		// If client_message_id is empty, XyncraClient auto-generates a UUID v4 (D-006).
		result, err := xc.SendMessage(ctx, params.ConversationID, params.Content, params.ClientMessageID, params.ReplyTo)
		if err != nil {
			if ce, ok := errors.AsType[*client.ClientError](err); ok {
				return NewIPCErrorResponse(req.ID, int(ce.Code), ce.Message), nil
			}
			return NewIPCErrorResponse(req.ID, -300, err.Error()), nil
		}
		// Persist sent message to local DB (D-035).
		if result.Message != nil && db != nil {
			if err := db.Messages.Create(ctx, result.Message); err != nil {
				if !errors.Is(err, store.ErrDuplicateKey) {
					fmt.Fprintf(os.Stderr, "[xyncra] warning: failed to persist sent message locally: %v\n", err)
				}
			} else {
				// Update conversation last-message pointer.
				if err := db.Conversations.UpdateLastMessage(ctx, result.Message.ConversationID, result.Message.CreatedAt, result.Message.MessageID); err != nil && !errors.Is(err, store.ErrNotFound) {
					fmt.Fprintf(os.Stderr, "[xyncra] warning: failed to update conversation last message: %v\n", err)
				}
			}
		}

		return NewIPCResponse(req.ID, result)
	})

	// sync_updates triggers a FullSync on the daemon (D-036).
	s.Register("sync_updates", func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
		if err := xc.FullSync(ctx); err != nil {
			return NewIPCErrorResponse(req.ID, -300, err.Error()), nil
		}
		return NewIPCResponse(req.ID, map[string]string{"status": "ok"})
	})

	// create_conversation creates a new 1-on-1 conversation and persists it
	// to the local database so that list-conversations (D-035) can show it
	// immediately without waiting for the next sync cycle.
	s.Register("create_conversation", func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
		var params struct {
			UserID2 string `json:"user_id2"`
			Title   string `json:"title"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewIPCErrorResponse(req.ID, -32602, fmt.Sprintf("invalid params: %v", err)), nil
		}
		result, err := xc.CreateConversation(ctx, params.UserID2, params.Title)
		if err != nil {
			if ce, ok := errors.AsType[*client.ClientError](err); ok {
				return NewIPCErrorResponse(req.ID, int(ce.Code), ce.Message), nil
			}
			return NewIPCErrorResponse(req.ID, -300, err.Error()), nil
		}

		// Persist the conversation to the local DB (D-035).
		if result.Conversation != nil && db != nil {
			if err := db.Conversations.Upsert(ctx, result.Conversation); err != nil {
				// Log but do not fail the RPC — the conversation was
				// created on the server successfully.
				fmt.Fprintf(os.Stderr, "[xyncra] warning: failed to persist created conversation locally: %v\n", err)
			}
		}

		return NewIPCResponse(req.ID, result)
	})

	// delete_conversation soft-deletes a conversation and returns the count of
	// cascade-deleted messages (D-013).
	s.Register("delete_conversation", func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
		var params struct {
			ConversationID string `json:"conversation_id"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewIPCErrorResponse(req.ID, -32602, fmt.Sprintf("invalid params: %v", err)), nil
		}
		result, err := xc.DeleteConversation(ctx, params.ConversationID)
		if err != nil {
			if ce, ok := errors.AsType[*client.ClientError](err); ok {
				return NewIPCErrorResponse(req.ID, int(ce.Code), ce.Message), nil
			}
			return NewIPCErrorResponse(req.ID, -300, err.Error()), nil
		}

		// Cascade soft-delete conversation in local DB (D-035, D-013).
		if db != nil {
			if err := db.Conversations.Delete(ctx, params.ConversationID); err != nil && !errors.Is(err, store.ErrNotFound) {
				fmt.Fprintf(os.Stderr, "[xyncra] warning: failed to delete conversation locally: %v\n", err)
			}
		}

		return NewIPCResponse(req.ID, result)
	})

	// restore_conversation restores a previously soft-deleted conversation and
	// returns the count of cascade-restored messages.
	s.Register("restore_conversation", func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
		var params struct {
			ConversationID string `json:"conversation_id"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewIPCErrorResponse(req.ID, -32602, fmt.Sprintf("invalid params: %v", err)), nil
		}
		result, err := xc.RestoreConversation(ctx, params.ConversationID)
		if err != nil {
			if ce, ok := errors.AsType[*client.ClientError](err); ok {
				return NewIPCErrorResponse(req.ID, int(ce.Code), ce.Message), nil
			}
			return NewIPCErrorResponse(req.ID, -300, err.Error()), nil
		}

		// Cascade restore conversation in local DB (D-035, D-015).
		if db != nil {
			if err := db.Conversations.Restore(ctx, params.ConversationID); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					// Local record doesn't exist (e.g. initiator's DB). Fetch
					// from server and upsert so the local DB is consistent.
					fetchResult, fetchErr := xc.GetConversation(ctx, params.ConversationID)
					if fetchErr != nil {
						fmt.Fprintf(os.Stderr, "[xyncra] warning: failed to fetch conversation after local restore miss: %v\n", fetchErr)
					} else if fetchResult != nil && fetchResult.Conversation != nil {
						if upsertErr := db.Conversations.Upsert(ctx, fetchResult.Conversation); upsertErr != nil {
							fmt.Fprintf(os.Stderr, "[xyncra] warning: failed to upsert fetched conversation locally: %v\n", upsertErr)
						}
					}
				} else {
					fmt.Fprintf(os.Stderr, "[xyncra] warning: failed to restore conversation locally: %v\n", err)
				}
			}
		}

		return NewIPCResponse(req.ID, result)
	})

	// delete_message soft-deletes a message (void RPC).
	s.Register("delete_message", func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
		var params struct {
			MessageID string `json:"message_id"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewIPCErrorResponse(req.ID, -32602, fmt.Sprintf("invalid params: %v", err)), nil
		}
		if err := xc.DeleteMessage(ctx, params.MessageID); err != nil {
			if ce, ok := errors.AsType[*client.ClientError](err); ok {
				return NewIPCErrorResponse(req.ID, int(ce.Code), ce.Message), nil
			}
			return NewIPCErrorResponse(req.ID, -300, err.Error()), nil
		}

		// Soft-delete message in local DB (D-035).
		if db != nil {
			if err := db.Messages.Delete(ctx, params.MessageID); err != nil && !errors.Is(err, store.ErrNotFound) {
				fmt.Fprintf(os.Stderr, "[xyncra] warning: failed to delete message locally: %v\n", err)
			}
		}

		return NewIPCResponse(req.ID, nil)
	})

	// mark_as_read advances the read cursor for the current user.
	// The server returns the actual cursor value (MAX semantics, D-012), which
	// we forward to the CLI so it can display the server-confirmed position.
	s.Register("mark_as_read", func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
		var params struct {
			ConversationID string `json:"conversation_id"`
			MessageID      uint32 `json:"message_id"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewIPCErrorResponse(req.ID, -32602, fmt.Sprintf("invalid params: %v", err)), nil
		}
		callParams := map[string]any{
			"conversation_id": params.ConversationID,
			"message_id":      params.MessageID,
		}
		data, err := xc.Call(ctx, "mark_as_read", callParams)
		if err != nil {
			if ce, ok := errors.AsType[*client.ClientError](err); ok {
				return NewIPCErrorResponse(req.ID, int(ce.Code), ce.Message), nil
			}
			return NewIPCErrorResponse(req.ID, -300, err.Error()), nil
		}
		// Parse the server's actual last_read_message_id from the response.
		var result struct {
			LastReadMessageID uint32 `json:"last_read_message_id"`
		}
		if err := json.Unmarshal(data, &result); err != nil {
			return NewIPCErrorResponse(req.ID, -300, fmt.Sprintf("unmarshal mark_as_read result: %v", err)), nil
		}

		// Update local read cursor using SERVER-RETURNED value (D-012, D-047).
		if db != nil {
			if err := db.Conversations.UpdateLastRead(ctx, params.ConversationID, userID, result.LastReadMessageID); err != nil {
				fmt.Fprintf(os.Stderr, "[xyncra] warning: failed to update local read cursor: %v\n", err)
			}
		}

		return NewIPCResponse(req.ID, result)
	})

	// set_typing forwards typing indicators to the server (fire-and-forget, D-050).
	s.Register("set_typing", func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
		var params struct {
			ConversationID string `json:"conversation_id"`
			IsTyping       bool   `json:"is_typing"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewIPCErrorResponse(req.ID, -32602, fmt.Sprintf("invalid params: %v", err)), nil
		}
		callParams := map[string]any{
			"conversation_id": params.ConversationID,
			"is_typing":       params.IsTyping,
		}
		data, err := xc.Call(ctx, "set_typing", callParams)
		if err != nil {
			if ce, ok := errors.AsType[*client.ClientError](err); ok {
				return NewIPCErrorResponse(req.ID, int(ce.Code), ce.Message), nil
			}
			return NewIPCErrorResponse(req.ID, -300, err.Error()), nil
		}
		// Forward the server response as-is.
		return NewIPCResponse(req.ID, json.RawMessage(data))
	})

	// stream_text forwards streaming text to the server (fire-and-forget, D-051).
	s.Register("stream_text", func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
		var params struct {
			ConversationID string `json:"conversation_id"`
			StreamID       string `json:"stream_id"`
			Text           string `json:"text"`
			IsDone         bool   `json:"is_done"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewIPCErrorResponse(req.ID, -32602, fmt.Sprintf("invalid params: %v", err)), nil
		}
		callParams := map[string]any{
			"conversation_id": params.ConversationID,
			"stream_id":       params.StreamID,
			"text":            params.Text,
			"is_done":         params.IsDone,
		}
		data, err := xc.Call(ctx, "stream_text", callParams)
		if err != nil {
			if ce, ok := errors.AsType[*client.ClientError](err); ok {
				return NewIPCErrorResponse(req.ID, int(ce.Code), ce.Message), nil
			}
			return NewIPCErrorResponse(req.ID, -300, err.Error()), nil
		}
		// Forward the server response as-is.
		return NewIPCResponse(req.ID, json.RawMessage(data))
	})

	// agent_resume forwards the resume request to the server via WebSocket (D-085, D-114).
	s.Register("agent_resume", func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
		var params struct {
			ConversationID string `json:"conversation_id"`
			CheckpointID   string `json:"checkpoint_id"`
			InterruptID    string `json:"interrupt_id"`
			Answer         string `json:"answer"`
			AgentID        string `json:"agent_id"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewIPCErrorResponse(req.ID, -32602, fmt.Sprintf("invalid params: %v", err)), nil
		}
		data, err := xc.Call(ctx, "agent_resume", params)
		if err != nil {
			if ce, ok := errors.AsType[*client.ClientError](err); ok {
				return NewIPCErrorResponse(req.ID, int(ce.Code), ce.Message), nil
			}
			return NewIPCErrorResponse(req.ID, -300, err.Error()), nil
		}
		return NewIPCResponse(req.ID, json.RawMessage(data))
	})
}

// ---------------------------------------------------------------------------
// Automatic log cleanup (D-040)
// ---------------------------------------------------------------------------

// startLogCleanup runs a periodic cleanup of expired RPC logs and notification
// logs from the client database. It ticks every interval and hard-deletes
// records older than retention. The goroutine exits when ctx is cancelled.
//
// Cleanup failures are logged but do not terminate the daemon.
func startLogCleanup(ctx context.Context, db *store.ClientDB, interval, retention time.Duration, logger *cliLogger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runCleanup(db, retention, logger)
		}
	}
}

// runCleanup performs a single cleanup pass, deleting RPC logs and notification
// logs older than retention. Both deletes run inside a single transaction so
// that either both succeed or both are rolled back (L-1).
func runCleanup(db *store.ClientDB, retention time.Duration, logger *cliLogger) {
	ctx := context.Background()
	before := time.Now().Add(-retention)

	err := db.Transaction(ctx, func(tx *gorm.DB) error {
		rpcResult := tx.Where("created_at < ?", before).Delete(&model.RPCLog{})
		if rpcResult.Error != nil {
			return fmt.Errorf("cleanup rpc logs: %w", rpcResult.Error)
		}

		notifResult := tx.Where("created_at < ?", before).Delete(&model.NotificationLog{})
		if notifResult.Error != nil {
			return fmt.Errorf("cleanup notification logs: %w", notifResult.Error)
		}

		return nil
	})

	if err != nil {
		logger.Error("auto-cleanup", "error", err)
	}
}
