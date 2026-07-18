/**
 * @packageDocumentation
 * ClientOptions interface for the Xyncra TypeScript client.
 *
 * Mirrors Go's clientOptions struct (pkg/client/options.go).
 * Uses constructor injection for all environment dependencies (TS-D-002).
 */

import type { FunctionInfo } from '@xyncra/protocol';
import type {
  IIndexedDBProvider,
  ILogger,
  IUpdateHandler,
  IWebSocketFactory,
} from './interfaces';

/**
 * ClientOptions holds the full set of configuration values for a XyncraClient.
 *
 * Required fields have no defaults and must be provided.
 * Optional fields have sensible defaults defined in constants.ts.
 */
export interface ClientOptions {
  // ---- Required fields (no defaults) ----

  /** WebSocket server URL to connect to. */
  serverURL: string;

  /** User identifier for authentication (D-005). */
  userID: string;

  /** Device identifier for this client. */
  deviceID: string;

  /**
   * IndexedDB database name (TS-D-012: --db-path semantics changed to
   * IndexedDB database name instead of a file path).
   * If omitted, a default name is derived from userID + deviceID.
   */
  dbPath?: string;

  /** Factory for creating WebSocket connections (TS-D-002). */
  wsFactory: IWebSocketFactory;

  /** IndexedDB provider for local persistence (TS-D-002). */
  idbProvider: IIndexedDBProvider;

  /** Logger for diagnostic output. */
  logger: ILogger;

  /** Handler that receives processed data updates from the sync pipeline. */
  updateHandler: IUpdateHandler;

  // ---- Optional fields (defaults in constants.ts) ----

  /** Interval between heartbeat pings (ms). Default: 30000. */
  heartbeatInterval?: number;

  /** Initial delay before the first reconnect attempt (ms). Default: 1000. */
  reconnectBaseDelay?: number;

  /** Maximum cap for exponential reconnect backoff (ms). Default: 30000. */
  reconnectMaxDelay?: number;

  /** Maximum duration for a single RPC call (ms). Default: 30000. */
  rpcTimeout?: number;

  /** Interval between pings (ms). Default: 54000. */
  pingInterval?: number;

  /** Maximum time to wait for a pong from the server (ms). Default: 60000. */
  pongWait?: number;

  /** Maximum duration allowed for a write to complete (ms). Default: 10000. */
  writeWait?: number;

  /** Capacity of the outbound message channel. Default: 256. */
  sendBufSize?: number;

  /** Number of records fetched per sync pull batch. Default: 100. */
  syncBatchSize?: number;

  /** Debounce window for coalescing pull requests (ms). Default: 500. */
  syncRetryInterval?: number;

  /** Polling interval used during retry backoff (ms). Default: 1000. */
  retryPollInterval?: number;

  /** Maximum number of idempotency keys to cache. Default: 1024. */
  idempotencyCacheSize?: number;

  /** Number of RTT samples in the sliding window. Default: 50. */
  rttSamples?: number;

  /** Debounce interval for pull requests (ms). Default: 500. */
  debounceInterval?: number;

  /** Maximum inbound message size in bytes. Default: 65536. */
  maxMessageSize?: number;

  // ---- Advanced / rarely-tuned options ----

  /**
   * Device metadata sent with function registration.
   * Mirrors Go clientOptions.deviceInfo.
   */
  deviceInfo?: Record<string, string>;

  /**
   * Functions to auto-register on connect/reconnect.
   * Mirrors Go clientOptions.functions.
   */
  functions?: FunctionInfo[];
}
