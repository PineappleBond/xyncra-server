// Package tools provides a factory-pattern tool registry and built-in tool
// implementations for the Xyncra Agent system.
//
// The Registry manages tool creation via named factories (D-078). Agent
// configurations reference tools by name; unregistered names are logged and
// skipped (fail-open) so that missing optional tools never block agent
// construction.
//
// Built-in tools registered into DefaultRegistry at init time:
//   - get_weather          — mock weather data (development/demo)
//   - get_current_time     — current time in any IANA timezone
//   - retrieve_tool_result — retrieve a previously truncated tool result by ID
package tools
