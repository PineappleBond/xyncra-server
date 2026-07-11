package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUpdateTypeAgentConstants(t *testing.T) {
	// Verify D-087 constants have the expected string values.
	assert.Equal(t, "agent_status", UpdateTypeAgentStatus)
	assert.Equal(t, "agent_question", UpdateTypeAgentQuestion)
	assert.Equal(t, "agent_checkpoint_created", UpdateTypeAgentCheckpointCreated)
	assert.Equal(t, "agent_timeout", UpdateTypeAgentTimeout)
}

func TestUpdateTypeAgentConstants_Distinct(t *testing.T) {
	// All agent ephemeral types must be distinct.
	types := []string{
		UpdateTypeAgentStatus,
		UpdateTypeAgentQuestion,
		UpdateTypeAgentCheckpointCreated,
		UpdateTypeAgentTimeout,
	}
	seen := make(map[string]bool)
	for _, typ := range types {
		assert.False(t, seen[typ], "duplicate update type: %s", typ)
		seen[typ] = true
	}
}
