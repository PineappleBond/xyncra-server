package tools

import (
	"context"
	"fmt"
	"time"

	// Embed the timezone database in the binary so time.LoadLocation works
	// for non-UTC zones even when the container image (alpine) has no
	// /usr/share/zoneinfo (D-101: ui-assistant calls get_current_time with
	// the user's local timezone, e.g. Asia/Shanghai).
	_ "time/tzdata"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// TimeInput is the input schema for the get_current_time tool.
type TimeInput struct {
	Timezone string `json:"timezone,omitempty" jsonschema:"description=IANA timezone name, e.g. Asia/Shanghai (default UTC)"`
}

// TimeOutput is the output schema for the get_current_time tool.
type TimeOutput struct {
	Time     string `json:"time"`
	Timezone string `json:"timezone"`
}

// NewTimeTool creates a tool that returns the current time in the requested
// timezone. If the timezone is empty or invalid, UTC is used. A recoverable
// failure (invalid timezone) is returned as a ToolResult envelope with
// success=false rather than a Go error, so the LLM can self-correct (D-101).
func NewTimeTool() (tool.InvokableTool, error) {
	return utils.InferTool(
		"get_current_time",
		"Get the current date and time in a specified timezone. Returns a JSON envelope {\"success\":true,\"data\":{...}} on success or {\"success\":false,\"error\":\"...\"} on failure.",
		func(ctx context.Context, input TimeInput) (string, error) {
			tz := input.Timezone
			if tz == "" {
				tz = "UTC"
			}

			loc, err := time.LoadLocation(tz)
			if err != nil {
				// Recoverable: tell the LLM the timezone is invalid so it can
				// pick a valid IANA name, rather than aborting the run (D-101).
				return SoftFailure(fmt.Sprintf("invalid timezone %q: use a valid IANA timezone name (e.g. Asia/Shanghai, UTC)", tz)), nil
			}

			now := time.Now().In(loc)
			return SuccessResult(&TimeOutput{
				Time:     now.Format(time.RFC3339),
				Timezone: loc.String(),
			})
		},
	)
}
