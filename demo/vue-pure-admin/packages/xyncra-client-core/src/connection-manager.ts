/**
 * @packageDocumentation
 * WebSocket connection manager for the Xyncra TypeScript client.
 *
 * Manages the lifecycle of a single WebSocket connection to the Xyncra server,
 * including reading, writing, heartbeating (ping/pong), and reconnection with
 * exponential backoff. Mirrors Go's connectionManager (pkg/client/connection.go).
 *
 * Key constraints (must be strictly enforced):
 * - C1: Package.version is always forced to 1 in sendPackage()
 * - C2: 4001 close code = device replaced; set replaced=true, do NOT reconnect
 * - C3: Backoff algorithm: base * 2^(attempt-1), capped at max, exponent cap 30,
 *        +/-25% random jitter
 * - TS-D-002: No environment-specific imports; all WebSocket access via IWebSocket
 */

import type {
  Package,
  PackageDataRequest,
  PackageDataResponse,
  PackageDataUpdates,
} from '@xyncra/protocol';
import {
  DefaultBackoffJitterFraction,
  DefaultBackoffMaxExponent,
  DefaultMaxMessageSize,
  DefaultPingInterval,
  DefaultPongWait,
  DefaultReconnectBaseDelay,
  DefaultReconnectMaxDelay,
  DefaultSendBufSize,
  DefaultWriteWait,
} from './constants';
import { ConnectionError } from './errors';
import type { ILogger, IWebSocket, IWebSocketFactory } from './interfaces';

// ---------------------------------------------------------------------------
// ConnectionCallbacks
// ---------------------------------------------------------------------------

/**
 * Callbacks invoked by ConnectionManager at key lifecycle events.
 *
 * Mirrors Go's connectionCallbacks (pkg/client/connection.go).
 */
export interface ConnectionCallbacks {
  /** Called when a PackageTypeResponse is received from the server. */
  onResponse(response: PackageDataResponse): void;
  /** Called when a PackageTypeUpdates batch is received from the server. */
  onUpdates(updates: PackageDataUpdates): void;
  /** Called when a server-initiated PackageTypeRequest is received (D-092). */
  onRequest(request: PackageDataRequest): void;
  /** Called after a WebSocket connection has been successfully established. */
  onConnect(): void;
  /**
   * Called when an active WebSocket connection is lost unexpectedly.
   * The `replaced` parameter is true when the disconnect was caused by the
   * server sending a 4001 close frame (device replacement, D-095/D-111).
   */
  onDisconnect(replaced: boolean): void;
}

// ---------------------------------------------------------------------------
// ConnectionManagerOptions
// ---------------------------------------------------------------------------

/**
 * Configuration options for ConnectionManager.
 *
 * Required fields have no defaults and must be provided.
 * Optional fields have sensible defaults defined in constants.ts.
 */
export interface ConnectionManagerOptions {
  /** WebSocket server URL to connect to. */
  serverURL: string;
  /** User identifier for authentication (D-005). */
  userID: string;
  /** Device identifier for this client (D-033). */
  deviceID: string;
  /** Factory for creating WebSocket connections (TS-D-002). */
  wsFactory: IWebSocketFactory;
  /** Logger for diagnostic output. */
  logger: ILogger;
  /** Callbacks for lifecycle events. */
  callbacks: ConnectionCallbacks;

  // ---- Optional fields (defaults in constants.ts) ----

  /** Interval between pings (ms). Default: 54000. */
  pingInterval?: number;
  /** Maximum time to wait for a pong from the server (ms). Default: 60000. */
  pongWait?: number;
  /** Maximum duration allowed for a write to complete (ms). Default: 10000. */
  writeWait?: number;
  /** Capacity of the outbound send buffer. Default: 256. */
  sendBufSize?: number;
  /** Initial delay before the first reconnect attempt (ms). Default: 1000. */
  reconnectBaseDelay?: number;
  /** Maximum cap for exponential reconnect backoff (ms). Default: 30000. */
  reconnectMaxDelay?: number;
  /** Maximum inbound message size in bytes. Default: 65536. */
  maxMessageSize?: number;
}

// ---------------------------------------------------------------------------
// ConnectionManager
// ---------------------------------------------------------------------------

