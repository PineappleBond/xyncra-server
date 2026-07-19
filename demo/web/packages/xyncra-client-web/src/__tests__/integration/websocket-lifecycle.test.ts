/**
 * Integration test: WebSocket lifecycle — connect, send, receive, close.
 */

import { BrowserWebSocketAdapter } from '../../adapters/websocket';

class MockWebSocket {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSING = 2;
  static CLOSED = 3;

  static instances: MockWebSocket[] = [];

  readyState = MockWebSocket.CONNECTING;
  url: string;
  binaryType = '';
  onopen: ((event: any) => void) | null = null;
  onclose: ((event: any) => void) | null = null;
  onmessage: ((event: any) => void) | null = null;
  onerror: ((event: any) => void) | null = null;
  _sendCalls: any[] = [];

  constructor(url: string) {
    this.url = url;
    MockWebSocket.instances.push(this);
  }

  send(data: any) {
    this._sendCalls.push(data);
  }

  close() {
    this.readyState = MockWebSocket.CLOSED;
  }

  simulateOpen() {
    this.readyState = MockWebSocket.OPEN;
    if (this.onopen) {
      this.onopen({ type: 'open' });
    }
  }

  simulateMessage(data: any) {
    if (this.onmessage) {
      this.onmessage({
        data: typeof data === 'string' ? data : JSON.stringify(data),
      });
    }
  }

  simulateClose(code = 1000, reason = '') {
    this.readyState = MockWebSocket.CLOSED;
    if (this.onclose) {
      this.onclose({ code, reason, wasClean: code === 1000 });
    }
  }

  simulateError() {
    if (this.onerror) {
      this.onerror({ type: 'error' });
    }
  }
}

(globalThis as any).WebSocket = MockWebSocket;

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
