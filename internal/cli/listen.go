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

	"github.com/PineappleBond/xyncra-server/pkg/client"
	"github.com/PineappleBond/xyncra-server/pkg/store"
	"github.com/PineappleBond/xyncra-server/pkg/store/model"
)

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
	return &cobra.Command{
		Use:   "listen",
		Short: "Start listening for message updates",
		RunE:  runListen,
	}
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

	// Create the XyncraClient.
	xc, err := client.New(
		client.WithServerURL(serverURL),
		client.WithUserID(cliCtx.UserID),
		client.WithDB(db),
		client.WithUpdateHandler(handler),
		client.WithLogger(logger),
	)
	if err != nil {
		return fmt.Errorf("listen: create client: %w", err)
	}
	defer xc.Stop()

	// Register IPC method handlers.
	registerIPCHandlers(ipcServer, xc)

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

	// Start the client (blocks until context is cancelled).
	return xc.Start(ctx)
}

// ---------------------------------------------------------------------------
// IPC handler registration
// ---------------------------------------------------------------------------

// registerIPCHandlers binds JSON-RPC method handlers on the IPC server that
// forward requests to the corresponding XyncraClient methods.
func registerIPCHandlers(s *IPCServer, xc *client.XyncraClient) {
	s.Register("send_message", func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
		var params struct {
			ConversationID string `json:"conversation_id"`
			Content        string `json:"content"`
			ReplyTo        uint32 `json:"reply_to"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewIPCErrorResponse(req.ID, -32602, fmt.Sprintf("invalid params: %v", err)), nil
		}

		// clientMsgID is left empty — XyncraClient auto-generates a UUID v4 (D-006).
		result, err := xc.SendMessage(ctx, params.ConversationID, params.Content, "", params.ReplyTo)
		if err != nil {
			if ce, ok := errors.AsType[*client.ClientError](err); ok {
				return NewIPCErrorResponse(req.ID, int(ce.Code), ce.Message), nil
			}
			return NewIPCErrorResponse(req.ID, -300, err.Error()), nil
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

	// create_conversation creates a new 1-on-1 conversation.
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
		return NewIPCResponse(req.ID, result)
	})

	// delete_conversation soft-deletes a conversation (void RPC).
	s.Register("delete_conversation", func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
		var params struct {
			ConversationID string `json:"conversation_id"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewIPCErrorResponse(req.ID, -32602, fmt.Sprintf("invalid params: %v", err)), nil
		}
		if err := xc.DeleteConversation(ctx, params.ConversationID); err != nil {
			if ce, ok := errors.AsType[*client.ClientError](err); ok {
				return NewIPCErrorResponse(req.ID, int(ce.Code), ce.Message), nil
			}
			return NewIPCErrorResponse(req.ID, -300, err.Error()), nil
		}
		return NewIPCResponse(req.ID, nil)
	})

	// restore_conversation restores a previously soft-deleted conversation (void RPC).
	s.Register("restore_conversation", func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
		var params struct {
			ConversationID string `json:"conversation_id"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewIPCErrorResponse(req.ID, -32602, fmt.Sprintf("invalid params: %v", err)), nil
		}
		if err := xc.RestoreConversation(ctx, params.ConversationID); err != nil {
			if ce, ok := errors.AsType[*client.ClientError](err); ok {
				return NewIPCErrorResponse(req.ID, int(ce.Code), ce.Message), nil
			}
			return NewIPCErrorResponse(req.ID, -300, err.Error()), nil
		}
		return NewIPCResponse(req.ID, nil)
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
		return NewIPCResponse(req.ID, nil)
	})

	// mark_as_read advances the read cursor for the current user (void RPC).
	s.Register("mark_as_read", func(ctx context.Context, req *IPCRequest) (*IPCResponse, error) {
		var params struct {
			ConversationID string `json:"conversation_id"`
			MessageID      uint32 `json:"message_id"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewIPCErrorResponse(req.ID, -32602, fmt.Sprintf("invalid params: %v", err)), nil
		}
		if err := xc.MarkAsRead(ctx, params.ConversationID, params.MessageID); err != nil {
			if ce, ok := errors.AsType[*client.ClientError](err); ok {
				return NewIPCErrorResponse(req.ID, int(ce.Code), ce.Message), nil
			}
			return NewIPCErrorResponse(req.ID, -300, err.Error()), nil
		}
		return NewIPCResponse(req.ID, nil)
	})
}