/**
 * ConnectionManager manages the lifecycle of a single WebSocket connection to
 * the Xyncra server, including reading, writing, heartbeating, and
 * reconnection with exponential backoff.
 *
 * The Go reference uses goroutine-based readPump/writePump. The TypeScript
 * implementation is event-driven: onmessage/onclose/onopen callbacks replace
 * goroutine loops. A JS single-threaded execution model means no Mutex is
 * needed (contrast Go's sync.Mutex).
 *
 * Concurrency model mapping:
 * | Go                    | TypeScript                     |
 * |-----------------------|--------------------------------|
 * | goroutine readPump    | onmessage event callback       |
 * | goroutine writePump   | sendBuffer + setTimeout (ping) |
 * | send chan []byte (256)| sendBuffer: Uint8Array[]       |
 * | disconnectCh chan     | disconnectPromise: Promise     |
 * | sync.Mutex            | Not needed (single-threaded)   |
 * | context.Context       | AbortSignal                    |
 * | time.Timer            | setTimeout / clearTimeout      |
 */
export class ConnectionManager {
  private ws: IWebSocket | null = null;
  private sendBuffer: string[] = [];
  private connected = false;
  private closing = false;
  private replaced = false;
  private attempt = 0;
  private pingTimer: ReturnType<typeof setTimeout> | null = null;
  private pongTimer: ReturnType<typeof setTimeout> | null = null;

  /**
   * Resolved when an unexpected disconnection occurs. Mirrors Go's
   * disconnectCh channel. Recreated after each successful (re)connect.
   */
  private disconnectPromise: Promise<void>;
  private disconnectResolve: (() => void) | null = null;

  /** Options with defaults applied. */
  private readonly opts: Required<
    Pick<
      ConnectionManagerOptions,
      | 'pingInterval'
      | 'pongWait'
      | 'writeWait'
      | 'sendBufSize'
      | 'reconnectBaseDelay'
      | 'reconnectMaxDelay'
      | 'maxMessageSize'
    >
  > &
    ConnectionManagerOptions;

  constructor(options: ConnectionManagerOptions) {
    // Merge user options with defaults for all tunable parameters.
    this.opts = {
      ...options,
      pingInterval: options.pingInterval ?? DefaultPingInterval,
      pongWait: options.pongWait ?? DefaultPongWait,
      writeWait: options.writeWait ?? DefaultWriteWait,
      sendBufSize: options.sendBufSize ?? DefaultSendBufSize,
      reconnectBaseDelay:
        options.reconnectBaseDelay ?? DefaultReconnectBaseDelay,
      reconnectMaxDelay: options.reconnectMaxDelay ?? DefaultReconnectMaxDelay,
      maxMessageSize: options.maxMessageSize ?? DefaultMaxMessageSize,
    };

    // Initialize the disconnect Promise (mirrors Go's disconnectCh).
    this.disconnectPromise = this.createDisconnectPromise();
  }

  // ---------------------------------------------------------------------------
  // Public API
  // ---------------------------------------------------------------------------

