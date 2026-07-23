package agent

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/adk/middlewares/patchtoolcalls"
	"github.com/cloudwego/eino/adk/middlewares/reduction"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
)

// buildMiddleware constructs the middleware chain for an agent based on its
// MiddlewareConfig. The order is fixed: PatchToolCalls -> Summarization -> ToolReduction
// (Eino recommended order, D-079).
//
// Each middleware that fails to initialize is skipped with a warning log (fail-open).
// If no middleware is enabled, returns nil.
func (b *AgentBuilder) buildMiddleware(
	ctx context.Context,
	config *AgentConfig,
	chatModel einomodel.BaseChatModel,
	messageStore *store.MessageStore,
) []adk.ChatModelAgentMiddleware {
	var mws []adk.ChatModelAgentMiddleware

	// 0. DynamicToolProvider (must be first -- injects tools before other middleware sees them)
	if config.Middleware.EnableClientTools {
		if b.clientFunctionProvider != nil {
			dtp := NewDynamicToolProvider(b.clientFunctionProvider, config.Middleware.ClientTools, b.logger,
				b.toolRegistry, config.DynamicTools)
			mws = append(mws, dtp)
		} else {
			log.Default().Printf("agent %s: client_tools enabled but FunctionProvider not set, skipping", config.ID)
		}
	}

	// 1. PatchToolCalls (D-079)
	if config.Middleware.EnablePatchToolCalls {
		mw, err := patchtoolcalls.New(ctx, &patchtoolcalls.Config{})
		if err != nil {
			log.Default().Printf("agent %s: patchtoolcalls middleware init failed, skipping: %v", config.ID, err)
		} else {
			mws = append(mws, mw)
		}
	}

	// 2. Summarization (D-079)
	if config.Middleware.EnableSummarization {
		tokens := config.Middleware.SummarizationTokens
		if tokens <= 0 {
			tokens = 160000
		}
		mw, err := summarization.New(ctx, &summarization.Config{
			Model: chatModel,
			Trigger: &summarization.TriggerCondition{
				ContextTokens: tokens,
			},
			Callback: buildSummarizeCallback(messageStore, b.logger),
		})
		if err != nil {
			log.Default().Printf("agent %s: summarization middleware init failed, skipping: %v", config.ID, err)
		} else {
			mws = append(mws, mw)
		}
	}

	// 3. ToolReduction (D-079)
	if config.Middleware.EnableToolReduction {
		maxChars := config.Middleware.ToolReductionMaxChars
		if maxChars <= 0 {
			maxChars = 50000
		}
		// TODO(Phase 8C): upgrade to filesystem backend when eino-ext provides a
		// stable local.Backend API (D-080). Currently uses in-memory per D-080
		// baseline. Investigated: no adk/backend/local package exists in eino-ext
		// as of v0.9.12; reduction.Backend only requires Write().
		mw, err := reduction.New(ctx, &reduction.Config{
			Backend:           filesystem.NewInMemoryBackend(),
			MaxLengthForTrunc: maxChars,
		})
		if err != nil {
			log.Default().Printf("agent %s: tool reduction middleware init failed, skipping: %v", config.ID, err)
		} else {
			mws = append(mws, mw)
		}
	}

	// N. LLM logging middleware (must be last — observes all prior middleware effects)
	if b.llmLogger != nil {
		mws = append(mws, NewLoggingMiddleware(b.llmLogger, config.ID, config.Model))
	}

	// N+1. Tracing middleware (only when tracing is enabled).
	// Uses the global tracer provider; no-op when tracing is disabled (zero overhead).
	if b.tracingEnabled {
		tmw := NewTracingMiddleware(config.ID, config.Model)
		tmw.SetDebugFilter(b.tracingDebugUsers, b.tracingDebugDevices)
		mws = append(mws, tmw)
	}

	return mws
}

// buildSummarizeCallback creates a Callback that persists the summary to the database.
// It marks old messages as summarized and inserts a new summary message.
func buildSummarizeCallback(messageStore *store.MessageStore, logger Logger) summarization.TypedCallbackFunc[*schema.Message] {
	return func(ctx context.Context, before, after adk.TypedChatModelAgentState[*schema.Message]) error {
		// Get conversationID from context
		convID, ok := ConversationIDFromContext(ctx)
		if !ok || convID == "" {
			if logger != nil {
				logger.Info("summarize callback: no conversation ID in context, skipping persistence")
			}
			return nil
		}

		if messageStore == nil {
			return nil
		}

		// Find the max DB MessageID from before.Messages
		var maxMessageID uint32
		for _, msg := range before.Messages {
			if msg.Extra == nil {
				continue
			}
			if idRaw, ok := msg.Extra["xyncra_message_id"]; ok {
				// JSON unmarshal converts uint32 to float64
				if idFloat, ok := idRaw.(float64); ok {
					id := uint32(idFloat)
					if id > maxMessageID {
						maxMessageID = id
					}
				}
			}
		}

		if maxMessageID == 0 {
			if logger != nil {
				logger.Info("summarize callback: no DB MessageID found in before messages")
			}
			return nil
		}

		// Extract summary content from after.Messages
		// The summary is a user message with Extra["_eino_summarization_content_type"] == "summary"
		var summaryContent string
		for _, msg := range after.Messages {
			if msg.Extra == nil {
				continue
			}
			if ct, ok := msg.Extra["_eino_summarization_content_type"]; ok && ct == "summary" {
				if msg.Content != "" {
					summaryContent = msg.Content
				}
				break
			}
		}

		if summaryContent == "" {
			if logger != nil {
				logger.Info("summarize callback: no summary content found in after messages")
			}
			return nil
		}

		// Create summary message
		summaryMsg := &model.Message{
			ID:             uuid.New().String(),
			ConversationID: convID,
			SenderID:       "system",
			Content:        summaryContent,
			Type:           "summary",
			Status:         "sent",
			CreatedAt:      time.Now(),
		}

		// Use a transaction to ensure atomicity
		tx := messageStore.Begin()
		if tx.Error != nil {
			return fmt.Errorf("summarize callback: begin tx: %w", tx.Error)
		}

		// Insert summary message
		if err := tx.Create(summaryMsg).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("summarize callback: insert summary: %w", err)
		}

		// Mark old messages as summarized
		result := tx.Model(&model.Message{}).
			Where("conversation_id = ? AND message_id <= ? AND summarized = ?", convID, maxMessageID, false).
			Update("summarized", true)
		if result.Error != nil {
			tx.Rollback()
			return fmt.Errorf("summarize callback: mark summarized: %w", result.Error)
		}

		// Commit transaction
		if err := tx.Commit().Error; err != nil {
			return fmt.Errorf("summarize callback: commit tx: %w", err)
		}

		if logger != nil {
			logger.Info("summarize callback: persisted summary",
				"conversation_id", convID,
				"messages_marked", result.RowsAffected,
			)
		}

		return nil
	}
}
