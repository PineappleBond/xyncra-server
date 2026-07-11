package client

import "testing"

func TestIsAgentUser(t *testing.T) {
	tests := []struct {
		name     string
		userID   string
		expected bool
	}{
		{"standard agent ID", "agent/assistant-001", true},
		{"human user", "alice", false},
		{"empty string", "", false},
		{"prefix only", "agent/", true},
		{"case sensitive - uppercase", "Agent/xxx", false},
		{"similar prefix - agents", "agents/xxx", false},
		{"similar prefix - agent-dash", "agent-x", false},
		{"agent with complex ID", "agent/my-bot-123", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsAgentUser(tc.userID)
			if result != tc.expected {
				t.Errorf("IsAgentUser(%q) = %v, want %v", tc.userID, result, tc.expected)
			}
		})
	}
}

func TestAgentUserIDPrefix(t *testing.T) {
	if AgentUserIDPrefix != "agent/" {
		t.Errorf("AgentUserIDPrefix = %q, want %q", AgentUserIDPrefix, "agent/")
	}
}
