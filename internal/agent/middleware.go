package agent

import (
	"context"
	"log"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/adk/middlewares/patchtoolcalls"
	"github.com/cloudwego/eino/adk/middlewares/reduction"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	einomodel "github.com/cloudwego/eino/components/model"
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
) []adk.ChatModelAgentMiddleware {
	var mws []adk.ChatModelAgentMiddleware

	// 0. DynamicToolProvider (must be first -- injects tools before other middleware sees them)
	if config.Middleware.EnableClientTools {
		if b.clientFunctionProvider != nil && b.clientCaller != nil {
			dtp := NewDynamicToolProvider(b.clientFunctionProvider, b.clientCaller, config.Middleware.ClientTools, nil,
				b.toolRegistry, config.DynamicTools)
			mws = append(mws, dtp)
		} else {
			log.Default().Printf("agent %s: client_tools enabled but FunctionProvider/Caller not set, skipping", config.ID)
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
