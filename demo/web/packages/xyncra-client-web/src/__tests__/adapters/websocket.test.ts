import {
  BrowserWebSocketAdapter,
  BrowserWebSocketFactory,
} from '../../adapters/websocket';

describe('BrowserWebSocketAdapter', () => {
  let adapter: BrowserWebSocketAdapter;

  beforeEach(() => {
    adapter = new BrowserWebSocketAdapter();
    (globalThis.WebSocket as any).instances = [];
  });

  describe('connect', () => {
    it('should create a WebSocket and resolve on open', async () => {
      const connectPromise = adapter.connect('ws://localhost:8080/ws');
      const ws = (globalThis.WebSocket as any).instances[0];

      expect(ws).toBeDefined();
      expect(ws.url).toBe('ws://localhost:8080/ws');
      expect(ws.binaryType).toBe('arraybuffer');

      ws.simulateOpen();
      await connectPromise;
    });

    it('should reject on error', async () => {
      const connectPromise = adapter.connect('ws://localhost:8080/ws');
      const ws = (globalThis.WebSocket as any).instances[0];

      ws.simulateError();
      await expect(connectPromise).rejects.toThrow('WebSocket error');
    });
  });

  describe('send', () => {
    it('should send data when WebSocket is open', async () => {
      const connectPromise = adapter.connect('ws://test');
      const ws = (globalThis.WebSocket as any).instances[0];
      ws.simulateOpen();
      await connectPromise;

      const data = new TextEncoder().encode('hello');
      await adapter.send(data);
      expect(ws._sendCalls).toContain(data.buffer);
    });

    it('should throw when WebSocket is not open', async () => {
      await expect(
        adapter.send(new TextEncoder().encode('hello')),
      ).rejects.toThrow('WebSocket is not open');
    });
  });

  describe('close', () => {
    it('should resolve immediately when no WebSocket exists', async () => {
      await expect(adapter.close()).resolves.toBeUndefined();
    });

    it('should close the WebSocket and resolve', async () => {
      const connectPromise = adapter.connect('ws://test');
      const ws = (globalThis.WebSocket as any).instances[0];
      ws.simulateOpen();
      await connectPromise;

      const closePromise = adapter.close();
      ws.simulateClose(1000, '');
      await closePromise;
      expect(ws.readyState).toBe(3); // CLOSED
    });

    it('should resolve immediately if already closed', async () => {
      const connectPromise = adapter.connect('ws://test');
      const ws = (globalThis.WebSocket as any).instances[0];
      ws.simulateOpen();
      await connectPromise;

      ws.readyState = 3; // CLOSED
      await expect(adapter.close()).resolves.toBeUndefined();
    });
  });

  describe('onMessage', () => {
    it('should receive ArrayBuffer messages', async () => {
      const messageCallback = jest.fn();
      adapter.onMessage(messageCallback);

      const connectPromise = adapter.connect('ws://test');
      const ws = (globalThis.WebSocket as any).instances[0];
      ws.simulateOpen();
      await connectPromise;

      const buffer = new ArrayBuffer(5);
      const view = new Uint8Array(buffer);
      view.set([72, 101, 108, 108, 111]);
      ws.onmessage({ data: buffer });

      expect(messageCallback).toHaveBeenCalledWith(expect.any(Uint8Array));
    });

    it('should receive string messages', async () => {
      const messageCallback = jest.fn();
      adapter.onMessage(messageCallback);

      const connectPromise = adapter.connect('ws://test');
      const ws = (globalThis.WebSocket as any).instances[0];
      ws.simulateOpen();
      await connectPromise;

      ws.onmessage({ data: 'hello' });

      expect(messageCallback).toHaveBeenCalled();
      const received = messageCallback.mock.calls[0][0];
      expect(received).toBeDefined();
      expect(received.length).toBeGreaterThan(0);
    });
  });

  describe('onClose', () => {
    it('should receive close events', async () => {
      const closeCallback = jest.fn();
      adapter.onClose(closeCallback);

      const connectPromise = adapter.connect('ws://test');
      const ws = (globalThis.WebSocket as any).instances[0];
      ws.simulateOpen();
      await connectPromise;

      ws.simulateClose(1000, 'normal');
      expect(closeCallback).toHaveBeenCalledWith({
        code: 1000,
        reason: 'normal',
        wasClean: true,
      });
    });
  });

  describe('onError', () => {
    it('should receive error events', async () => {
      const errorCallback = jest.fn();
      adapter.onError(errorCallback);

      const connectPromise = adapter.connect('ws://test');
      const ws = (globalThis.WebSocket as any).instances[0];

      ws.simulateError();
      expect(errorCallback).toHaveBeenCalled();
      await expect(connectPromise).rejects.toThrow();
    });
  });
});

