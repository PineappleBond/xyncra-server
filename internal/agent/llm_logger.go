package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Tool-calling message ID tracker (execution-time bridge)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Context keys for function call broadcasting
// ---------------------------------------------------------------------------

// ctxKey is an unexported type for context keys to avoid collisions.
type ctxKey int

const (
	ctxKeyBroadcastHelper ctxKey = iota
	ctxKeyBroadcastHumanUserID
	ctxKeyBroadcastAgentUserID
	ctxKeyBroadcastConversationID
	ctxKeyStore
	ctxKeyToolCallingMessage // Stores the tool_calling message ID for executor to use
)

// WithBroadcastInfo returns a context enriched with broadcast metadata so
// that the LoggingMiddleware can emit function_call updates to clients.
func WithBroadcastInfo(ctx context.Context, bh *BroadcastHelper, humanUserID, agentUserID, conversationID string) context.Context {
	ctx = context.WithValue(ctx, ctxKeyBroadcastHelper, bh)
	ctx = context.WithValue(ctx, ctxKeyBroadcastHumanUserID, humanUserID)
	ctx = context.WithValue(ctx, ctxKeyBroadcastAgentUserID, agentUserID)
	ctx = context.WithValue(ctx, ctxKeyBroadcastConversationID, conversationID)
	return ctx
}

func WithMessage(ctx context.Context, message *model.Message) context.Context {
	ctx = context.WithValue(ctx, ctxKeyToolCallingMessage, message)
	return ctx
}

// WithStore returns a context enriched with a StoreAPI so that the
// LoggingMiddleware can persist tool_calling messages. When the store is nil,
// the middleware falls back to ephemeral-only broadcasting (D-063 nil-safe).
func WithStore(ctx context.Context, s store.StoreAPI) context.Context {
	return context.WithValue(ctx, ctxKeyStore, s)
}

// storeFromContext extracts the StoreAPI from ctx. Returns nil when not
// injected (caller must nil-check before use).
func storeFromContext(ctx context.Context) store.StoreAPI {
	s, _ := ctx.Value(ctxKeyStore).(store.StoreAPI)
	return s
}

// ToolCallingPayload is the JSON structure persisted as a tool_calling
// message's Content field. It captures the full lifecycle of a single tool
// invocation for client-side rendering and post-hoc analysis.
type ToolCallingPayload struct {
	Name       string `json:"name"`
	Args       string `json:"args"`
	Status     string `json:"status"`                // "executing" | "completed" | "failed"
	Result     string `json:"result,omitempty"`      // non-empty on completed
	Error      string `json:"error,omitempty"`       // non-empty on failed
	DurationMs int64  `json:"duration_ms,omitempty"` // non-zero on completed/failed
}

// ---------------------------------------------------------------------------
// LLM Logger — structured JSONL logger for LLM observability
// ---------------------------------------------------------------------------

// LLMLogger writes structured JSON log records to an io.Writer in JSONL
// format (one JSON object per line). It is safe for concurrent use; all
// writes are serialized through an internal mutex.
type LLMLogger struct {
	mu     sync.Mutex
	w      io.Writer
	enc    *json.Encoder
	indent bool
}

// NewLLMLogger creates an LLMLogger that writes to w. When indent is true
// each record is pretty-printed; production deployments should pass false
// to keep one record per line.
func NewLLMLogger(w io.Writer, indent bool) *LLMLogger {
	enc := json.NewEncoder(w)
	if indent {
		enc.SetIndent("", "  ")
	}
	return &LLMLogger{w: w, enc: enc, indent: indent}
}

// write serializes a LogRecord as a single JSON line.
func (l *LLMLogger) write(rec LogRecord) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.enc.Encode(rec)
}

// ---------------------------------------------------------------------------
// Snapshot types (JSON-serializable representations of Eino types)
// ---------------------------------------------------------------------------

// LogRecord is a single JSONL record describing one phase of LLM interaction.
type LogRecord struct {
	Timestamp  time.Time         `json:"timestamp"`
	AgentID    string            `json:"agent_id"`
	Model      string            `json:"model"`
	Iteration  int               `json:"iteration"`
	Phase      string            `json:"phase"` // "request" | "response" | "tool_call" | "tool_result" | "agent_start" | "agent_end" | "error"
	Messages   []MessageSnapshot `json:"messages,omitempty"`
	Tools      []ToolSnapshot    `json:"tools,omitempty"`
	Output     *MessageSnapshot  `json:"output,omitempty"`
	TokenUsage *TokenSnapshot    `json:"token_usage,omitempty"`
	DurationMs int64             `json:"duration_ms,omitempty"`
	ToolName   string            `json:"tool_name,omitempty"`
	ToolArgs   string            `json:"tool_args,omitempty"`
	ToolResult string            `json:"tool_result,omitempty"`
	Error      string            `json:"error,omitempty"`
}

