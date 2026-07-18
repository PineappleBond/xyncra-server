/**
 * @packageDocumentation
 * Function registration types — 1:1 mapping of Go pkg/protocol/function.go.
 */

/**
 * FunctionInfo describes a single callable function that a client device
 * exposes. It is the wire format used in system.register_functions requests.
 * JSON field names match the Go struct tags exactly.
 */
export interface FunctionInfo {
  /**
   * Unique function identifier within the device scope.
   * Must be non-empty and no longer than 255 characters.
   */
  name: string;

  /**
   * Optional human-readable summary of what the function does.
   * Stored as-is and returned verbatim on queries.
   */
  description?: string;

  /**
   * Optional JSON Schema (draft 7) describing the function's input parameters.
   * The server does not validate against this schema; it is stored for
   * consumers (e.g. agents) to interpret.
   */
  parameters?: Record<string, unknown>;

  /** Optional description of the function's return value. */
  returns?: ReturnInfo;

  /** Optional labels for filtering functions. */
  tags?: string[];

  /**
   * Optional per-function timeout in milliseconds.
   * If zero / omitted, the Agent's default call_timeout applies.
   */
  timeout_ms?: number;
}

/**
 * ReturnInfo describes a function's return value.
 * JSON field names match the Go struct tags exactly.
 */
export interface ReturnInfo {
  type: string;
  description?: string;
}
