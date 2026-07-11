package tools

import (
	"context"
	"fmt"
	"time"

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
// timezone. If the timezone is empty or invalid, UTC is used.
func NewTimeTool() (tool.InvokableTool, error) {
	return utils.InferTool(
		"get_current_time",
		"Get the current date and time in a specified timezone",
		func(ctx context.Context, input TimeInput) (*TimeOutput, error) {
			tz := input.Timezone
			if tz == "" {
				tz = "UTC"
			}

			loc, err := time.LoadLocation(tz)
			if err != nil {
				return nil, fmt.Errorf("invalid timezone %q: %w", tz, err)
			}

			now := time.Now().In(loc)
			return &TimeOutput{
				Time:     now.Format(time.RFC3339),
				Timezone: loc.String(),
			}, nil
		},
	)
}