// MessageSnapshot is a trimmed representation of a schema.Message.
type MessageSnapshot struct {
	Role      string             `json:"role"`
	Content   string             `json:"content"`
	ToolCalls []ToolCallSnapshot `json:"tool_calls,omitempty"`
}

// ToolCallSnapshot captures one tool call from an assistant message.
type ToolCallSnapshot struct {
	Name string `json:"name"`
	Args string `json:"args"`
}

// ToolSnapshot is a trimmed representation of a schema.ToolInfo.
type ToolSnapshot struct {
	Name string `json:"name"`
	Desc string `json:"desc"`
}

// TokenSnapshot captures token usage from a model response.
type TokenSnapshot struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// convertMessage converts a schema.Message to a MessageSnapshot, truncating
// long content to keep log records manageable.
func convertMessage(msg *schema.Message) MessageSnapshot {
	if msg == nil {
		return MessageSnapshot{}
	}
	snap := MessageSnapshot{
		Role:    string(msg.Role),
		Content: truncate(msg.Content, 4096),
	}
	for _, tc := range msg.ToolCalls {
		snap.ToolCalls = append(snap.ToolCalls, ToolCallSnapshot{
			Name: tc.Function.Name,
			Args: truncate(tc.Function.Arguments, 2048),
		})
	}
	return snap
}

// convertToolInfo converts a schema.ToolInfo to a ToolSnapshot.
func convertToolInfo(ti *schema.ToolInfo) ToolSnapshot {
	if ti == nil {
		return ToolSnapshot{}
	}
	return ToolSnapshot{
		Name: ti.Name,
		Desc: ti.Desc,
	}
}

// truncate shortens s to at most maxLen characters. When truncation occurs
// the suffix "…[truncated]" is appended so the reader knows the value was
// cut.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen < 16 {
		return s[:maxLen]
	}
	return s[:maxLen-14] + "...[truncated]"
}

// ---------------------------------------------------------------------------
// LoggingMiddleware — Eino middleware that records LLM interactions
// ---------------------------------------------------------------------------

// LoggingMiddleware is an Eino ChatModelAgentMiddleware that logs every
// model request/response, tool call, and agent lifecycle event to an
// LLMLogger. It implements adk.TypedChatModelAgentMiddleware[*schema.Message]
// by embedding *adk.BaseChatModelAgentMiddleware and overriding only the
// hooks it needs.
type LoggingMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	logger    *LLMLogger
	agentID   string
	model     string
	iteration int32 // accessed atomically

	// modelCallStart records when the current model call began, used to
	// compute DurationMs in the "response" record.
	modelCallStart atomic.Value // stores time.Time
}

// NewLoggingMiddleware creates a LoggingMiddleware that writes records to
// logger for the given agentID and model name.
func NewLoggingMiddleware(logger *LLMLogger, agentID, model string) *LoggingMiddleware {
	return &LoggingMiddleware{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		logger:                       logger,
		agentID:                      agentID,
		model:                        model,
	}
}

// BeforeAgent logs the "agent_start" phase.
func (m *LoggingMiddleware) BeforeAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext) (context.Context, *adk.ChatModelAgentContext, error) {
	m.logger.write(LogRecord{
		Timestamp: time.Now(),
		AgentID:   m.agentID,
		Model:     m.model,
		Iteration: int(atomic.LoadInt32(&m.iteration)),
		Phase:     "agent_start",
	})
	return ctx, runCtx, nil
}

// AfterAgent logs the "agent_end" phase with a summary of the final state.
func (m *LoggingMiddleware) AfterAgent(ctx context.Context, state *adk.ChatModelAgentState) (context.Context, error) {
	var lastMsg *MessageSnapshot
	if n := len(state.Messages); n > 0 {
		snap := convertMessage(state.Messages[n-1])
		lastMsg = &snap
	}
	m.logger.write(LogRecord{
		Timestamp: time.Now(),
		AgentID:   m.agentID,
		Model:     m.model,
		Iteration: int(atomic.LoadInt32(&m.iteration)),
		Phase:     "agent_end",
		Output:    lastMsg,
	})
	return ctx, nil
}

