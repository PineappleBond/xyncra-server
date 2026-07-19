// Jest setup for @xyncra/client-web tests
// NOTE: This runs via setupFiles (before test framework), so no jest.fn() here.

// Polyfill TextEncoder/TextDecoder for jsdom
const { TextEncoder, TextDecoder } = require('util');
if (typeof globalThis.TextEncoder === 'undefined') {
  globalThis.TextEncoder = TextEncoder;
}
if (typeof globalThis.TextDecoder === 'undefined') {
  globalThis.TextDecoder = TextDecoder;
}

// Polyfill structuredClone for fake-indexeddb (needed for Node < 17 and jsdom)
if (typeof globalThis.structuredClone === 'undefined') {
  globalThis.structuredClone = (value) => JSON.parse(JSON.stringify(value));
}

require('fake-indexeddb/auto');

// ---------------------------------------------------------------------------
// Mock WebSocket
// ---------------------------------------------------------------------------

class MockWebSocket {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSING = 2;
  static CLOSED = 3;

  static instances = [];

  readyState = MockWebSocket.CONNECTING;
  url;
  binaryType = '';
  onopen = null;
  onclose = null;
  onmessage = null;
  onerror = null;

  constructor(url) {
    this.url = url;
    this._sendCalls = [];
    MockWebSocket.instances.push(this);
  }

  send(data) {
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

  simulateMessage(data) {
    if (this.onmessage) {
      this.onmessage({ data: typeof data === 'string' ? data : JSON.stringify(data) });
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

global.WebSocket = MockWebSocket;

// ---------------------------------------------------------------------------
// Mock localStorage
// ---------------------------------------------------------------------------

const store = {};

global.localStorage = {
  getItem: (key) => store[key] ?? null,
  setItem: (key, value) => {
    store[key] = String(value);
  },
  removeItem: (key) => {
    delete store[key];
  },
  clear: () => {
    Object.keys(store).forEach((key) => {
      delete store[key];
    });
  },
};

// ---------------------------------------------------------------------------
// Mock crypto.randomUUID
// ---------------------------------------------------------------------------

let uuidCounter = 0;

if (!global.crypto) {
  global.crypto = {};
}
global.crypto.randomUUID = () => `test-device-id-${++uuidCounter}`;

// ---------------------------------------------------------------------------
// Mock requestAnimationFrame / cancelAnimationFrame
// ---------------------------------------------------------------------------

if (typeof global.requestAnimationFrame === 'undefined') {
  global.requestAnimationFrame = (cb) => setTimeout(cb, 0);
  global.cancelAnimationFrame = (id) => clearTimeout(id);
}

// ---------------------------------------------------------------------------
// Mock ResizeObserver
// ---------------------------------------------------------------------------

if (typeof global.ResizeObserver === 'undefined') {
  global.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
}

// ---------------------------------------------------------------------------
// Mock matchMedia
// ---------------------------------------------------------------------------

if (typeof window !== 'undefined' && !window.matchMedia) {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    configurable: true,
    value: (query) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => {},
    }),
  });
}
