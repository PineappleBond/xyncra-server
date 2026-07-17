package logger

import "log/slog"

// Field helper functions for structured logging. Each function returns a
// slog.Attr with a canonical key name used across Xyncra Server, ensuring
// consistent structured output.
//
// Usage:
//
//	logger.Info("agent completed",
//	    logger.AgentID(id),
//	    logger.ConversationID(cid),
//	    logger.DurationMs(ms),
//	)

// AgentID returns a slog.Attr with key "agent_id".
func AgentID(id string) slog.Attr { return slog.String("agent_id", id) }

// UserID returns a slog.Attr with key "user_id".
func UserID(id string) slog.Attr { return slog.String("user_id", id) }

// DeviceID returns a slog.Attr with key "device_id".
func DeviceID(id string) slog.Attr { return slog.String("device_id", id) }

// ConversationID returns a slog.Attr with key "conversation_id".
func ConversationID(id string) slog.Attr { return slog.String("conversation_id", id) }

// DurationMs returns a slog.Attr with key "duration_ms" and the given integer value.
func DurationMs(ms int64) slog.Attr { return slog.Int64("duration_ms", ms) }

// Model returns a slog.Attr with key "model".
func Model(model string) slog.Attr { return slog.String("model", model) }