// BeforeModelRewriteState logs the "request" phase before each model
// invocation. It captures the full message list and tool definitions that
// will be sent to the model.
func (m *LoggingMiddleware) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, mc *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	iter := int(atomic.AddInt32(&m.iteration, 1))

	msgs := make([]MessageSnapshot, 0, len(state.Messages))
	for _, msg := range state.Messages {
		msgs = append(msgs, convertMessage(msg))
	}
	tools := make([]ToolSnapshot, 0, len(state.ToolInfos))
	for _, ti := range state.ToolInfos {
		tools = append(tools, convertToolInfo(ti))
	}

	m.modelCallStart.Store(time.Now())

	m.logger.write(LogRecord{
		Timestamp: time.Now(),
		AgentID:   m.agentID,
		Model:     m.model,
		Iteration: iter,
		Phase:     "request",
		Messages:  msgs,
		Tools:     tools,
	})
	return ctx, state, nil
}

// AfterModelRewriteState logs the "response" phase after each model
// invocation. It captures the model's output (last message) and any token
// usage information.
func (m *LoggingMiddleware) AfterModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, mc *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	rec := LogRecord{
		Timestamp: time.Now(),
		AgentID:   m.agentID,
		Model:     m.model,
		Iteration: int(atomic.LoadInt32(&m.iteration)),
		Phase:     "response",
	}

	if n := len(state.Messages); n > 0 {
		last := state.Messages[n-1]
		snap := convertMessage(last)
		rec.Output = &snap

		// Extract token usage from the model response metadata.
		if last.ResponseMeta != nil && last.ResponseMeta.Usage != nil {
			u := last.ResponseMeta.Usage
			rec.TokenUsage = &TokenSnapshot{
				InputTokens:  u.PromptTokens,
				OutputTokens: u.CompletionTokens,
				TotalTokens:  u.TotalTokens,
			}
		}
	}

	// Compute duration from the stored start time.
	if startVal := m.modelCallStart.Load(); startVal != nil {
		if start, ok := startVal.(time.Time); ok {
			rec.DurationMs = time.Since(start).Milliseconds()
		}
	}

	m.logger.write(rec)
	return ctx, state, nil
}

