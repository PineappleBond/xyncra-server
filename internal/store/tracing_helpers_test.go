package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/internal/tracing"
)

// testRecorder captures spans emitted during store tests.
var testRecorder *tracetest.SpanRecorder

func TestMain(m *testing.M) {
	testRecorder = tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(testRecorder),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	m.Run()
}

// testSpansSince returns spans that ended after the given index.
func testSpansSince(startIdx int) []sdktrace.ReadOnlySpan {
	all := testRecorder.Ended()
	if startIdx >= len(all) {
		return nil
	}
	return all[startIdx:]
}

// testRoAttrMap extracts attributes from a ReadOnlySpan into a map[string]string.
func testRoAttrMap(s sdktrace.ReadOnlySpan) map[string]string {
	m := make(map[string]string)
	for _, kv := range s.Attributes() {
		m[string(kv.Key)] = kv.Value.Emit()
	}
	return m
}

// findSpan returns the first span with the given name, or nil.
func findSpan(spans []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	for _, sp := range spans {
		if sp.Name() == name {
			return sp
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Unit tests for startSpan helper
// ---------------------------------------------------------------------------

func TestStartSpan_CreatesSpanWithName(t *testing.T) {
	startIdx := len(testRecorder.Ended())

	_, finish := startSpan(context.Background(), "test.span.name")
	finish(nil)

	spans := testSpansSince(startIdx)
	require.Len(t, spans, 1)
	assert.Equal(t, "test.span.name", spans[0].Name())
}

func TestStartSpan_RecordsAttributes(t *testing.T) {
	startIdx := len(testRecorder.Ended())

	_, finish := startSpan(context.Background(), "test.span.attrs",
		attribute.String("key1", "value1"),
		attribute.Int("key2", 42),
	)
	finish(nil)

	spans := testSpansSince(startIdx)
	require.Len(t, spans, 1)
	attrs := testRoAttrMap(spans[0])
	assert.Equal(t, "value1", attrs["key1"])
	assert.Equal(t, "42", attrs["key2"])
}

func TestStartSpan_NoError_StatusUnset(t *testing.T) {
	startIdx := len(testRecorder.Ended())

	_, finish := startSpan(context.Background(), "test.span.noerror")
	finish(nil)

	spans := testSpansSince(startIdx)
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Unset, spans[0].Status().Code)
}

func TestStartSpan_WithError_SetsErrorStatus(t *testing.T) {
	startIdx := len(testRecorder.Ended())

	_, finish := startSpan(context.Background(), "test.span.error")
	finish(assert.AnError)

	spans := testSpansSince(startIdx)
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status().Code)
	assert.Contains(t, spans[0].Status().Description, assert.AnError.Error())
	assert.Len(t, spans[0].Events(), 1)
	assert.Equal(t, "exception", spans[0].Events()[0].Name)
}

func TestStartSpan_ReturnsContextWithSpan(t *testing.T) {
	ctx, finish := startSpan(context.Background(), "test.span.ctx")
	defer finish(nil)

	spanCtx := oteltrace.SpanFromContext(ctx)
	assert.NotNil(t, spanCtx)
	assert.True(t, spanCtx.SpanContext().IsValid())
}

func TestStartSpan_NestedSpans_ParentChild(t *testing.T) {
	startIdx := len(testRecorder.Ended())

	ctx, outerFinish := startSpan(context.Background(), "outer.span")
	_, innerFinish := startSpan(ctx, "inner.span")
	innerFinish(nil)
	outerFinish(nil)

	spans := testSpansSince(startIdx)
	require.Len(t, spans, 2)
	// inner ended first, outer ended second
	inner := spans[0]
	outer := spans[1]
	assert.Equal(t, "inner.span", inner.Name())
	assert.Equal(t, "outer.span", outer.Name())
	assert.Equal(t, outer.SpanContext().SpanID(), inner.Parent().SpanID())
}

// ---------------------------------------------------------------------------
// Integration spot-check tests for DB layer spans
// ---------------------------------------------------------------------------

func TestConversationStore_Get_EmitsSpan(t *testing.T) {
	s := setupSQLite(t)
	startIdx := len(testRecorder.Ended())

	cs := s.ConversationStore()
	ctx := context.Background()
	conv := newTestConv("conv-1", "user-1", "user-2", "direct", "Test Chat")
	require.NoError(t, cs.Create(ctx, conv))

	_, err := cs.Get(ctx, conv.ID)
	require.NoError(t, err)

	spans := testSpansSince(startIdx)
	sp := findSpan(spans, tracing.SpanDBConversationGet)
	require.NotNil(t, sp, "expected span %q", tracing.SpanDBConversationGet)
	attrs := testRoAttrMap(sp)
	assert.Equal(t, "conv-1", attrs[tracing.AttrConversationID])
}

func TestConversationStore_Get_NotFound_EmitsSpanWithError(t *testing.T) {
	s := setupSQLite(t)
	startIdx := len(testRecorder.Ended())

	cs := s.ConversationStore()
	_, err := cs.Get(context.Background(), "nonexistent-id")
	require.Error(t, err)

	spans := testSpansSince(startIdx)
	sp := findSpan(spans, tracing.SpanDBConversationGet)
	require.NotNil(t, sp, "expected span %q with error status", tracing.SpanDBConversationGet)
	assert.Equal(t, codes.Error, sp.Status().Code)
}

func TestMessageStore_Create_EmitsSpan(t *testing.T) {
	s := setupSQLite(t)
	startIdx := len(testRecorder.Ended())

	// Create a conversation first so the FK is valid.
	cs := s.ConversationStore()
	ctx := context.Background()
	conv := newTestConv("conv-msg-1", "user-1", "user-2", "direct", "Test")
	require.NoError(t, cs.Create(ctx, conv))

	ms := s.MessageStore()
	msg := &model.Message{
		ID:              "msg-1",
		ClientMessageID: "msg-client-1",
		ConversationID:  conv.ID,
		MessageID:       1,
		SenderID:        "user-1",
		Content:         "hello",
		CreatedAt:       testNow,
	}
	require.NoError(t, ms.Create(ctx, msg))

	spans := testSpansSince(startIdx)
	sp := findSpan(spans, tracing.SpanDBMessageCreate)
	require.NotNil(t, sp, "expected span %q", tracing.SpanDBMessageCreate)
}

func TestStore_Ping_EmitsSpan(t *testing.T) {
	s := setupSQLite(t)
	startIdx := len(testRecorder.Ended())

	require.NoError(t, s.Ping(context.Background()))

	spans := testSpansSince(startIdx)
	sp := findSpan(spans, tracing.SpanDBStorePing)
	require.NotNil(t, sp, "expected span %q", tracing.SpanDBStorePing)
	assert.Equal(t, codes.Unset, sp.Status().Code)
}

func TestStore_SendMessage_EmitsNestedSpans(t *testing.T) {
	s := setupSQLite(t)
	startIdx := len(testRecorder.Ended())

	cs := s.ConversationStore()
	ctx := context.Background()
	conv := newTestConv("conv-send", "user-1", "user-2", "direct", "Test")
	require.NoError(t, cs.Create(ctx, conv))

	msg := &model.Message{
		ID:              "msg-send",
		ClientMessageID: "msg-send-client",
		ConversationID:  conv.ID,
		SenderID:        "user-1",
		Content:         "hello",
		CreatedAt:       testNow,
	}
	result, err := s.SendMessage(ctx, msg, []string{"user-1", "user-2"})
	require.NoError(t, err)
	require.NotNil(t, result)

	spans := testSpansSince(startIdx)
	sendSpan := findSpan(spans, tracing.SpanDBStoreSendMessage)
	txSpan := findSpan(spans, tracing.SpanDBStoreTransaction)
	require.NotNil(t, sendSpan, "expected SpanDBStoreSendMessage")
	require.NotNil(t, txSpan, "expected SpanDBStoreTransaction")
	assert.Equal(t, sendSpan.SpanContext().SpanID(), txSpan.Parent().SpanID(),
		"transaction span should be child of send_message span")
}

func TestNestedSpans_HealthCheckPing(t *testing.T) {
	s := setupSQLite(t)
	startIdx := len(testRecorder.Ended())

	require.NoError(t, s.HealthCheck(context.Background()))

	spans := testSpansSince(startIdx)
	healthSpan := findSpan(spans, tracing.SpanDBStoreHealthCheck)
	pingSpan := findSpan(spans, tracing.SpanDBStorePing)
	require.NotNil(t, healthSpan, "expected SpanDBStoreHealthCheck")
	require.NotNil(t, pingSpan, "expected SpanDBStorePing")
	assert.Equal(t, healthSpan.SpanContext().SpanID(), pingSpan.Parent().SpanID(),
		"ping span should be child of health_check span")
}
