/**
 * @packageDocumentation
 * Browser WebSocket adapter — wraps the native browser WebSocket API.
 *
 * Provides a Promise-based, typed wrapper around the browser's built-in
 * WebSocket global. BrowserWebSocketFactory implements IWebSocketFactory
 * from @xyncra/client-core so it can be injected via ClientOptions.
 *
 * Includes automatic reconnection with exponential backoff.
 *
 * @module
 */

import type { IWebSocket, IWebSocketFactory } from '@xyncra/client-core';

// ---------------------------------------------------------------------------
// Reconnection configuration
// ---------------------------------------------------------------------------

const RECONNECT_MAX_RETRIES = 5;
const RECONNECT_BASE_DELAY = 1000; // 1s
const RECONNECT_MAX_DELAY = 30000; // 30s

export interface ReconnectionState {
  isReconnecting: boolean;
  attempt: number;
  maxRetries: number;
  nextRetryIn: number; // seconds until next attempt
}

// ---------------------------------------------------------------------------
// WebSocketAdapter — browser-native WebSocket wrapper interface
// ---------------------------------------------------------------------------

/**
 * CloseEvent mirrors the browser's native CloseEvent.
 * Defined locally to avoid importing DOM types at the module level.
 */
export interface CloseEvent {
  code: number;
  reason: string;
  wasClean: boolean;
}

/**
 * WebSocketAdapter defines a Promise-based WebSocket interface for browsers.
 * This is the web package's own adapter interface, separate from the
 * core's IWebSocket which uses callback registration.
 */
export interface WebSocketAdapter {
  /** Connect to the given WebSocket URL. Resolves when the connection opens. */
  connect(url: string): Promise<void>;
  /** Send binary data over the connection. */
  send(data: Uint8Array): Promise<void>;
  /** Close the connection gracefully. */
  close(): Promise<void>;
  /** Register a handler for incoming messages. */
  onMessage(callback: (data: Uint8Array) => void): void;
  /** Register a handler for connection close events. */
  onClose(callback: (event: CloseEvent) => void): void;
  /** Register a handler for errors. */
  onError(callback: (event: Event) => void): void;
}

// ---------------------------------------------------------------------------
// BrowserWebSocketAdapter — Promise-based wrapper (WebSocketAdapter)
// ---------------------------------------------------------------------------

/**
 * BrowserWebSocketAdapter wraps the browser's native WebSocket global
 * with a Promise-based interface.
 *
 * Implements {@link WebSocketAdapter} — the web package's Promise-based API.
 */
export class BrowserWebSocketAdapter implements WebSocketAdapter {
  private ws: WebSocket | null = null;
  private messageCallback: ((data: Uint8Array) => void) | null = null;
  private closeCallback: ((event: CloseEvent) => void) | null = null;
  private errorCallback: ((event: Event) => void) | null = null;

  async connect(url: string): Promise<void> {
    return new Promise<void>((resolve, reject) => {
      try {
        this.ws = new WebSocket(url);
        this.ws.binaryType = 'arraybuffer';
      } catch (err) {
        reject(err);
        return;
      }

      this.ws.onopen = () => {
        resolve();
      };

      this.ws.onmessage = (event: MessageEvent) => {
        let data: Uint8Array;
        if (event.data instanceof ArrayBuffer) {
          data = new Uint8Array(event.data);
        } else if (typeof event.data === 'string') {
          data = new TextEncoder().encode(event.data);
        } else {
          data = new Uint8Array(0);
        }

        if (this.messageCallback) {
          this.messageCallback(data);
        }
      };

      this.ws.onclose = (event: globalThis.CloseEvent) => {
        const closeEvent: CloseEvent = {
          code: event.code,
          reason: event.reason,
          wasClean: event.wasClean,
        };
        if (this.closeCallback) {
          this.closeCallback(closeEvent);
        }
      };

      this.ws.onerror = (event: Event) => {
        if (this.errorCallback) {
          this.errorCallback(event);
        }
        reject(new Error('WebSocket error'));
      };
    });
  }

  async send(data: Uint8Array): Promise<void> {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      throw new Error('WebSocket is not open');
    }
    this.ws.send(data.buffer as ArrayBuffer);
  }

  async close(): Promise<void> {
    if (!this.ws) return;
    return new Promise<void>((resolve) => {
      if (
        this.ws!.readyState === WebSocket.CLOSED ||
        this.ws!.readyState === WebSocket.CLOSING
      ) {
        resolve();
        return;
      }
      const prevOnClose = this.ws!.onclose;
      this.ws!.onclose = (event: globalThis.CloseEvent) => {
        if (prevOnClose) {
          (prevOnClose as (event: globalThis.CloseEvent) => void).call(
            this.ws,
            event,
          );
        }
        resolve();
      };
      this.ws!.close();
    });
  }

  onMessage(callback: (data: Uint8Array) => void): void {
    this.messageCallback = callback;
  }

  onClose(callback: (event: CloseEvent) => void): void {
    this.closeCallback = callback;
  }

  onError(callback: (event: Event) => void): void {
    this.errorCallback = callback;
  }
}

// ---------------------------------------------------------------------------
// CoreWebSocketBridge — adapts BrowserWebSocketAdapter to IWebSocket
// ---------------------------------------------------------------------------

/**
 * CoreWebSocketBridge adapts a raw WebSocket connection to the core's
 * callback-based {@link IWebSocket} interface.
 *
 * This class is used internally by {@link BrowserWebSocketFactory} to
 * create IWebSocket instances for the core XyncraClient.
 *
 * Features automatic reconnection with exponential backoff.
 */
