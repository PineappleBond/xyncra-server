package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// CK-01: ContextWithCallerDevice → CallerDeviceFromContext round-trip
// ---------------------------------------------------------------------------

func TestCallerDevice_RoundTrip(t *testing.T) {
	device := CallerDevice{UserID: "alice", DeviceID: "device-1"}
	ctx := ContextWithCallerDevice(context.Background(), device)
	got, ok := CallerDeviceFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, device, got)
}

// ---------------------------------------------------------------------------
// CK-02: Uninjected context returns zero value + false
// ---------------------------------------------------------------------------

func TestCallerDeviceFromContext_Missing(t *testing.T) {
	got, ok := CallerDeviceFromContext(context.Background())
	assert.False(t, ok)
	assert.Equal(t, CallerDevice{}, got)
}

// ---------------------------------------------------------------------------
// CK-03: Nested override — inner value wins
// ---------------------------------------------------------------------------

func TestCallerDevice_NestedOverride(t *testing.T) {
	outer := CallerDevice{UserID: "alice", DeviceID: "device-1"}
	inner := CallerDevice{UserID: "bob", DeviceID: "device-2"}
	ctx := ContextWithCallerDevice(context.Background(), outer)
	ctx = ContextWithCallerDevice(ctx, inner)
	got, ok := CallerDeviceFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, inner, got)
}
