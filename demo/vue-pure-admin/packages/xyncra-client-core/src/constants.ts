/**
 * @packageDocumentation
 * Default configuration constants for the Xyncra TypeScript client.
 *
 * Mirrors Go pkg/client/options.go default values.
 * All durations are in milliseconds (TypeScript convention),
 * unlike Go where they are time.Duration (nanoseconds).
 */

// ---------------------------------------------------------------------------
// Connection defaults
// ---------------------------------------------------------------------------

/** WebSocket endpoint used when none is supplied. */
export const DefaultServerURL = 'ws://localhost:8080/ws';

/** Maximum duration allowed for a write to complete (ms). */
export const DefaultWriteWait = 10_000;

/** Maximum time to wait for a pong from the server (ms). */
export const DefaultPongWait = 60_000;

/**
 * Interval between pings; must be less than PongWait.
 * Computed as (PongWait * 9) / 10.
 */
export const DefaultPingInterval = 54_000; // (PongWait * 9) / 10

// ---------------------------------------------------------------------------
// Heartbeat defaults
// ---------------------------------------------------------------------------

/** Interval between heartbeat pings (ms). */
export const DefaultHeartbeatInterval = 30_000;

/**
 * Minimum allowed heartbeat interval (ms).
 * Values below this are clamped to prevent heartbeat flooding (BUG-001).
 */
export const MinHeartbeatInterval = 5_000;

// ---------------------------------------------------------------------------
// RPC defaults
// ---------------------------------------------------------------------------

/** Default timeout for a single RPC call (ms). */
export const DefaultRPCTimeout = 30_000;

// ---------------------------------------------------------------------------
// Reconnect / backoff defaults
// ---------------------------------------------------------------------------

/** Initial delay before the first reconnect attempt (ms). */
export const DefaultReconnectBaseDelay = 1_000;

/** Maximum cap for exponential reconnect backoff (ms). */
export const DefaultReconnectMaxDelay = 30_000;

/**
 * Maximum exponent for backoff delay calculation.
 * Prevents overflow when computing base * 2^exp.
 * Mirrors Go connection.go: `if exp > 30 { exp = 30 }`.
 */
export const DefaultBackoffMaxExponent = 30;

/**
 * Jitter fraction for backoff calculations.
 * Applied as +/- 25% random jitter around the computed delay.
 * Mirrors Go connection.go: `delay/4` jitter range.
 */
export const DefaultBackoffJitterFraction = 0.25;

// ---------------------------------------------------------------------------
// Sync defaults
// ---------------------------------------------------------------------------

/** Number of records fetched per sync pull batch. */
export const DefaultSyncBatchSize = 100;

/** Debounce window for coalescing pull requests (ms). */
export const DefaultSyncRetryInterval = 500;

// ---------------------------------------------------------------------------
// Retry defaults
// ---------------------------------------------------------------------------

/** Initial delay before the first RPC retry attempt (ms). */
export const DefaultRetryBaseDelay = 1_000;

/** Maximum number of RPC retry attempts. */
export const DefaultRetryMaxAttempts = 5;

/** Polling interval used during retry backoff (ms). */
export const DefaultRetryPollInterval = 1_000;

// ---------------------------------------------------------------------------
// Outbound queue defaults
// ---------------------------------------------------------------------------

/** Capacity of the outbound message channel. */
export const DefaultSendBufSize = 256;

/** Maximum inbound message size in bytes. */
export const DefaultMaxMessageSize = 64 * 1024;

// ---------------------------------------------------------------------------
// Idempotency defaults
// ---------------------------------------------------------------------------

/** Maximum number of idempotency keys to cache. */
export const DefaultIdempotencyCacheSize = 1024;

// ---------------------------------------------------------------------------
// RTT / adaptive timeout defaults
// ---------------------------------------------------------------------------

/** Number of RTT samples in the sliding window. */
export const DefaultRTTSamples = 50;

/**
 * Minimum number of RTT samples required before adaptive timeout kicks in.
 * Below this count, the default RPC timeout is used.
 */
export const DefaultRTTMinSamples = 5;

/** Minimum adaptive timeout (ms). */
export const DefaultAdaptiveTimeoutMin = 5_000;

/** Maximum adaptive timeout (ms). */
export const DefaultAdaptiveTimeoutMax = 120_000;

/**
 * Multiplier applied to SRTT when computing adaptive timeout.
 * timeout = SRTT * 3 / 2
 */
export const DefaultAdaptiveTimeoutMultiplier = 1.5;

// ---------------------------------------------------------------------------
// Response retry defaults
// ---------------------------------------------------------------------------

/** Maximum size of the response retry queue. */
export const DefaultResponseRetryMaxSize = 100;

/** Maximum retry attempts per response. */
export const DefaultResponseRetryMax = 3;
