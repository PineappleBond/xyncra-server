package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponseSidecar_AppendAndRead(t *testing.T) {
	sc := &ResponseSidecar{}

	// Initially empty.
	assert.Nil(t, sc.Updates(), "empty sidecar should return nil")

	// Append one update.
	sc.Append(protocol.PackageDataUpdate{
		Seq:     0,
		Type:    protocol.UpdateTypeConversation,
		Payload: json.RawMessage(`{"conversation_id":"conv-1","action":"update"}`),
	})

	updates := sc.Updates()
	require.Len(t, updates, 1)
	assert.Equal(t, protocol.UpdateTypeConversation, updates[0].Type)
	assert.Equal(t, uint32(0), updates[0].Seq)
}

func TestResponseSidecar_AppendMultiple(t *testing.T) {
	sc := &ResponseSidecar{}

	sc.Append(
		protocol.PackageDataUpdate{
			Seq:     0,
			Type:    protocol.UpdateTypeConversation,
			Payload: json.RawMessage(`{}`),
		},
		protocol.PackageDataUpdate{
			Seq:     1,
			Type:    protocol.UpdateTypeMessage,
			Payload: json.RawMessage(`{}`),
		},
	)

	updates := sc.Updates()
	require.Len(t, updates, 2)
	assert.Equal(t, protocol.UpdateTypeConversation, updates[0].Type)
	assert.Equal(t, protocol.UpdateTypeMessage, updates[1].Type)
}

func TestWithSidecar_And_GetSidecar(t *testing.T) {
	ctx := context.Background()

	// Before injection, GetSidecar returns nil.
	assert.Nil(t, GetSidecar(ctx))

	// Inject sidecar.
	ctx = WithSidecar(ctx)

	// After injection, GetSidecar returns a non-nil sidecar.
	sc := GetSidecar(ctx)
	require.NotNil(t, sc)

	// Sidecar is initially empty.
	assert.Nil(t, sc.Updates())

	// Append and read.
	sc.Append(protocol.PackageDataUpdate{
		Seq:     0,
		Type:    protocol.UpdateTypeConversation,
		Payload: json.RawMessage(`{}`),
	})
	assert.Len(t, sc.Updates(), 1)
}

func TestGetSidecar_NilContext(t *testing.T) {
	// Context without sidecar should return nil.
	ctx := context.Background()
	assert.Nil(t, GetSidecar(ctx))
}

func TestWithSidecar_MultipleRequests(t *testing.T) {
	// Each request gets its own sidecar.
	ctx1 := WithSidecar(context.Background())
	ctx2 := WithSidecar(context.Background())

	sc1 := GetSidecar(ctx1)
	sc2 := GetSidecar(ctx2)

	require.NotNil(t, sc1)
	require.NotNil(t, sc2)

	// They should be different instances.
	assert.NotSame(t, sc1, sc2)

	// Appending to one doesn't affect the other.
	sc1.Append(protocol.PackageDataUpdate{
		Seq:     0,
		Type:    protocol.UpdateTypeConversation,
		Payload: json.RawMessage(`{}`),
	})
	assert.Len(t, sc1.Updates(), 1)
	assert.Nil(t, sc2.Updates())
}