describe('CoreWebSocketBridge.send', () => {
  it('send string does not throw a TypeError', () => {
    const factory = new BrowserWebSocketFactory();
    (globalThis.WebSocket as any).instances = [];

    const bridge = factory.create('ws://localhost/test');
    expect(typeof (bridge as any).send).toBe('function');
    // Not open yet → throws 'WebSocket is not open' (Error), not a TypeError
    // from type conversion. Both string and Uint8Array reach the same guard.
    expect(() => bridge.send('hello')).toThrow('WebSocket is not open');
  });

  it('send Uint8Array does not throw a TypeError', () => {
    const factory = new BrowserWebSocketFactory();
    (globalThis.WebSocket as any).instances = [];

    const bridge = factory.create('ws://localhost/test');
    expect(() => bridge.send(new Uint8Array([1, 2, 3]))).toThrow(
      'WebSocket is not open',
    );
  });
});

describe('BrowserWebSocketFactory', () => {
  it('should create an IWebSocket instance', () => {
    const factory = new BrowserWebSocketFactory();
    (globalThis.WebSocket as any).instances = [];

    const ws = factory.create('ws://test');
    expect(ws).toBeDefined();
    expect((globalThis.WebSocket as any).instances).toHaveLength(1);
  });

  it('should set binaryType to arraybuffer', () => {
    const factory = new BrowserWebSocketFactory();
    (globalThis.WebSocket as any).instances = [];

    factory.create('ws://test');
    const wsInstance = (globalThis.WebSocket as any).instances[0];
    expect(wsInstance.binaryType).toBe('arraybuffer');
  });

  it('should forward open events', () => {
    const factory = new BrowserWebSocketFactory();
    (globalThis.WebSocket as any).instances = [];

    const ws = factory.create('ws://test');
    const openHandler = jest.fn();
    ws.onopen(openHandler);

    const wsInstance = (globalThis.WebSocket as any).instances[0];
    wsInstance.simulateOpen();

    expect(openHandler).toHaveBeenCalled();
  });

  it('should forward message events', () => {
    const factory = new BrowserWebSocketFactory();
    (globalThis.WebSocket as any).instances = [];

    const ws = factory.create('ws://test');
    const messageHandler = jest.fn();
    ws.onmessage(messageHandler);

    const wsInstance = (globalThis.WebSocket as any).instances[0];
    wsInstance.onmessage({ data: 'hello' });

    expect(messageHandler).toHaveBeenCalledWith('hello');
  });

  it('should forward close events', () => {
    const factory = new BrowserWebSocketFactory();
    (globalThis.WebSocket as any).instances = [];

    const ws = factory.create('ws://test');
    const closeHandler = jest.fn();
    ws.onclose(closeHandler);

    const wsInstance = (globalThis.WebSocket as any).instances[0];
    wsInstance.simulateClose(1000, 'done');

    expect(closeHandler).toHaveBeenCalledWith(1000, 'done');
  });

  it('should forward error events', () => {
    const factory = new BrowserWebSocketFactory();
    (globalThis.WebSocket as any).instances = [];

    const ws = factory.create('ws://test');
    const errorHandler = jest.fn();
    ws.onerror(errorHandler);

    const wsInstance = (globalThis.WebSocket as any).instances[0];
    wsInstance.simulateError();

    expect(errorHandler).toHaveBeenCalledWith(expect.any(Error));
  });

  it('should send data when open', () => {
    const factory = new BrowserWebSocketFactory();
    (globalThis.WebSocket as any).instances = [];

    const ws = factory.create('ws://test');
    const wsInstance = (globalThis.WebSocket as any).instances[0];
    wsInstance.readyState = 1; // OPEN

    ws.send('hello');
    expect(wsInstance._sendCalls).toContain('hello');
  });

  it('should throw on send when not open', () => {
    const factory = new BrowserWebSocketFactory();
    (globalThis.WebSocket as any).instances = [];

    const ws = factory.create('ws://test');
    expect(() => ws.send('hello')).toThrow('WebSocket is not open');
  });

  it('should report readyState', () => {
    const factory = new BrowserWebSocketFactory();
    (globalThis.WebSocket as any).instances = [];

    const ws = factory.create('ws://test');
    expect(ws.readyState).toBe(0); // CONNECTING (default)

    const wsInstance = (globalThis.WebSocket as any).instances[0];
    wsInstance.readyState = 1;
    expect(ws.readyState).toBe(1);
  });
});
