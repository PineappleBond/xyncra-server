package server

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Device index tests
// ---------------------------------------------------------------------------

// newDeviceTestServer creates a minimal WebSocketServer for device index tests.
// It does not start the HTTP listener.
func newDeviceTestServer(t *testing.T) *WebSocketServer {
	t.Helper()
	srv, err := NewWebSocketServer(
		WSWithAddr(":0"),
		WSWithConnectionStore(&RedisConnectionStore{}),
		WSWithStore(&mockStore{}),
		WSWithBroker(&mockBroker{}),
	)
	require.NoError(t, err)
	return srv
}

// newDeviceTestClient creates a Client suitable for device index tests.
// It has a buffered send channel so SendPackage succeeds without a real WebSocket.
func newDeviceTestClient(t *testing.T, userID, deviceID, connID string) *Client {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &Client{
		userID:   userID,
		deviceID: deviceID,
		connID:   connID,
		send:     make(chan []byte, 10),
		ctx:      ctx,
		cancel:   cancel,
		done:     make(chan struct{}),
	}
}

// readMsgFromSend reads a message from the client's send channel with a timeout.
// Returns nil if no message is available within the timeout.
func readMsgFromSend(t *testing.T, c *Client, timeout time.Duration) []byte {
	t.Helper()
	select {
	case msg := <-c.send:
		return msg
	case <-time.After(timeout):
		return nil
	}
}

// TestDeviceIndex_SendToDevice verifies that sendToDevice delivers a message
// only to the specified device and not to other devices of the same user.
func TestDeviceIndex_SendToDevice(t *testing.T) {
	t.Parallel()

	srv := newDeviceTestServer(t)

	device1 := newDeviceTestClient(t, "user-1", "device-1", "conn-1")
	device2 := newDeviceTestClient(t, "user-1", "device-2", "conn-2")

	// Register both devices in the indexes.
	srv.mu.Lock()
	srv.clients["conn-1"] = device1
	srv.clients["conn-2"] = device2

	srv.clientsByUser["user-1"] = map[string]*Client{
		"conn-1": device1,
		"conn-2": device2,
	}

	key1 := "user-1\x00device-1"
	key2 := "user-1\x00device-2"
	srv.clientsByDevice[key1] = map[string]*Client{"conn-1": device1}
	srv.clientsByDevice[key2] = map[string]*Client{"conn-2": device2}
	srv.mu.Unlock()

	// Send a package to device-1.
	pkg := &protocol.Package{
		Type: protocol.PackageTypeRequest,
		Data: json.RawMessage(`{"id":"r1","method":"ping","params":{}}`),
	}
	err := srv.sendToDevice("user-1", "device-1", pkg)
	require.NoError(t, err)

	// device-1 should receive the message.
	msg := readMsgFromSend(t, device1, 2*time.Second)
	require.NotNil(t, msg, "device-1 should receive the message")

	// device-2 should NOT receive the message.
	msg2 := readMsgFromSend(t, device2, 200*time.Millisecond)
	assert.Nil(t, msg2, "device-2 should not receive the message")
}

// TestDeviceIndex_SendToDevice_Offline verifies that sendToDevice returns
// ErrDeviceOffline when the target device is not connected.
func TestDeviceIndex_SendToDevice_Offline(t *testing.T) {
	t.Parallel()

	srv := newDeviceTestServer(t)

	pkg := &protocol.Package{
		Type: protocol.PackageTypeRequest,
		Data: json.RawMessage(`{"id":"r1","method":"ping","params":{}}`),
	}

	// No devices registered.
	err := srv.sendToDevice("user-1", "device-1", pkg)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDeviceOffline)
}

// TestDeviceIndex_DeviceReplacement verifies that registering a new connection
// for the same device replaces the old one in the clientsByDevice index.
func TestDeviceIndex_DeviceReplacement(t *testing.T) {
	t.Parallel()

	srv := newDeviceTestServer(t)

	oldClient := newDeviceTestClient(t, "user-1", "device-1", "conn-1")
	newClient := newDeviceTestClient(t, "user-1", "device-1", "conn-2")

	// Register old client.
	deviceKey := "user-1\x00device-1"
	srv.mu.Lock()
	srv.clients["conn-1"] = oldClient
	srv.clientsByUser["user-1"] = map[string]*Client{"conn-1": oldClient}
	srv.clientsByDevice[deviceKey] = map[string]*Client{"conn-1": oldClient}
	srv.mu.Unlock()

	// Simulate device replacement: remove old, register new.
	srv.removeClient("conn-1", "user-1", "device-1")

	srv.mu.Lock()
	srv.clients["conn-2"] = newClient
	srv.clientsByUser["user-1"] = map[string]*Client{"conn-2": newClient}
	if srv.clientsByDevice[deviceKey] == nil {
		srv.clientsByDevice[deviceKey] = make(map[string]*Client)
	}
	srv.clientsByDevice[deviceKey]["conn-2"] = newClient
	srv.mu.Unlock()

	// Verify only the new client is in the device index.
	srv.mu.RLock()
	deviceClients := srv.clientsByDevice[deviceKey]
	srv.mu.RUnlock()

	require.Len(t, deviceClients, 1, "only one connection should exist for the device")
	_, exists := deviceClients["conn-2"]
	assert.True(t, exists, "new connection should be in the device index")
	_, exists = deviceClients["conn-1"]
	assert.False(t, exists, "old connection should be removed from the device index")

	// Verify sendToDevice routes to the new client.
	pkg := &protocol.Package{
		Type: protocol.PackageTypeRequest,
		Data: json.RawMessage(`{"id":"r2","method":"ping","params":{}}`),
	}
	err := srv.sendToDevice("user-1", "device-1", pkg)
	require.NoError(t, err)

	msg := readMsgFromSend(t, newClient, 2*time.Second)
	require.NotNil(t, msg, "new client should receive the message")
}

