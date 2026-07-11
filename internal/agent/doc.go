// Package agent provides the AI agent configuration and registry system.
//
// Agents are defined using Markdown files with YAML front matter. The front
// matter contains configuration (ID, model, parameters), and the body is
// the agent's system prompt.
//
// Agent userIDs follow the format "agent/{id}" (D-054).
//
// # Context Management
//
// The ContextManager interface provides conversation history loading with
// token-based trimming and in-memory caching. DBContextManager is the
// default implementation, backed by the MessageStore with sync.Map caching.
package agent