// WrapInvokableToolCall wraps tool execution to log "tool_call" before
// invocation and "tool_result" after completion. It also persists tool_calling
// messages to the database when a StoreAPI is available in the context, and
// broadcasts updates to clients via BroadcastHelper. When the store is not
// available (nil-safe D-063), it falls back to ephemeral-only broadcasting.
func (m *LoggingMiddleware) WrapInvokableToolCall(ctx context.Context, endpoint adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		iter := int(atomic.LoadInt32(&m.iteration))

		m.logger.write(LogRecord{
			Timestamp: time.Now(),
			AgentID:   m.agentID,
			Model:     m.model,
			Iteration: iter,
			Phase:     "tool_call",
			ToolName:  tCtx.Name,
			ToolArgs:  truncate(argumentsInJSON, 2048),
		})

		// Attempt to persist tool_calling message (best-effort, fire-and-forget).
		var toolMsgID uint32
		var toolMsgUUID string // UUID of persisted message (for ephemeral broadcast)
		s := storeFromContext(ctx)
		bh, _ := ctx.Value(ctxKeyBroadcastHelper).(*BroadcastHelper)
		humanUserID, _ := ctx.Value(ctxKeyBroadcastHumanUserID).(string)
		agentUserID, _ := ctx.Value(ctxKeyBroadcastAgentUserID).(string)
		conversationID, _ := ctx.Value(ctxKeyBroadcastConversationID).(string)
		message, _ := ctx.Value(ctxKeyToolCallingMessage).(*model.Message)
		messageID := uint32(0)
		if message != nil {
			messageID = message.MessageID
		}
		m.logger.write(LogRecord{
			Timestamp: time.Now(),
			AgentID:   m.agentID,
			Model:     m.model,
			Iteration: iter,
			Phase:     "wrap_invokable_entry",
			ToolName:  tCtx.Name,
			Error:     fmt.Sprintf("ENTER WrapInvokableToolCall: tool=%s, convID=%s, store=%v, bh=%v", tCtx.Name, conversationID, s != nil, bh != nil),
		})

		if s != nil && bh != nil && humanUserID != "" && conversationID != "" {
			// Build executing payload.
			payload := ToolCallingPayload{
				Name:   tCtx.Name,
				Args:   truncate(argumentsInJSON, 2048),
				Status: "executing",
			}
			payloadJSON, err := json.Marshal(payload)
			if err != nil {
				m.logger.write(LogRecord{
					Timestamp: time.Now(),
					AgentID:   m.agentID,
					Model:     m.model,
					Iteration: iter,
					Phase:     "error",
					Error:     fmt.Sprintf("marshal tool_calling payload: %v", err),
				})
			} else {
				// Determine member IDs for UserUpdate fan-out.
				memberIDs := []string{humanUserID}
				if agentUserID != "" && agentUserID != humanUserID {
					memberIDs = append(memberIDs, agentUserID)
				}

				// Check if we're in a Resume path: look for a resolved RemoteCalling
				// with this method + conversationID. If found, update the existing
				// tool_calling message instead of creating a duplicate.
				// This handles the case where Run() created a message, interrupt happened,
				// and Resume() calls the same tool again.
				existingMsgID := messageID
				var existingMsgUUID string

				if existingMsgID > 0 {
					if message != nil {
						message.MessageID = 0
					}
					// Resume path: update the existing tool_calling message.
					tx := s.MessageStore().Begin()
					if tx != nil {
						if updateErr := s.MessageStore().UpdateMessageContentTx(ctx, tx, conversationID, existingMsgID, string(payloadJSON), "tool_calling", "executing"); updateErr != nil {
							tx.Rollback()
							m.logger.write(LogRecord{
								Timestamp: time.Now(),
								AgentID:   m.agentID,
								Model:     m.model,
								Iteration: iter,
								Phase:     "error",
								Error:     fmt.Sprintf("update existing tool_calling message: %v", updateErr),
							})
						} else {
							// Query full message for UserUpdate payload.
							var broadcastUpdates []model.UserUpdate
							if updatedMsg, getErr := s.MessageStore().GetByConversationAndMessageIDTx(ctx, tx, conversationID, existingMsgID); getErr == nil {
								// Use the actual message's UUID (not rc.ID) for ephemeral broadcast.
								existingMsgUUID = updatedMsg.ID
								payload, _ := json.Marshal(updatedMsg)
								now := time.Now()
								for _, memberID := range memberIDs {
									var latestSeq uint32
									if seqErr := tx.Model(&model.UserUpdate{}).Where("user_id = ?", memberID).Select("COALESCE(MAX(seq), 0)").Scan(&latestSeq).Error; seqErr == nil {
										broadcastUpdates = append(broadcastUpdates, model.UserUpdate{
											ID:        uuid.New().String(),
											UserID:    memberID,
											Seq:       latestSeq + 1,
											Type:      "message",
											Payload:   payload,
											CreatedAt: now,
										})
									}
								}
								if len(broadcastUpdates) > 0 {
									tx.CreateInBatches(broadcastUpdates, 100)
								}
							}
							tx.Commit()
							toolMsgID = existingMsgID
							toolMsgUUID = existingMsgUUID
							m.logger.write(LogRecord{
								Timestamp: time.Now(),
								AgentID:   m.agentID,
								Model:     m.model,
								Iteration: iter,
								Phase:     "resumed_existing_tool_calling_message",
								ToolName:  tCtx.Name,
								Error:     fmt.Sprintf("updated existing message: tool=%s, msgID=%d", tCtx.Name, toolMsgID),
							})
							if len(broadcastUpdates) > 0 {
								bh.BroadcastMessageUpdate(ctx, broadcastUpdates)
							}
						}
					}
				} else {
					// Normal path: create a new tool_calling message.
					msgUUID := uuid.New().String()
					msg := &model.Message{
						ID:              msgUUID,
						ClientMessageID: msgUUID,
						ConversationID:  conversationID,
						SenderID:        agentUserID,
						Content:         string(payloadJSON),
						Type:            "tool_calling",
						Status:          "executing",
						CreatedAt:       time.Now(),
					}
					result, err := s.SendMessage(ctx, msg, memberIDs)
					if err != nil {
						m.logger.write(LogRecord{
							Timestamp: time.Now(),
							AgentID:   m.agentID,
							Model:     m.model,
							Iteration: iter,
							Phase:     "error",
							Error:     fmt.Sprintf("persist tool_calling message: %v", err),
						})
					} else {
						toolMsgID = result.Message.MessageID
						toolMsgUUID = msgUUID
						ctx = WithMessage(ctx, result.Message)
						// DIAG: log the number of UserUpdates created and their details
						updateDetails := make([]string, 0, len(result.Updates))
						for _, u := range result.Updates {
							updateDetails = append(updateDetails, fmt.Sprintf("user=%s seq=%d", u.UserID, u.Seq))
						}
						m.logger.write(LogRecord{
							Timestamp: time.Now(),
							AgentID:   m.agentID,
							Model:     m.model,
							Iteration: iter,
							Phase:     "created_tool_calling_message",
							ToolName:  tCtx.Name,
							Error:     fmt.Sprintf("CREATED message: tool=%s, msgID=%d, msgUUID=%s, memberIDs=%v, userUpdates=%d, details=%v", tCtx.Name, toolMsgID, msgUUID, memberIDs, len(result.Updates), updateDetails),
						})
						bh.BroadcastMessageUpdate(ctx, result.Updates)
					}
				}
			}
		} else {
			// Fallback: ephemeral broadcast (legacy behavior).
			m.broadcastFunctionCall(ctx, tCtx.Name, truncate(argumentsInJSON, 2048), "", "", 0, false)
		}

		start := time.Now()
		result, err := endpoint(ctx, argumentsInJSON, opts...)
		dur := time.Since(start).Milliseconds()

		resultRec := LogRecord{
			Timestamp:  time.Now(),
			AgentID:    m.agentID,
			Model:      m.model,
			Iteration:  iter,
			Phase:      "tool_result",
			ToolName:   tCtx.Name,
			ToolResult: truncate(result, 4096),
			DurationMs: dur,
		}
		if err != nil {
			resultRec.Error = err.Error()
		}
		m.logger.write(resultRec)

		// Update persisted tool_calling message (best-effort).
		if toolMsgID > 0 && s != nil {
			errStr := ""
			if err != nil {
				errStr = err.Error()
			}

			// Build final payload.
			finalPayload := ToolCallingPayload{
				Name:       tCtx.Name,
				Args:       truncate(argumentsInJSON, 2048),
				DurationMs: dur,
			}
			if _, isInterrupt := compose.IsInterruptRerunError(err); isInterrupt {
				// Interrupt is not an error — the agent is paused waiting for
				// an external result (remote calling). Keep the message as
				// "executing" so the client knows it's still in progress.
				finalPayload.Status = "executing"
			} else if err != nil {
				finalPayload.Status = "failed"
				finalPayload.Error = truncate(errStr, 2048)
			} else {
				finalPayload.Status = "completed"
				finalPayload.Result = truncate(result, 4096)
			}

			// DIAG: log before attempting update
			m.logger.write(LogRecord{
				Timestamp: time.Now(),
				AgentID:   m.agentID,
				Model:     m.model,
				Iteration: iter,
				Phase:     "tool_calling_diag",
				ToolName:  tCtx.Name,
				Error:     fmt.Sprintf("pre-update: toolMsgID=%d status=%s convID=%s humanUser=%s agentUser=%s ctxErr=%v dur=%dms", toolMsgID, finalPayload.Status, conversationID, humanUserID, agentUserID, ctx.Err(), dur),
			})

			finalPayloadJSON, marshalErr := json.Marshal(finalPayload)
			if marshalErr != nil {
				m.logger.write(LogRecord{
					Timestamp: time.Now(),
					AgentID:   m.agentID,
					Model:     m.model,
					Iteration: iter,
					Phase:     "error",
					Error:     fmt.Sprintf("marshal final tool_calling payload: %v", marshalErr),
				})
			} else {
				// Update message in transaction with UserUpdate fan-out.
				m.updateToolCallingMessage(ctx, s, bh, conversationID, toolMsgID, humanUserID, agentUserID, string(finalPayloadJSON), finalPayload.Status)

				// Send an ephemeral message update (seq=0) for instant UI feedback.
				// The persisted update above goes through the client's sync pipeline
				// (IndexedDB + applyChain), which can be delayed by streaming updates.
				// The ephemeral update bypasses this pipeline and triggers an immediate
				// message:added event, causing ToolCallingMessage to re-render.
				if bh != nil && humanUserID != "" && toolMsgID > 0 && toolMsgUUID != "" {
					ephemeralPayload, _ := json.Marshal(map[string]interface{}{
						"id":              toolMsgUUID,
						"conversation_id": conversationID,
						"sender_id":       agentUserID,
						"content":         string(finalPayloadJSON),
						"type":            "tool_calling",
						"message_id":      toolMsgID,
						"status":          finalPayload.Status,
						"created_at":      time.Now().Format(time.RFC3339Nano),
					})
					if ephemeralPayload != nil {
						m.logger.write(LogRecord{
							Timestamp: time.Now(),
							AgentID:   m.agentID,
							Model:     m.model,
							Iteration: iter,
							Phase:     "tool_calling_ephemeral_broadcast",
							ToolName:  tCtx.Name,
							Error:     fmt.Sprintf("sending ephemeral message update: toolMsgUUID=%s toolMsgID=%d status=%s", toolMsgUUID, toolMsgID, finalPayload.Status),
						})
						bh.SendEphemeralMessageUpdate(humanUserID, ephemeralPayload)
					}
				}

				// Also send an ephemeral function_call broadcast for immediate
				// UI feedback. The persisted message update above goes through
				// IndexedDB transaction + applyChain serialization on the client,
				// which may be deferred during rapid streaming. The ephemeral
				// broadcast (seq=0) bypasses this pipeline and triggers an
				// instant UI update via the function:called event.
				if bh != nil && humanUserID != "" && conversationID != "" {
					errStr := ""
					if err != nil {
						errStr = err.Error()
					}
					bh.SendFunctionCall(ctx, humanUserID, agentUserID, conversationID,
						tCtx.Name, truncate(argumentsInJSON, 2048),
						truncate(result, 4096), errStr, dur, true)
				}
			}
		} else if bh != nil {
			// Fallback: ephemeral broadcast completion (legacy behavior).
			// Skip interrupt errors — they are not real failures, just the
			// agent pausing for an external result (remote calling).
			if _, isInterrupt := compose.IsInterruptRerunError(err); !isInterrupt {
				errStr := ""
				if err != nil {
					errStr = err.Error()
				}
				m.broadcastFunctionCall(ctx, tCtx.Name, truncate(argumentsInJSON, 2048), truncate(result, 4096), errStr, dur, true)
			}
		}

		return result, err
	}, nil
}