// TestDeviceIndex_Cleanup verifies that removeClient removes the device from
// all indexes (clients, clientsByUser, clientsByDevice).
func TestDeviceIndex_Cleanup(t *testing.T) {
	t.Parallel()

	srv := newDeviceTestServer(t)

	client := newDeviceTestClient(t, "user-1", "device-1", "conn-1")

	deviceKey := "user-1\x00device-1"
	srv.mu.Lock()
	srv.clients["conn-1"] = client
	srv.clientsByUser["user-1"] = map[string]*Client{"conn-1": client}
	srv.clientsByDevice[deviceKey] = map[string]*Client{"conn-1": client}
	srv.mu.Unlock()

	// Verify the client is registered.
	srv.mu.RLock()
	assert.Len(t, srv.clients, 1)
	assert.Len(t, srv.clientsByUser["user-1"], 1)
	assert.Len(t, srv.clientsByDevice[deviceKey], 1)
	srv.mu.RUnlock()

	// Cleanup.
	srv.removeClient("conn-1", "user-1", "device-1")

	// Verify all indexes are cleaned up.
	srv.mu.RLock()
	assert.Len(t, srv.clients, 0, "clients should be empty after cleanup")
	_, userExists := srv.clientsByUser["user-1"]
	assert.False(t, userExists, "clientsByUser entry should be removed")
	_, deviceExists := srv.clientsByDevice[deviceKey]
	assert.False(t, deviceExists, "clientsByDevice entry should be removed")
	srv.mu.RUnlock()
}

// TestDeviceIndex_ConcurrentRegister verifies that concurrent registration of
// different devices does not cause data races.
func TestDeviceIndex_ConcurrentRegister(t *testing.T) {
	t.Parallel()

	srv := newDeviceTestServer(t)

	const numDevices = 50

	// Pre-create all clients in the test goroutine (t.Cleanup is not safe in goroutines).
	type clientEntry struct {
		client    *Client
		deviceKey string
	}
	entries := make([]clientEntry, numDevices)
	for i := range numDevices {
		deviceID := "device-" + string(rune('A'+i%26)) + string(rune('0'+i/26))
		connID := "conn-" + deviceID
		entries[i] = clientEntry{
			client:    newDeviceTestClient(t, "user-1", deviceID, connID),
			deviceKey: "user-1\x00" + deviceID,
		}
	}

	var wg sync.WaitGroup
	wg.Add(numDevices)

	for i := range numDevices {
		go func(idx int) {
			defer wg.Done()
			entry := entries[idx]
			connID := entry.client.ConnID()

			srv.mu.Lock()
			srv.clients[connID] = entry.client
			if srv.clientsByUser["user-1"] == nil {
				srv.clientsByUser["user-1"] = make(map[string]*Client)
			}
			srv.clientsByUser["user-1"][connID] = entry.client
			if srv.clientsByDevice[entry.deviceKey] == nil {
				srv.clientsByDevice[entry.deviceKey] = make(map[string]*Client)
			}
			srv.clientsByDevice[entry.deviceKey][connID] = entry.client
			srv.mu.Unlock()
		}(i)
	}

	wg.Wait()

	// Verify all devices are registered.
	srv.mu.RLock()
	deviceCount := len(srv.clients)
	srv.mu.RUnlock()

	assert.Equal(t, numDevices, deviceCount, "all devices should be registered")
}

// TestDeviceIndex_ConcurrentReplacement verifies that concurrent registration
// of the same device from multiple connections does not corrupt the index.
// The final state should have exactly one connection for the device.
func TestDeviceIndex_ConcurrentReplacement(t *testing.T) {
	t.Parallel()

	srv := newDeviceTestServer(t)

	const numConnections = 20
	deviceKey := "user-1\x00device-1"

	// Pre-create all clients in the test goroutine (t.Cleanup is not safe in goroutines).
	clients := make([]*Client, numConnections)
	for i := range numConnections {
		connID := "conn-" + string(rune('0'+i))
		clients[i] = newDeviceTestClient(t, "user-1", "device-1", connID)
	}

	var wg sync.WaitGroup
	wg.Add(numConnections)

	for i := range numConnections {
		go func(idx int) {
			defer wg.Done()
			client := clients[idx]
			connID := client.ConnID()

			// Simulate device replacement: remove any existing, then register.
			srv.mu.Lock()
			// Remove existing entries for this device.
			if existing := srv.clientsByDevice[deviceKey]; existing != nil {
				for oldConnID, oldClient := range existing {
					delete(srv.clients, oldConnID)
					if userClients := srv.clientsByUser["user-1"]; userClients != nil {
						delete(userClients, oldConnID)
					}
					delete(existing, oldConnID)
					_ = oldClient
				}
			}
			// Register new.
			srv.clients[connID] = client
			if srv.clientsByUser["user-1"] == nil {
				srv.clientsByUser["user-1"] = make(map[string]*Client)
			}
			srv.clientsByUser["user-1"][connID] = client
			if srv.clientsByDevice[deviceKey] == nil {
				srv.clientsByDevice[deviceKey] = make(map[string]*Client)
			}
			srv.clientsByDevice[deviceKey][connID] = client
			srv.mu.Unlock()
		}(i)
	}

	wg.Wait()

	// Verify exactly one connection remains for the device.
	srv.mu.RLock()
	deviceClients := srv.clientsByDevice[deviceKey]
	totalClients := len(srv.clients)
	srv.mu.RUnlock()

	require.Len(t, deviceClients, 1, "exactly one connection should remain for the device")
	assert.Equal(t, 1, totalClients, "exactly one client should be in the global index")
}