  /**
   * Establishes a WebSocket connection to the server.
   *
   * Constructs the URL with user_id and device_id query parameters (D-005),
   * creates a WebSocket via the injected factory, and waits for the connection
   * to open. On success, sets up the read pump (event handlers) and starts the
   * ping timer.
   *
   * @param abortSignal - Optional AbortSignal to cancel the connection attempt.
   * @throws {ConnectionError} If the WebSocket connection fails.
   */
  async connect(abortSignal?: AbortSignal): Promise<void> {
    if (this.connected) return;
    if (this.replaced) {
      throw new ConnectionError('Device replaced, cannot connect');
    }

    // Build URL with query parameters (mirrors Go's url.Parse + q.Set pattern).
    const url = buildConnectionURL(
      this.opts.serverURL,
      this.opts.userID,
      this.opts.deviceID,
    );

    this.opts.logger.info('connecting', { url: this.opts.serverURL });

    // Clean up any previous WebSocket before creating a new one.
    this.cleanupWebSocket();

    const ws = this.opts.wsFactory.create(url);
    this.ws = ws;

    return new Promise<void>((resolve, reject) => {
      let settled = false;

      ws.onopen(() => {
        if (settled) return;
        settled = true;

        this.connected = true;
        this.closing = false;
        this.replaced = false;
        this.attempt = 0;

        // Recreate the disconnect Promise for this session (mirrors Go:
        // "A new channel is created after each successful (re)connect").
        this.disconnectPromise = this.createDisconnectPromise();

        // Set up event-driven read pump + start ping timer.
        this.setupReadPump();
        this.schedulePing();

        this.opts.callbacks.onConnect();
        this.opts.logger.info('connected', { url: this.opts.serverURL });
        resolve();
      });

      ws.onerror((error: Error) => {
        if (settled) return;
        settled = true;
        reject(new ConnectionError('Failed to connect to WebSocket', error));
      });

      if (abortSignal) {
        if (abortSignal.aborted) {
          if (!settled) {
            settled = true;
            ws.close(1000, 'aborted');
            reject(new ConnectionError('Connection aborted'));
          }
          return;
        }
        abortSignal.addEventListener(
          'abort',
          () => {
            if (settled) return;
            settled = true;
            ws.close(1000, 'aborted');
            reject(new ConnectionError('Connection aborted'));
          },
          { once: true },
        );
      }
    });
  }

  /**
   * Enqueues a protocol Package for asynchronous delivery.
   *
   * C1: Package.version is always forced to 1 before serialization.
   * Non-blocking: if the send buffer is full, the message is dropped with a
   * warning log (mirrors Go's non-blocking `select { case send <- data: default: }`).
   *
   * @param pkg - The protocol Package to send.
   */
  sendPackage(pkg: Package): void {
    // C1: Package.Version is always set to 1 before marshalling.
    pkg.version = 1;

    if (this.closing || !this.connected || !this.ws) {
      this.opts.logger.warn('sendPackage: not connected, rejecting message');
      throw new ConnectionError('Cannot send package: not connected');
    }

    // Log the method being sent
    const data = pkg.data as any;
    if (data?.method) {
      this.opts.logger.info('sendPackage: sending', { method: data.method });
    }

    // Non-blocking: if buffer full, drop + warn log (mirrors Go default: branch).
    if (this.sendBuffer.length >= this.opts.sendBufSize) {
      this.opts.logger.warn('Send buffer full, dropping message');
      return;
    }

    // Send as text JSON for easier debugging (server supports both text and binary)
    const jsonData = JSON.stringify(pkg);
    this.sendBuffer.push(jsonData);
    this.flushSendBuffer();
  }

  /**
   * Closes the current connection (if any) and attempts to establish a new one
   * with exponential backoff. Loops until connected or aborted.
   *
   * The attempt counter is incremented before each attempt and reset to 0 on
   * successful connection. The backoff delay is computed by backoffDelay().
   *
   * @param abortSignal - Optional AbortSignal to cancel the reconnect loop.
   * @throws {ConnectionError} If the device has been replaced (4001).
   * @throws {ConnectionError} If the reconnect was aborted.
   */
  async reconnect(abortSignal?: AbortSignal): Promise<void> {
    if (this.replaced) {
      throw new ConnectionError('Device replaced, cannot reconnect');
    }

    while (!abortSignal?.aborted) {
      this.attempt++;
      const delay = backoffDelay(
        this.attempt,
        this.opts.reconnectBaseDelay,
        this.opts.reconnectMaxDelay,
      );

      this.opts.logger.info('reconnecting', {
        attempt: this.attempt,
        delay,
      });

      // Honour abort during the backoff wait (mirrors Go's select on ctx.Done).
      await abortableSleep(delay, abortSignal);
      if (abortSignal?.aborted) break;

      // Close the previous connection before reconnecting.
      this.cleanupWebSocket();
      this.connected = false;

      try {
        await this.connect(abortSignal);
        return; // Connection succeeded.
      } catch (error) {
        this.opts.logger.warn(
          `Reconnect attempt ${this.attempt} failed`,
          error,
        );
        // Continue the loop: try again after backoff.
      }
    }

    throw new ConnectionError('Reconnect aborted');
  }