// updateToolCallingMessage updates a persisted tool_calling message within a
// transaction and creates UserUpdate records for sync. This is fire-and-forget;
// errors are logged but never propagated.
func (m *LoggingMiddleware) updateToolCallingMessage(ctx context.Context, s store.StoreAPI, bh *BroadcastHelper, conversationID string, messageID uint32, humanUserID, agentUserID, content, status string) {
	// DIAG: trace entry
	m.logger.write(LogRecord{
		Timestamp: time.Now(),
		AgentID:   m.agentID,
		Phase:     "tool_calling_update_diag",
		Error:     fmt.Sprintf("ENTER: convID=%s msgID=%d status=%s ctxErr=%v", conversationID, messageID, status, ctx.Err()),
	})

	// Use the store's transaction to update the message and create UserUpdates.
	tx := s.MessageStore().Begin()
	if tx == nil {
		m.logger.write(LogRecord{
			Timestamp: time.Now(),
			AgentID:   m.agentID,
			Phase:     "error",
			Error:     "begin transaction for tool_calling update",
		})
		return
	}

	// Update message content, type, and status.
	if err := s.MessageStore().UpdateMessageContentTx(ctx, tx, conversationID, messageID, content, "tool_calling", status); err != nil {
		tx.Rollback()
		m.logger.write(LogRecord{
			Timestamp: time.Now(),
			AgentID:   m.agentID,
			Phase:     "error",
			Error:     fmt.Sprintf("update tool_calling message: %v", err),
		})
		return
	}

	// DIAG: update succeeded
	m.logger.write(LogRecord{
		Timestamp: time.Now(),
		AgentID:   m.agentID,
		Phase:     "tool_calling_update_diag",
		Error:     fmt.Sprintf("STEP1 OK: UpdateMessageContentTx success, msgID=%d", messageID),
	})

	// Query the full message to get all fields (id, sender_id, created_at, etc.)
	// for the UserUpdate payload. The partial message from the update would have
	// empty fields that cause frontend validation errors (D-137).
	updatedMsg, err := s.MessageStore().GetByConversationAndMessageIDTx(ctx, tx, conversationID, messageID)
	if err != nil {
		tx.Rollback()
		m.logger.write(LogRecord{
			Timestamp: time.Now(),
			AgentID:   m.agentID,
			Phase:     "error",
			Error:     fmt.Sprintf("get updated message for payload: %v", err),
		})
		return
	}
	payload, err := json.Marshal(updatedMsg)
	if err != nil {
		tx.Rollback()
		m.logger.write(LogRecord{
			Timestamp: time.Now(),
			AgentID:   m.agentID,
			Phase:     "error",
			Error:     fmt.Sprintf("marshal updated message: %v", err),
		})
		return
	}

	// DIAG: query + marshal succeeded
	m.logger.write(LogRecord{
		Timestamp: time.Now(),
		AgentID:   m.agentID,
		Phase:     "tool_calling_update_diag",
		Error:     fmt.Sprintf("STEP2 OK: GetByConversationAndMessageIDTx success, payloadLen=%d", len(payload)),
	})

	// Allocate per-user seq values and build UserUpdate records.
	memberIDs := []string{humanUserID}
	if agentUserID != "" && agentUserID != humanUserID {
		memberIDs = append(memberIDs, agentUserID)
	}

	now := time.Now()
	updates := make([]model.UserUpdate, 0, len(memberIDs))
	for _, memberID := range memberIDs {
		var latestSeq uint32
		if err := tx.Model(&model.UserUpdate{}).
			Where("user_id = ?", memberID).
			Select("COALESCE(MAX(seq), 0)").
			Scan(&latestSeq).Error; err != nil {
			tx.Rollback()
			m.logger.write(LogRecord{
				Timestamp: time.Now(),
				AgentID:   m.agentID,
				Phase:     "error",
				Error:     fmt.Sprintf("get latest seq for user %s: %v", memberID, err),
			})
			return
		}

		update := model.UserUpdate{
			ID:        uuid.New().String(),
			UserID:    memberID,
			Seq:       latestSeq + 1,
			Type:      "message",
			Payload:   payload,
			CreatedAt: now,
		}
		updates = append(updates, update)
	}

	// DIAG: seq allocation done
	m.logger.write(LogRecord{
		Timestamp: time.Now(),
		AgentID:   m.agentID,
		Phase:     "tool_calling_update_diag",
		Error:     fmt.Sprintf("STEP3 OK: allocated %d UserUpdates, memberIDs=%v", len(updates), memberIDs),
	})

	// Batch insert UserUpdates.
	if len(updates) > 0 {
		if err := tx.CreateInBatches(updates, 100).Error; err != nil {
			tx.Rollback()
			m.logger.write(LogRecord{
				Timestamp: time.Now(),
				AgentID:   m.agentID,
				Phase:     "error",
				Error:     fmt.Sprintf("insert user updates: %v", err),
			})
			return
		}
	}

	// Commit transaction.
	if err := tx.Commit().Error; err != nil {
		m.logger.write(LogRecord{
			Timestamp: time.Now(),
			AgentID:   m.agentID,
			Phase:     "error",
			Error:     fmt.Sprintf("commit tool_calling update transaction: %v", err),
		})
		return
	}

	// DIAG: commit done, about to broadcast
	updateDetails := make([]string, 0, len(updates))
	for _, u := range updates {
		updateDetails = append(updateDetails, fmt.Sprintf("user=%s seq=%d", u.UserID, u.Seq))
	}
	m.logger.write(LogRecord{
		Timestamp: time.Now(),
		AgentID:   m.agentID,
		Phase:     "tool_calling_update_diag",
		Error:     fmt.Sprintf("STEP4 OK: tx committed, broadcasting %d updates, memberIDs=%v, details=%v", len(updates), memberIDs, updateDetails),
	})

	// Broadcast the update.
	bh.BroadcastMessageUpdate(ctx, updates)

	// DIAG: broadcast done
	m.logger.write(LogRecord{
		Timestamp: time.Now(),
		AgentID:   m.agentID,
		Phase:     "tool_calling_update_diag",
		Error:     fmt.Sprintf("STEP5 OK: BroadcastMessageUpdate returned, status=%s", status),
	})
}

