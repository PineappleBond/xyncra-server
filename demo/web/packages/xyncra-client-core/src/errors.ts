/**
 * @packageDocumentation
 * Error types and codes for the Xyncra TypeScript client.
 *
 * Mirrors Go client layer error codes (pkg/client/options.go) and
 * store layer sentinel errors (pkg/store/errors.go).
 */

// ---------------------------------------------------------------------------
// Client error codes (D-027)
// ---------------------------------------------------------------------------

/** Indicates a WebSocket connection failure. */
export const ErrorCodeConnectionError = -400 as const;
/** Indicates an operation exceeded its deadline. */
export const ErrorCodeTimeoutError = -401 as const;
/** Indicates a data synchronisation failure. */
export const ErrorCodeSyncError = -402 as const;

// ---------------------------------------------------------------------------
// ClientError hierarchy
// ---------------------------------------------------------------------------

/**
 * ClientError represents an error returned by the client layer, carrying one of
 * the extension error codes defined in D-027.
 *
 * Uses ES2022 Error.cause for error chaining (mirrors Go Unwrap).
 */
export class ClientError extends Error {
  public readonly code: number;

  constructor(message: string, code: number, cause?: Error) {
    super(message, cause !== undefined ? { cause } : undefined);
    this.name = 'ClientError';
    this.code = code;
  }

  /**
   * Returns the underlying cause as an Error, or undefined.
   * Mirrors Go's (*ClientError).Unwrap().
   */
  unwrap(): Error | undefined {
    return this.cause as Error | undefined;
  }
}

/**
 * ConnectionError indicates a WebSocket connection failure (code -400).
 */
export class ConnectionError extends ClientError {
  constructor(message: string, cause?: Error) {
    super(message, ErrorCodeConnectionError, cause);
    this.name = 'ConnectionError';
  }
}

/**
 * TimeoutError indicates an operation exceeded its deadline (code -401).
 */
export class TimeoutError extends ClientError {
  constructor(message: string, cause?: Error) {
    super(message, ErrorCodeTimeoutError, cause);
    this.name = 'TimeoutError';
  }
}

/**
 * SyncError indicates a data synchronisation failure (code -402).
 */
export class SyncError extends ClientError {
  constructor(message: string, cause?: Error) {
    super(message, ErrorCodeSyncError, cause);
    this.name = 'SyncError';
  }
}

// ---------------------------------------------------------------------------
// Store layer sentinel errors (mirrors Go pkg/store/errors.go)
// ---------------------------------------------------------------------------

/** The requested resource was not found. */
export const ErrNotFound = new Error('not found');
/** A uniqueness constraint was violated. */
export const ErrDuplicateKey = new Error('duplicate key');
/** A foreign key constraint was violated. */
export const ErrForeignKeyViolation = new Error('foreign key violation');
/** The database connection could not be established. */
export const ErrConnectionFailed = new Error('connection failed');
/** The operation exceeded its context deadline. */
export const ErrContextDeadlineExceeded = new Error(
  'context deadline exceeded',
);
/** The database is locked by another writer. */
export const ErrDatabaseLocked = new Error('database locked');

// ---------------------------------------------------------------------------
// Factory helpers (mirror Go NewConnectionError / NewTimeoutError / NewSyncError)
// ---------------------------------------------------------------------------

/** Creates a ConnectionError (-400) wrapping an optional cause. */
export function newConnectionError(
  message: string,
  cause?: Error,
): ConnectionError {
  return new ConnectionError(message, cause);
}

/** Creates a TimeoutError (-401) wrapping an optional cause. */
export function newTimeoutError(message: string, cause?: Error): TimeoutError {
  return new TimeoutError(message, cause);
}

/** Creates a SyncError (-402) wrapping an optional cause. */
export function newSyncError(message: string, cause?: Error): SyncError {
  return new SyncError(message, cause);
}