  /**
   * Shuts down the connection manager. Idempotent: calling close() more than
   * once has no additional effect.
   *
   * Clears all timers, closes the WebSocket, marks the connection as closed,
   * and drains the send buffer. The onDisconnect callback is NOT invoked when
   * close() is called explicitly (mirrors Go: `if wasConnected && !isClosing`).
   */
  close(): void {
    if (this.closing) return;
    this.closing = true;

    this.clearTimers();

    if (this.ws) {
      this.ws.close(1000, 'client closing');
    }

    this.connected = false;
    this.sendBuffer = [];

    this.opts.logger.info('connection manager closed');
  }

  /** Reports whether a WebSocket connection is currently active. */
  isConnected(): boolean {
    return this.connected;
  }

  /** Returns the current reconnect attempt counter. Reset to 0 on success. */
  getAttempt(): number {
    return this.attempt;
  }

  /** Returns the device identifier used by this connection manager. */
  getDeviceID(): string {
    return this.opts.deviceID;
  }

  /**
   * Reports whether this connection was replaced by a newer one from the same
   * device (server sent 4001 close frame). When true, the caller should NOT
   * attempt to reconnect (D-095, D-111).
   */
  isReplaced(): boolean {
    return this.replaced;
  }

  /**
   * Returns a Promise that resolves when an unexpected disconnection occurs.
   * Mirrors Go's `Disconnected() <-chan struct{}` channel.
   *
   * A new Promise is created after each successful (re)connect.
   */
  disconnected(): Promise<void> {
    return this.disconnectPromise;
  }

  // ---------------------------------------------------------------------------
  // Read pump (event-driven, replaces Go goroutine loop)
  // ---------------------------------------------------------------------------

  /**
   * Sets up the event-driven read pump by registering onmessage, onclose, and
   * onerror handlers on the current WebSocket.
   *
   * Go's readPump runs in a goroutine loop calling conn.ReadMessage(). The
   * TypeScript version uses onmessage callbacks instead.
   */
  private setupReadPump(): void {
    const ws = this.ws;
    if (!ws) return;

    ws.onmessage((data: string | Uint8Array) => {
      // Check message size against maxMessageSize (mirrors Go's SetReadLimit).
      const size =
        typeof data === 'string'
          ? new TextEncoder().encode(data).length
          : data.length;
      if (size > this.opts.maxMessageSize) {
        this.opts.logger.warn('message exceeds max size, dropping', {
          size,
          maxSize: this.opts.maxMessageSize,
        });
        return;
      }

      try {
        const text =
          typeof data === 'string' ? data : new TextDecoder().decode(data);
        const pkg = JSON.parse(text) as Package;
        this.dispatchPackage(pkg);
      } catch (error) {
        this.opts.logger.error('Failed to parse message', error);
      }
    });

    ws.onclose((code: number, reason: string) => {
      this.handleConnectionClose(code, reason);
    });

    ws.onerror((error: Error) => {
      this.opts.logger.error('WebSocket error', error);
    });
  }

  /**
   * Handles WebSocket close events. Updates state, detects 4001 (device
   * replacement), and notifies via the disconnect Promise and callback.
   *
   * Mirrors Go's handleDisconnect() method.
   */
  private handleConnectionClose(code: number, reason: string): void {
    this.clearTimers();

    const wasConnected = this.connected;
    const wasClosing = this.closing;
    this.connected = false;

    // C2: 4001 close code = device replaced.
    if (code === 4001) {
      this.replaced = true;
      this.opts.logger.warn('Device replaced (4001 close code)', { reason });
    }

    // Log unexpected close errors (mirrors Go's IsUnexpectedCloseError check).
    if (
      !wasClosing &&
      code !== 1000 && // Normal closure
      code !== 1001 && // Going away
      code !== 4001 // Device replaced (expected from server)
    ) {
      this.opts.logger.error('Unexpected WebSocket close', { code, reason });
    }

    // Signal disconnect to listeners (mirrors Go's close(disconnectCh)).
    // Only signal if the connection was active and this was not a clean close.
    if (wasConnected && !wasClosing) {
      this.resolveDisconnect();
      this.opts.callbacks.onDisconnect(this.replaced);
    }
  }