// broadcastFunctionCall reads broadcast metadata from the context and sends
// a function_call ephemeral update. It is a no-op when the context does not
// contain broadcast info (e.g. when the middleware is used outside the
// executor). Errors are logged but never propagated (fire-and-forget).
func (m *LoggingMiddleware) broadcastFunctionCall(ctx context.Context, name, args, result, errStr string, durationMs int64, isDone bool) {
	bh, _ := ctx.Value(ctxKeyBroadcastHelper).(*BroadcastHelper)
	if bh == nil {
		return
	}
	humanUserID, _ := ctx.Value(ctxKeyBroadcastHumanUserID).(string)
	agentUserID, _ := ctx.Value(ctxKeyBroadcastAgentUserID).(string)
	conversationID, _ := ctx.Value(ctxKeyBroadcastConversationID).(string)
	if humanUserID == "" || conversationID == "" {
		return
	}
	bh.SendFunctionCall(ctx, humanUserID, agentUserID, conversationID, name, args, result, errStr, durationMs, isDone)
}

// ---------------------------------------------------------------------------
// Tool-calling message helpers (used by resume_handler)
// ---------------------------------------------------------------------------

// buildToolCallingContent constructs the JSON content for a tool_calling message.
func buildToolCallingContent(method, params, result, errMsg, status string) string {
	payload := ToolCallingPayload{
		Name:   method,
		Args:   params,
		Status: status,
	}
	if result != "" {
		payload.Result = truncate(result, 4096)
	}
	if errMsg != "" {
		payload.Error = truncate(errMsg, 2048)
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf(`{"name":%q,"status":%q}`, method, status)
	}
	return string(b)
}

