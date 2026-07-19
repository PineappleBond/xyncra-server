/**
 * Integration test: WebSocket lifecycle — connect, send, receive, close.
 */

import { BrowserWebSocketAdapter } from '../../adapters/websocket';

describe('WebSocket Lifecycle Integration', () => {
  beforeEach(() => {
    (globalThis.WebSocket as any).instances = [];
  });

  it('should connect, send messages, and receive data', async () => {
    const adapter = new BrowserWebSocketAdapter();
    const messageCallback = jest.fn();
    adapter.onMessage(messageCallback);

    // Start connection
    const connectPromise = adapter.connect('ws://localhost:8080/ws');
    const ws = (globalThis.WebSocket as any).instances[0];

    // Simulate connection opening
    ws.simulateOpen();
    await connectPromise;

    // Send a message
    const data = new TextEncoder().encode('hello');
    await adapter.send(data);
    expect(ws._sendCalls).toContain(data.buffer);

    // Receive a message
    ws.onmessage({ data: 'response-data' });
    expect(messageCallback).toHaveBeenCalled();
  });

  it('should handle connection error during connect', async () => {
    const adapter = new BrowserWebSocketAdapter();

    const connectPromise = adapter.connect('ws://localhost:8080/ws');
    const ws = (globalThis.WebSocket as any).instances[0];

    ws.simulateError();

    await expect(connectPromise).rejects.toThrow('WebSocket error');
  });

  it('should handle close events', async () => {
    const adapter = new BrowserWebSocketAdapter();
    const closeCallback = jest.fn();
    adapter.onClose(closeCallback);

    const connectPromise = adapter.connect('ws://localhost:8080/ws');
    const ws = (globalThis.WebSocket as any).instances[0];
    ws.simulateOpen();
    await connectPromise;

    // Simulate server closing connection
    ws.simulateClose(1000, 'Normal closure');
    expect(closeCallback).toHaveBeenCalledWith({
      code: 1000,
      reason: 'Normal closure',
      wasClean: true,
    });
  });

  it('should handle device replacement (4001 close code)', async () => {
    const adapter = new BrowserWebSocketAdapter();
    const closeCallback = jest.fn();
    adapter.onClose(closeCallback);

    const connectPromise = adapter.connect('ws://localhost:8080/ws');
    const ws = (globalThis.WebSocket as any).instances[0];
    ws.simulateOpen();
    await connectPromise;

    // Server closes with 4001 (device replaced)
    ws.simulateClose(4001, 'Device replaced');
    expect(closeCallback).toHaveBeenCalledWith({
      code: 4001,
      reason: 'Device replaced',
      wasClean: false,
    });
  });

  it('should gracefully close the connection', async () => {
    const adapter = new BrowserWebSocketAdapter();

    const connectPromise = adapter.connect('ws://localhost:8080/ws');
    const ws = (globalThis.WebSocket as any).instances[0];
    ws.simulateOpen();
    await connectPromise;

    const closePromise = adapter.close();
    ws.simulateClose(1000, '');
    await closePromise;

    expect(ws.readyState).toBe(3); // CLOSED
  });
});