  /**
   * Dispatches a parsed Package to the appropriate callback based on its type.
   *
   * Mirrors Go's readPump switch statement. Also handles protocol-level pong
   * messages to reset the ping timer.
   */
  private dispatchPackage(pkg: Package): void {
    switch (pkg.type) {
      case 1: {
        // PackageTypeResponse
        this.opts.callbacks.onResponse(pkg.data as PackageDataResponse);
        break;
      }
      case 2: {
        // PackageTypeUpdates
        const updatesPkg = pkg.data as PackageDataUpdates;
        this.opts.callbacks.onUpdates(updatesPkg);
        break;
      }
      case 0: {
        // PackageTypeRequest (includes server-initiated requests, D-092)
        this.opts.callbacks.onRequest(pkg.data as PackageDataRequest);
        break;
      }
      default: {
        // Check for protocol-level pong (not in PackageType enum).
        // The server may send { type: 'pong', version: 1, data: null }.
        const pkgType = pkg.type as unknown;
        if (pkgType === 'pong') {
          this.clearPongTimer();
          // Reschedule ping after receiving pong.
          this.schedulePing();
        } else {
          this.opts.logger.warn('Unknown package type', { type: pkg.type });
        }
        break;
      }
    }
  }

  // ---------------------------------------------------------------------------
  // Ping/Pong (replaces Go's writePump ticker + native WS ping frames)
  // ---------------------------------------------------------------------------

  /**
   * Schedules a heartbeat check.
   *
   * Note: The Go server uses WebSocket protocol-level ping/pong frames
   * (websocket.PingMessage), not application-level messages. The browser
   * WebSocket API handles pong responses automatically.
   *
   * Application-level heartbeats are handled by XyncraClient.heartbeatLoop()
   * which sends heartbeat RPCs. This method is kept for potential future use
   * but currently does nothing to avoid sending invalid protocol messages.
   */
  private schedulePing(): void {
    // Intentionally empty: WebSocket-level ping/pong is handled by the browser,
    // and application-level heartbeats are sent by XyncraClient.heartbeatLoop().
    // See: https://github.com/PineappleBond/xyncra-server/issues/XXX
  }

  /**
   * Schedules a pong timeout. If no pong is received within pongWait ms,
   * the connection is considered dead and closed.
   *
   * Mirrors Go's writePump: `conn.SetWriteDeadline` + PingMessage failure
   * causes the pump to exit. In TS, we close the connection explicitly.
   */
  private schedulePong(): void {
    this.clearPongTimer();

    this.pongTimer = setTimeout(() => {
      this.opts.logger.warn('Pong timeout, closing connection');
      this.ws?.close(1000, 'pong timeout');
    }, this.opts.pongWait);
  }

  // ---------------------------------------------------------------------------
  // Send buffer flush
  // ---------------------------------------------------------------------------

  /**
   * Attempts to flush the send buffer by writing all buffered messages to the
   * WebSocket. Stops on the first message that cannot be sent (connection lost).
   *
   * Go writes from the send channel in writePump. TS flushes synchronously
   * after each sendPackage() call (JS single-threaded, no separate pump).
   */
  private flushSendBuffer(): void {
    if (!this.connected || !this.ws) return;

    while (this.sendBuffer.length > 0) {
      const data = this.sendBuffer[0];
      try {
        this.ws.send(data);
        this.sendBuffer.shift();
      } catch (error) {
        this.opts.logger.error('Failed to send message', error);
        // Stop flushing; remaining messages stay in the buffer.
        break;
      }
    }
  }

  // ---------------------------------------------------------------------------
  // Backoff algorithm (C3)
  // ---------------------------------------------------------------------------

  // ---------------------------------------------------------------------------
  // Timer management
  // ---------------------------------------------------------------------------

  /** Clears both ping and pong timers. */
  private clearTimers(): void {
    this.clearPingTimer();
    this.clearPongTimer();
  }

  private clearPingTimer(): void {
    if (this.pingTimer !== null) {
      clearTimeout(this.pingTimer);
      this.pingTimer = null;
    }
  }

  private clearPongTimer(): void {
    if (this.pongTimer !== null) {
      clearTimeout(this.pongTimer);
      this.pongTimer = null;
    }
  }

  // ---------------------------------------------------------------------------
  // Disconnect Promise management
  // ---------------------------------------------------------------------------

