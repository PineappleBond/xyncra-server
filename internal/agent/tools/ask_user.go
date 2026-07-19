package tools

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// AskUserInput is the input schema for the ask_user tool.
type AskUserInput struct {
	Question string `json:"question" jsonschema:"description=The question to ask the user for confirmation"`
}

// NewAskUserTool creates a HITL tool that interrupts execution and waits for user input.
// When invoked, it triggers tool.Interrupt which pauses the agent and saves a checkpoint.
// After the user responds via agent_resume RPC, the tool returns with the user's answer.
//
// The tool returns a plain string (not a struct) so that Eino's marshalString
// passes it through without JSON encoding. Returning a struct would produce
// `{"answer":"..."}` which the LLM tends to copy verbatim into its reply,
// leaking internal implementation details to the end user.
func NewAskUserTool() (tool.InvokableTool, error) {
	return utils.InferTool(
		"ask_user",
		"Ask the user a question and wait for their response. Use this for sensitive operations that require human confirmation. The agent will pause until the user responds.",
		func(ctx context.Context, input AskUserInput) (string, error) {
			if input.Question == "" {
				return SoftFailure("question is required"), nil
			}

			// Check if we are being resumed after an interrupt.
			isResumeTarget, hasData, data := tool.GetResumeContext[string](ctx)
			if isResumeTarget && hasData {
				// Return a neutral acknowledgment that doesn't echo the user's answer.
				// Include the answer in a way that guides the LLM's response without repetition.
				if data == "确认" {
					return "User has approved the operation. Proceed to execute it now.", nil
				}
				if data == "取消" {
					return "User has rejected the operation. Cancel and inform the user.", nil
				}
				// For other answers, provide context without echoing
				return fmt.Sprintf("User responded. Continue based on their response: %s", data), nil
			}
			if isResumeTarget && !hasData {
				return "User has confirmed. Proceed with the operation.", nil
			}

			// First call: trigger interrupt with the question.
			// This pauses execution and saves a checkpoint.
			// The function will return after user responds via agent_resume RPC.
			err := tool.Interrupt(ctx, input.Question)
			return "", err
		},
	)
}
