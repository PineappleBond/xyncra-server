/**
 * ConnectionManager unit tests.
 *
 * Tests cover:
 *   - C1: sendPackage forces version=1
 *   - C2: 4001 close code sets replaced=true
 *   - C3: backoff algorithm
 *   - Connect/disconnect lifecycle
 *   - Disconnect promise
 */

import type { ConnectionCallbacks } from '../connection-manager';
import { backoffDelay, ConnectionManager } from '../connection-manager';
import {
  createMockLogger,
  createMockWebSocket,
  createMockWebSocketFactory,
} from './test-helpers';

describe('ConnectionManager', () => {
  // ---------------------------------------------------------------------------
  // C1: sendPackage forces version=1
  // ---------------------------------------------------------------------------

  test('C1: sendPackage forces version=1', () => {
    const ws = createMockWebSocket();
    const logger = createMockLogger();
    const callbacks: ConnectionCallbacks = {
      onResponse: () => {},
      onUpdates: () => {},
      onRequest: () => {},
      onConnect: () => {},
      onDisconnect: () => {},
    };

    const connMgr = new ConnectionManager({
      serverURL: 'ws://localhost:8080',
      userID: 'user1',
      deviceID: 'device1',
      wsFactory: createMockWebSocketFactory(ws),
      logger,
      callbacks,
    });

    // Manually set connected state for testing sendPackage
    // We need to connect first via the mock
    ws.triggerOpen();
    // The manager is not connected yet since we didn't call connect(),
    // but we can test the version forcing logic by observing the pkg
    const pkg = {
      type: 0 as const,
      version: 999, // should be forced to 1
      data: { id: 'test', method: 'test', params: null },
    };

    // Note: sendPackage will drop if not connected, but it still sets version=1
    connMgr.sendPackage(pkg);
    expect(pkg.version).toBe(1);
  });

  // ---------------------------------------------------------------------------
  // C2: 4001 close code sets replaced=true
  // ---------------------------------------------------------------------------

  test('C2: 4001 close code sets replaced=true', () => {
    const ws = createMockWebSocket();
    const logger = createMockLogger();
    let disconnectReplaced = false;
    const callbacks: ConnectionCallbacks = {
      onResponse: () => {},
      onUpdates: () => {},
      onRequest: () => {},
      onConnect: () => {},
      onDisconnect: (replaced: boolean) => {
        disconnectReplaced = replaced;
      },
    };

    const connMgr = new ConnectionManager({
      serverURL: 'ws://localhost:8080',
      userID: 'user1',
      deviceID: 'device1',
      wsFactory: createMockWebSocketFactory(ws),
      logger,
      callbacks,
    });

    // Simulate connection established then 4001 close
    // We use connect() which internally sets up handlers
    const connectPromise = connMgr.connect();
    ws.triggerOpen();

    return connectPromise.then(async () => {
      expect(connMgr.isConnected()).toBe(true);
      expect(connMgr.isReplaced()).toBe(false);

      // Simulate 4001 close
      ws.triggerClose(4001, 'device replaced');

      // Give event loop a tick
      await new Promise((r) => setTimeout(r, 10));

      expect(connMgr.isReplaced()).toBe(true);
      expect(disconnectReplaced).toBe(true);
    });
  });

  // ---------------------------------------------------------------------------
  // C3: backoff algorithm
  // ---------------------------------------------------------------------------

  test('C3: backoffDelay base * 2^(attempt-1)', () => {
    // With attempt=1: delay = base * 2^0 = 1000
    // Jitter: +/- 25% = [750, 1250]
    const d1 = backoffDelay(1, 1000, 30000);
    expect(d1).toBeGreaterThanOrEqual(750);
    expect(d1).toBeLessThanOrEqual(1250);
  });

  test('C3: backoffDelay doubles each attempt', () => {
    // With attempt=2: delay = 1000 * 2^1 = 2000
    // Jitter: +/- 25% = [1500, 2500]
    const d2 = backoffDelay(2, 1000, 30000);
    expect(d2).toBeGreaterThanOrEqual(1500);
    expect(d2).toBeLessThanOrEqual(2500);
  });

  test('C3: backoffDelay caps at max', () => {
    // attempt=100: exp clamped to 30, delay = 1000 * 2^30 = very large
    // Should cap at max=30000
    const d31 = backoffDelay(100, 1000, 30000);
    // Capped at 30000, jitter +/- 25%: [22500, 37500]
    expect(d31).toBeGreaterThanOrEqual(22500);
    expect(d31).toBeLessThanOrEqual(37500);
  });

  test('C3: backoffDelay handles attempt=0', () => {
    // attempt=0: exp = max(0-1, 0) = 0
    const d0 = backoffDelay(0, 1000, 30000);
    expect(d0).toBeGreaterThanOrEqual(750);
    expect(d0).toBeLessThanOrEqual(1250);
  });

  // ---------------------------------------------------------------------------
  // Connect lifecycle
  // ---------------------------------------------------------------------------

  test('connect establishes connection', async () => {
    const ws = createMockWebSocket();
    const logger = createMockLogger();
    let connected = false;
    const callbacks: ConnectionCallbacks = {
      onResponse: () => {},
      onUpdates: () => {},
      onRequest: () => {},
      onConnect: () => {
        connected = true;
      },
      onDisconnect: () => {},
    };

    const connMgr = new ConnectionManager({
      serverURL: 'ws://localhost:8080',
      userID: 'user1',
      deviceID: 'device1',
      wsFactory: createMockWebSocketFactory(ws),
      logger,
      callbacks,
    });

    const connectPromise = connMgr.connect();
    expect(connMgr.isConnected()).toBe(false);

    ws.triggerOpen();
    await connectPromise;

    expect(connMgr.isConnected()).toBe(true);
    expect(connected).toBe(true);
    expect(connMgr.getAttempt()).toBe(0);
  });

  test('close is idempotent', () => {
    const ws = createMockWebSocket();
    const logger = createMockLogger();
    const callbacks: ConnectionCallbacks = {
      onResponse: () => {},
      onUpdates: () => {},
      onRequest: () => {},
      onConnect: () => {},
      onDisconnect: () => {},
    };

    const connMgr = new ConnectionManager({
      serverURL: 'ws://localhost:8080',
      userID: 'user1',
      deviceID: 'device1',
      wsFactory: createMockWebSocketFactory(ws),
      logger,
      callbacks,
    });

    // Should not throw
    connMgr.close();
    connMgr.close();
  });

  test('getDeviceID returns configured device ID', () => {
    const ws = createMockWebSocket();
    const logger = createMockLogger();
    const callbacks: ConnectionCallbacks = {
      onResponse: () => {},
      onUpdates: () => {},
      onRequest: () => {},
      onConnect: () => {},
      onDisconnect: () => {},
    };

    const connMgr = new ConnectionManager({
      serverURL: 'ws://localhost:8080',
      userID: 'user1',
      deviceID: 'my-device',
      wsFactory: createMockWebSocketFactory(ws),
      logger,
      callbacks,
    });

    expect(connMgr.getDeviceID()).toBe('my-device');
  });
});