class CoreWebSocketBridge implements IWebSocket {
  private ws: WebSocket | null = null;
  private msgHandler: ((data: string | Uint8Array) => void) | null = null;
  private closeHandler: ((code: number, reason: string) => void) | null = null;
  private errorHandler: ((error: Error) => void) | null = null;
  private openHandler: (() => void) | null = null;
  private reconnectionHandler: ((state: ReconnectionState) => void) | null = null;
  private readonly url: string;

  // Reconnection state
  private reconnectAttempt = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private countdownTimer: ReturnType<typeof setInterval> | null = null;
  private countdownSeconds = 0;
  private intentionalClose = false;
  private isReconnecting = false;

  constructor(url: string) {
    this.url = url;
    this.doConnect();
  }

  private doConnect(): void {
    try {
      this.ws = new WebSocket(this.url);
      this.ws.binaryType = 'arraybuffer';
    } catch {
      this.scheduleReconnect();
      return;
    }

    this.ws.onopen = () => {
      // Reset reconnection state on successful connection
      this.reconnectAttempt = 0;
      this.isReconnecting = false;
      this.notifyReconnectionState();
      if (this.openHandler) this.openHandler();
    };

    this.ws.onmessage = (event: MessageEvent) => {
      if (!this.msgHandler) return;
      if (event.data instanceof ArrayBuffer) {
        this.msgHandler(new Uint8Array(event.data));
      } else if (typeof event.data === 'string') {
        this.msgHandler(event.data);
      }
    };

    this.ws.onclose = (event: globalThis.CloseEvent) => {
      if (this.closeHandler) this.closeHandler(event.code, event.reason);
      if (!this.intentionalClose) {
        this.scheduleReconnect();
      }
    };

    this.ws.onerror = () => {
      if (this.errorHandler) this.errorHandler(new Error('WebSocket error'));
    };
  }

  private scheduleReconnect(): void {
    if (this.reconnectAttempt >= RECONNECT_MAX_RETRIES) {
      this.isReconnecting = false;
      this.notifyReconnectionState();
      return;
    }

    this.isReconnecting = true;
    const delay = Math.min(
      RECONNECT_BASE_DELAY * Math.pow(2, this.reconnectAttempt),
      RECONNECT_MAX_DELAY,
    );
    this.reconnectAttempt++;

    // Countdown for UI display
    this.countdownSeconds = Math.ceil(delay / 1000);
    this.notifyReconnectionState();

    if (this.countdownTimer) clearInterval(this.countdownTimer);
    this.countdownTimer = setInterval(() => {
      this.countdownSeconds = Math.max(0, this.countdownSeconds - 1);
      this.notifyReconnectionState();
    }, 1000);

    this.reconnectTimer = setTimeout(() => {
      if (this.countdownTimer) {
        clearInterval(this.countdownTimer);
        this.countdownTimer = null;
      }
      this.doConnect();
    }, delay);
  }

  private notifyReconnectionState(): void {
    if (this.reconnectionHandler) {
      this.reconnectionHandler({
        isReconnecting: this.isReconnecting,
        attempt: this.reconnectAttempt,
        maxRetries: RECONNECT_MAX_RETRIES,
        nextRetryIn: this.countdownSeconds,
      });
    }
  }

  /** Reset reconnection state and retry immediately. */
  reconnect(): void {
    this.clearTimers();
    this.reconnectAttempt = 0;
    this.intentionalClose = false;
    this.isReconnecting = true;
    this.notifyReconnectionState();
    this.doConnect();
  }

  private clearTimers(): void {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (this.countdownTimer) {
      clearInterval(this.countdownTimer);
      this.countdownTimer = null;
    }
  }

  send(data: string | Uint8Array): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      throw new Error('WebSocket is not open');
    }
    this.ws.send(
      data instanceof Uint8Array ? (data.buffer as ArrayBuffer) : data,
    );
  }

  close(code?: number, reason?: string): void {
    this.intentionalClose = true;
    this.clearTimers();
    this.isReconnecting = false;
    this.notifyReconnectionState();
    if (!this.ws) return;
    if (
      this.ws.readyState === WebSocket.CLOSED ||
      this.ws.readyState === WebSocket.CLOSING
    ) {
      return;
    }
    this.ws.close(code, reason);
  }

  onmessage(handler: (data: string | Uint8Array) => void): void {
    this.msgHandler = handler;
  }

  onclose(handler: (code: number, reason: string) => void): void {
    this.closeHandler = handler;
  }

  onerror(handler: (error: Error) => void): void {
    this.errorHandler = handler;
  }

  onopen(handler: () => void): void {
    this.openHandler = handler;
  }

  /** Register a handler for reconnection state changes. */
  onreconnection(handler: (state: ReconnectionState) => void): void {
    this.reconnectionHandler = handler;
  }

  get readyState(): number {
    return this.ws?.readyState ?? WebSocket.CLOSED;
  }
}

// ---------------------------------------------------------------------------
// BrowserWebSocketFactory — implements IWebSocketFactory from core
// ---------------------------------------------------------------------------

/**
 * BrowserWebSocketFactory creates IWebSocket instances for use with the
 * core XyncraClient. Each call to create() returns a new WebSocket
 * connection to the given URL.
 *
 * Implements {@link IWebSocketFactory} from `@xyncra/client-core`.
 *
 * Exposes the last created bridge for reconnection control.
 */
export class BrowserWebSocketFactory implements IWebSocketFactory {
  private lastBridge: CoreWebSocketBridge | null = null;

  create(url: string): IWebSocket {
    const bridge = new CoreWebSocketBridge(url);
    this.lastBridge = bridge;
    return bridge;
  }

  /** Get the last created bridge for reconnection control. */
  getLastBridge(): CoreWebSocketBridge | null {
    return this.lastBridge;
  }
}
