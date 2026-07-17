package server

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/PineappleBond/xyncra-server/internal/tracing"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// findServerSpan returns the first span with the given name, or nil.
func findServerSpan(spans []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	for _, sp := range spans {
		if sp.Name() == name {
			return sp
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// RedisConnectionStore integration tests
// ---------------------------------------------------------------------------

func TestRedisConnectionStore_Add_EmitsSpan(t *testing.T) {
	store, _, _ := setupTestRedisConnectionStore(t, 0)
	startIdx := len(recorder.Ended())

	info := makeConnInfo("conn-1", "user-1")
	err := store.Add(context.Background(), info)
	require.NoError(t, err)

	spans := spansSince(startIdx)
	sp := findServerSpan(spans, tracing.SpanRedisConnectionAdd)
	require.NotNil(t, sp, "expected span %q", tracing.SpanRedisConnectionAdd)
	attrs := roAttrMap(sp)
	assert.Equal(t, "conn-1", attrs[tracing.AttrConnID])
	assert.Equal(t, "user-1", attrs[tracing.AttrUserID])
}

func TestRedisConnectionStore_Get_EmitsSpan(t *testing.T) {
	store, _, _ := setupTestRedisConnectionStore(t, 0)
	ctx := context.Background()

	// Pre-populate a connection.
	info := makeConnInfo("conn-get", "user-1")
	require.NoError(t, store.Add(ctx, info))

	startIdx := len(recorder.Ended())
	got, err := store.Get(ctx, "conn-get")
	require.NoError(t, err)
	require.NotNil(t, got)

	spans := spansSince(startIdx)
	sp := findServerSpan(spans, tracing.SpanRedisConnectionGet)
	require.NotNil(t, sp, "expected span %q", tracing.SpanRedisConnectionGet)
	attrs := roAttrMap(sp)
	assert.Equal(t, "conn-get", attrs[tracing.AttrConnID])
}

func TestRedisConnectionStore_Get_NotFound_EmitsSpan(t *testing.T) {
	store, _, _ := setupTestRedisConnectionStore(t, 0)
	startIdx := len(recorder.Ended())

	_, err := store.Get(context.Background(), "nonexistent")
	require.Error(t, err)

	spans := spansSince(startIdx)
	sp := findServerSpan(spans, tracing.SpanRedisConnectionGet)
	require.NotNil(t, sp, "expected span %q even on not-found", tracing.SpanRedisConnectionGet)
}

// ---------------------------------------------------------------------------
// RedisPendingStore integration tests
// ---------------------------------------------------------------------------

func TestRedisPendingStore_Save_EmitsSpan(t *testing.T) {
	store, _ := setupTestRedisPendingStore(t)
	startIdx := len(recorder.Ended())

	req := makePendingReq("req-1", "user-1", "device-1", "send_message")
	err := store.Save(context.Background(), req)
	require.NoError(t, err)

	spans := spansSince(startIdx)
	sp := findServerSpan(spans, tracing.SpanRedisPendingSave)
	require.NotNil(t, sp, "expected span %q", tracing.SpanRedisPendingSave)
	attrs := roAttrMap(sp)
	assert.Equal(t, "user-1", attrs[tracing.AttrUserID])
	assert.Equal(t, "device-1", attrs[tracing.AttrDeviceID])
}

// ---------------------------------------------------------------------------
// RedisNodeBroadcaster integration tests
// ---------------------------------------------------------------------------

// TestRedisNodeBroadcaster_Publish_EmitsSpan verifies that Publish emits a
// redis.broadcaster.publish span. Uses miniredis for the PUBLISH command.
func TestRedisNodeBroadcaster_Publish_EmitsSpan(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	broadcaster := NewRedisNodeBroadcaster(client, "test-trace")
	t.Cleanup(func() { _ = broadcaster.Close() })

	startIdx := len(recorder.Ended())

	updates := &protocol.PackageDataUpdates{
		Updates: []protocol.PackageDataUpdate{
			{Seq: 1, Type: "message"},
		},
	}
	err = broadcaster.Publish(context.Background(), "user-1", updates, "node-1")
	require.NoError(t, err)

	spans := spansSince(startIdx)
	sp := findServerSpan(spans, tracing.SpanRedisBroadcasterPublish)
	require.NotNil(t, sp, "expected span %q", tracing.SpanRedisBroadcasterPublish)
	attrs := roAttrMap(sp)
	assert.Equal(t, "user-1", attrs[tracing.AttrUserID])
}
