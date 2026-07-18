/**
 * @packageDocumentation
 * Error codes and HandlerError — 1:1 mapping of Go pkg/protocol/errors.go.
 */

import { ResponseCode } from './package';

// Re-export ResponseCode for convenience so consumers can import from either module.
export { ResponseCode };

/**
 * Error code segments (mirrors Go errors.go):
 *
 *   -100 to -199: client errors (validation, not_found, duplicate)
 *   -200 to -299: permission errors (permission_denied, forbidden)
 *   -300 to -399: server errors (internal, unavailable)
 *
 * The base ResponseCode.OK (0) and ResponseCode.Error (-1) are defined in package.ts.
 * Concrete error codes below extend that set.
 */
export const ErrorCode = {
  // Client errors (-100s)
  /** Parameter validation failed. */
  ValidationError: -100,
  /** Requested resource was not found. */
  NotFound: -101,
  /** Resource already exists (duplicate). */
  Duplicate: -102,

  // Permission errors (-200s)
  /** Caller lacks the required permission. */
  PermissionDenied: -200,
  /** Access to the resource is forbidden. */
  Forbidden: -201,

  // Server errors (-300s)
  /** Internal server error. */
  InternalError: -300,
  /** Service is temporarily unavailable. */
  Unavailable: -301,
} as const;

export type ErrorCode = (typeof ErrorCode)[keyof typeof ErrorCode];

/**
 * Returns a human-readable name for a ResponseCode value.
 */
export function responseCodeName(code: ResponseCode): string {
  switch (code) {
    case ResponseCode.OK:
      return 'OK';
    case ResponseCode.Error:
      return 'Error';
    case ErrorCode.ValidationError:
      return 'ValidationError';
    case ErrorCode.NotFound:
      return 'NotFound';
    case ErrorCode.Duplicate:
      return 'Duplicate';
    case ErrorCode.PermissionDenied:
      return 'PermissionDenied';
    case ErrorCode.Forbidden:
      return 'Forbidden';
    case ErrorCode.InternalError:
      return 'InternalError';
    case ErrorCode.Unavailable:
      return 'Unavailable';
    default:
      return `Unknown(${code})`;
  }
}

/**
 * HandlerError is a typed error that carries a ResponseCode.
 * Handlers return HandlerError to communicate structured errors to clients.
 *
 * Uses ES2022 Error.cause for error chaining.
 */
export class HandlerError extends Error {
  public readonly code: ResponseCode;

  constructor(
    code: ResponseCode,
    message: string,
    options?: { cause?: unknown },
  ) {
    super(message, options);
    this.name = 'HandlerError';
    this.code = code;
    // Explicitly set cause to undefined when not provided (aligns with Go Unwrap returning nil).
    if (options?.cause === undefined) {
      this.cause = undefined;
    }
  }

  /**
   * Returns the underlying cause as an Error, or undefined if no cause was provided.
   * Mirrors Go's (*HandlerError).Unwrap().
   */
  unwrap(): Error | undefined {
    return this.cause as Error | undefined;
  }
}

/**
 * Creates a HandlerError with the given code and message.
 * Mirrors Go NewHandlerError(code, message).
 */
export function newHandlerError(
  code: ResponseCode,
  message: string,
  cause?: unknown,
): HandlerError {
  return new HandlerError(
    code,
    message,
    cause !== undefined ? { cause } : undefined,
  );
}

/**
 * Wraps an existing error as a HandlerError with the given code.
 * Mirrors Go WrapError(code, err).
 */
export function wrapError(code: ResponseCode, err: unknown): HandlerError {
  const message = err instanceof Error ? err.message : String(err);
  return new HandlerError(code, message, { cause: err });
}

/** Creates a -100 error for parameter validation failures. */
export function newValidationError(
  message: string,
  cause?: unknown,
): HandlerError {
  return newHandlerError(ErrorCode.ValidationError, message, cause);
}

/** Creates a -101 error for missing resources. */
export function newNotFoundError(
  message: string,
  cause?: unknown,
): HandlerError {
  return newHandlerError(ErrorCode.NotFound, message, cause);
}

/** Creates a -102 error for duplicate resources. */
export function newDuplicateError(
  message: string,
  cause?: unknown,
): HandlerError {
  return newHandlerError(ErrorCode.Duplicate, message, cause);
}

/** Creates a -200 error for authorization failures. */
export function newPermissionDeniedError(
  message: string,
  cause?: unknown,
): HandlerError {
  return newHandlerError(ErrorCode.PermissionDenied, message, cause);
}

/** Wraps an unexpected error as a -300 internal error. */
export function newInternalError(err: unknown): HandlerError {
  return wrapError(ErrorCode.InternalError, err);
}