// broadcastToolCallingUpdate creates a UserUpdate for the updated tool_calling
// message and broadcasts it to conversation members. Must be called within an
// existing transaction (tx). The tx is NOT committed or rolled back by this
// function — the caller is responsible for commit/rollback.
func broadcastToolCallingUpdate(ctx context.Context, executor *AgentExecutor, tx *gorm.DB, conversationID string, messageID uint32, content, humanUserID, agentUserID string, logger Logger) {
	// Query the full message for the UserUpdate payload.
	updatedMsg, err := executor.store.MessageStore().GetByConversationAndMessageIDTx(ctx, tx, conversationID, messageID)
	if err != nil {
		logger.Error("broadcastToolCallingUpdate: get message failed", "error", err)
		return
	}
	payload, err := json.Marshal(updatedMsg)
	if err != nil {
		logger.Error("broadcastToolCallingUpdate: marshal payload failed", "error", err)
		return
	}

	// Build member list.
	memberIDs := []string{humanUserID}
	if agentUserID != "" && agentUserID != humanUserID {
		memberIDs = append(memberIDs, agentUserID)
	}

	// Allocate seq and create UserUpdate records.
	now := time.Now()
	updates := make([]model.UserUpdate, 0, len(memberIDs))
	for _, memberID := range memberIDs {
		var latestSeq uint32
		if err := tx.Model(&model.UserUpdate{}).
			Where("user_id = ?", memberID).
			Select("COALESCE(MAX(seq), 0)").
			Scan(&latestSeq).Error; err != nil {
			logger.Error("broadcastToolCallingUpdate: get seq failed", "user_id", memberID, "error", err)
			return
		}
		updates = append(updates, model.UserUpdate{
			ID:        uuid.New().String(),
			UserID:    memberID,
			Seq:       latestSeq + 1,
			Type:      "message",
			Payload:   payload,
			CreatedAt: now,
		})
	}

	// Batch insert.
	if err := tx.Create(&updates).Error; err != nil {
		logger.Error("broadcastToolCallingUpdate: insert UserUpdates failed", "error", err)
		return
	}

	// Broadcast via WebSocket.
	for _, u := range updates {
		wsUpdates := &protocol.PackageDataUpdates{
			Updates: []protocol.PackageDataUpdate{
				{
					Seq:       u.Seq,
					Type:      protocol.UpdateTypeMessage,
					Payload:   u.Payload,
					CreatedAt: u.CreatedAt,
				},
			},
		}
		if err := executor.broadcaster.BroadcastRaw(u.UserID, wsUpdates); err != nil {
			logger.Error("broadcastToolCallingUpdate: broadcast failed", "user_id", u.UserID, "error", err)
		}
	}
}