  /** Creates a new disconnect Promise (called after each successful connect). */
  private createDisconnectPromise(): Promise<void> {
    this.disconnectResolve = null;
    return new Promise<void>((resolve) => {
      this.disconnectResolve = resolve;
    });
  }

  /** Resolves the current disconnect Promise (if not already resolved). */
  private resolveDisconnect(): void {
    if (this.disconnectResolve) {
      this.disconnectResolve();
      this.disconnectResolve = null;
    }
  }

  // ---------------------------------------------------------------------------
  // WebSocket cleanup
  // ---------------------------------------------------------------------------

  /**
   * Cleans up the current WebSocket by closing it and removing all handlers.
   * Called before creating a new WebSocket in connect() or reconnect().
   */
  private cleanupWebSocket(): void {
    if (this.ws) {
      try {
        this.ws.close(1000, 'reconnecting');
      } catch {
        // Ignore errors from closing a stale WebSocket.
      }
      // Remove handlers to prevent stale callbacks from firing.
      this.ws.onmessage(() => {});
      this.ws.onclose(() => {});
      this.ws.onerror(() => {});
      this.ws.onopen(() => {});
      this.ws = null;
    }
  }
}

// ---------------------------------------------------------------------------
// Module-level helper functions
// ---------------------------------------------------------------------------

/**
 * Computes the delay for a given reconnect attempt using exponential backoff.
 *
 * C3: Algorithm: base * 2^(attempt-1), capped at max, exponent cap 30,
 * +/-25% random jitter.
 *
 * Mirrors Go's backoffDelay() function (pkg/client/connection.go:531-551).
 *
 * @param attempt - The reconnect attempt number (should be >= 1).
 * @param base - The base delay in milliseconds.
 * @param max - The maximum delay cap in milliseconds.
 * @returns The computed delay in milliseconds (always > 0).
 */
export function backoffDelay(
  attempt: number,
  base: number,
  max: number,
): number {
  let exp = attempt - 1;
  if (exp < 0) exp = 0;
  // Guard against overflow for very large attempt numbers (C3: exponent cap 30).
  if (exp > DefaultBackoffMaxExponent) exp = DefaultBackoffMaxExponent;

  let delay = base * 2 ** exp;
  // Cap at max; also guard against NaN/Infinity.
  if (delay > max || delay <= 0 || !Number.isFinite(delay)) {
    delay = max;
  }

  // Jitter: +/-25% of delay (C3: +-25% random jitter).
  // jitterRange = delay * 0.5 (25% * 2), jitter in [-delay*0.25, +delay*0.25).
  const jitterRange = delay * DefaultBackoffJitterFraction * 2;
  if (jitterRange > 0) {
    const jitter =
      Math.random() * jitterRange - delay * DefaultBackoffJitterFraction;
    delay += jitter;
  }

  // Ensure delay is always positive.
  if (delay <= 0) delay = 1;

  return delay;
}

/**
 * Builds the WebSocket connection URL with user_id and device_id query params.
 *
 * Mirrors Go's url.Parse + q.Set pattern (pkg/client/connection.go:139-146).
 */
function buildConnectionURL(
  serverURL: string,
  userID: string,
  deviceID: string,
): string {
  const separator = serverURL.includes('?') ? '&' : '?';
  return `${serverURL}${separator}user_id=${encodeURIComponent(userID)}&device_id=${encodeURIComponent(deviceID)}`;
}

/**
 * Sleeps for the specified duration, or resolves immediately if the abort
 * signal is triggered. Mirrors Go's select { case <-ctx.Done(): case <-timer.C: }.
 */
function abortableSleep(ms: number, abortSignal?: AbortSignal): Promise<void> {
  return new Promise<void>((resolve) => {
    if (abortSignal?.aborted) {
      resolve();
      return;
    }

    const timer = setTimeout(() => {
      if (abortSignal) {
        abortSignal.removeEventListener('abort', onAbort);
      }
      resolve();
    }, ms);

    const onAbort = () => {
      clearTimeout(timer);
      resolve();
    };

    if (abortSignal) {
      abortSignal.addEventListener('abort', onAbort, { once: true });
    }
  });
}
