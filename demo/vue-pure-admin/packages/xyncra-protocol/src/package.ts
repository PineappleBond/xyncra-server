/**
 * @packageDocumentation
 * WebSocket protocol package types — 1:1 mapping of Go pkg/protocol/protocol.go.
 */

/**
 * PackageType identifies the kind of a WebSocket protocol Package.
 * Mirrors Go PackageType (uint8).
 */
export const PackageType = {
  /** Client-initiated request. */
  Request: 0,
  /** Server response to a request. */
  Response: 1,
  /** Push notification with data updates. */
  Updates: 2,
} as const;

export type PackageType = (typeof PackageType)[keyof typeof PackageType];

/**
 * Returns a human-readable name for a PackageType value.
 */
export function packageTypeName(type: PackageType): string {
  switch (type) {
    case PackageType.Request:
      return 'Request';
    case PackageType.Response:
      return 'Response';
    case PackageType.Updates:
      return 'Updates';
    default:
      return `Unknown(${type})`;
  }
}

/**
 * Update type constants for PackageDataUpdate.Type.
 * String values match the Go wire format exactly.
 */
export const UpdateType = {
  /** New message notification. */
  Message: 'message',
  /** Message deletion. */
  DeleteMessage: 'delete_message',
  /** Read cursor update. */
  MarkRead: 'mark_read',
  /** Conversation state change (delete/restore). */
  Conversation: 'conversation',
  /** Synthetic gap filler (runtime only, never persisted). */
  Gap: 'gap',
  /** Ephemeral: Seq=0, never persisted, never pulled. */
  Typing: 'typing',
  /** Ephemeral: Seq=0, cumulative text streaming. */
  Streaming: 'streaming',
  /** Ephemeral: Seq=0, agent status (thinking/tool_calling/generating/idle/asking_user). */
  AgentStatus: 'agent_status',
  /** Ephemeral: Seq=0, agent execution timed out. */
  AgentTimeout: 'agent_timeout',
  /** Ephemeral: Seq=0, function call info (name, args, result). */
  FunctionCall: 'function_call',
} as const;

export type UpdateType = (typeof UpdateType)[keyof typeof UpdateType];

const EPHEMERAL_UPDATE_TYPES: ReadonlySet<UpdateType> = new Set([
  UpdateType.Typing,
  UpdateType.Streaming,
  UpdateType.AgentStatus,
  UpdateType.AgentTimeout,
  UpdateType.FunctionCall,
  // NOTE: Conversation is NOT included here. It is a persistent type with real
  // sequence numbers in the standalone Updates channel. When piggybacked in a
  // response (D-118), it uses seq=0 which the SyncManager handles via the
  // `update.seq === 0` check, not via isEphemeralUpdateType.
]);

/**
 * Returns true if the update type is ephemeral (Seq=0, never persisted, never pulled).
 */
export function isEphemeralUpdateType(type: UpdateType): boolean {
  return EPHEMERAL_UPDATE_TYPES.has(type);
}

/**
 * ResponseCode indicates the result status of a request.
 * Zero or positive values indicate success; negative values indicate errors.
 * Mirrors Go ResponseCode (int32).
 */
export const ResponseCode = {
  /** Request was processed successfully. */
  OK: 0,
  /** Request failed with an error. */
  Error: -1,
} as const;

export type ResponseCode = number;

/**
 * Package is the top-level message envelope for the WebSocket protocol.
 * JSON field names match the Go struct tags exactly.
 */
export interface Package {
  /** Protocol version. Defaults to 1 when zero-valued (Go omitempty). */
  version?: number;
  type: PackageType;
  /** Raw JSON payload — one of PackageDataRequest / PackageDataResponse / PackageDataUpdates. */
  data: unknown;
}

/**
 * PackageDataRequest is a client-initiated request to invoke a method.
 * JSON field names match the Go struct tags exactly.
 */
export interface PackageDataRequest {
  /** Unique identifier for correlating requests with responses. */
  id: string;
  /** Name of the method to invoke on the server. */
  method: string;
  /** Method parameters as JSON. */
  params: unknown;
  /**
   * Server-generated key (equal to reqID) used for deduplication during
   * replay of timed-out requests (Phase 4, D-104).
   */
  idempotency_key?: string;
  /**
   * Per-device monotonically increasing sequence number assigned by the
   * server for ordering reverse-RPC requests (Phase 4, D-104).
   */
  seq?: number;
}

/**
 * PackageDataResponse is the server's reply to a PackageDataRequest.
 * JSON field names match the Go struct tags exactly.
 */
export interface PackageDataResponse {
  /** Correlates this response with the originating request. */
  id: string;
  /** Success (0) or an error (negative value). */
  code: ResponseCode;
  /** Human-readable status message. */
  msg: string;
  /** Response payload as JSON. */
  data: unknown;
  /**
   * Piggyback updates attached to this response (D-118).
   * When present, the client should process these updates before resolving
   * the pending RPC promise. This is a low-latency optimization variant of
   * the Pull-on-Notification pattern.
   */
  updates?: PackageDataUpdate[];
}

/**
 * PackageDataUpdates wraps a batch of data update notifications.
 * JSON field names match the Go struct tags exactly.
 */
export interface PackageDataUpdates {
  updates: PackageDataUpdate[];
}

/**
 * PackageDataUpdate represents a single incremental data change.
 * JSON field names match the Go struct tags exactly.
 */
export interface PackageDataUpdate {
  /** Monotonically increasing sequence number for ordering. */
  seq: number;
  /** Kind of update (e.g. "message", "delete_message"). See UpdateType. */
  type: string;
  /** Update data as JSON. */
  payload: unknown;
  /** Timestamp when this update was generated (ISO string from Go time.Time). */
  created_at?: string;
}
