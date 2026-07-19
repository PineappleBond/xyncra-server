package tools

import (
	"context"
	"fmt"
	"math/rand"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// WeatherInput is the input schema for the get_weather tool.
type WeatherInput struct {
	City string `json:"city" jsonschema:"description=City name"`
}

// WeatherOutput is the output schema for the get_weather tool.
type WeatherOutput struct {
	Temperature string `json:"temperature"`
	Condition   string `json:"condition"`
	Humidity    string `json:"humidity"`
}

// NewWeatherTool creates a mock weather tool.
// The tool returns deterministic fake data based on the city name so that
// the same city always produces the same result within a process lifetime.
// A missing city is a recoverable failure returned as a ToolResult envelope
// (success=false) so the LLM can supply the argument, rather than aborting
// the run (D-101).
func NewWeatherTool() (tool.InvokableTool, error) {
	return utils.InferTool(
		"get_weather",
		"Get current weather information for a city (mock implementation for development). Returns a JSON envelope {\"success\":true,\"data\":{...}} on success or {\"success\":false,\"error\":\"...\"} on failure.",
		func(ctx context.Context, input WeatherInput) (string, error) {
			if input.City == "" {
				return SoftFailure("city is required"), nil
			}
			// Deterministic pseudo-random values based on city name so that
			// repeated calls for the same city return consistent data.
			seed := int64(0)
			for _, c := range strings.ToLower(input.City) {
				seed = seed*31 + int64(c)
			}
			r := rand.New(rand.NewSource(seed))

			conditions := []string{"Sunny", "Cloudy", "Rainy", "Snowy", "Windy", "Foggy"}
			temp := r.Intn(40) - 5 // range: -5 to 34
			humidity := r.Intn(60) + 30

			return SuccessResult(&WeatherOutput{
				Temperature: fmt.Sprintf("%d°C", temp),
				Condition:   conditions[r.Intn(len(conditions))],
				Humidity:    fmt.Sprintf("%d%%", humidity),
			})
		},
	)
}
