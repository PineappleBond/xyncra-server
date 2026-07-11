package client

import "strings"

// AgentUserIDPrefix is the reserved prefix for agent user IDs (D-054).
const AgentUserIDPrefix = "agent/"

// IsAgentUser reports whether the given userID belongs to an agent.
// It checks the "agent/" prefix convention (D-054).
func IsAgentUser(userID string) bool {
	return strings.HasPrefix(userID, AgentUserIDPrefix)
}
