/**
 * @packageDocumentation
 * Data model interfaces for the Xyncra TypeScript client.
 *
 * These interfaces mirror the Go models defined in pkg/store/model/*.go.
 * Field names use snake_case to match Go JSON tags and database column names.
 *
 * All 9 models map 1:1 to the Go GORM models:
 *   - Conversation  -> pkg/store/model/conversation.go
 *   - Message       -> pkg/store/model/message.go
 *   - RemoteCalling -> pkg/store/model/remote_calling.go
 *   - SyncState     -> pkg/store/model/sync_state.go
 *   - Draft         -> pkg/store/model/draft.go
 *   - RetryTask     -> pkg/store/model/retry_task.go
 *   - RPCLog        -> pkg/store/model/rpc_log.go
 *   - NotificationLog -> pkg/store/model/notification_log.go
 *   - UserUpdate    -> pkg/store/model/user_update.go
 */

// ---------------------------------------------------------------------------
// Conversation — 1-on-1 messaging conversation between two users
// ---------------------------------------------------------------------------

/** Agent status constants for the HITL state machine (D-125). */
export const AgentStatus = {
  Idle: 'idle',
  Thinking: 'thinking',
  ToolCalling: 'tool_calling',
  Generating: 'generating',
  AskingUser: 'asking_user',
  Timeout: 'timeout',
} as const;

export type AgentStatusValue = (typeof AgentStatus)[keyof typeof AgentStatus];

/**
 * Conversation represents a 1-on-1 messaging conversation between two users.
 * Mirrors Go model.Conversation (pkg/store/model/conversation.go).
 */
export interface Conversation {
  /** Primary key (UUID, size:36). */
  id: string;
  /** First user ID (size:64). */
  user_id1: string;
  /** Second user ID (size:64). Only 1-on-1 not null. */
  user_id2: string;
  /** Conversation type: "1-on-1" / "group" / "channel". */
  type: string;
  /** Conversation title. */
  title: string;
  /** Whether the conversation is pinned. */
  pinned: boolean;
  /** Whether the conversation is muted. */
  muted: boolean;
  /** Avatar URL. */
  avatar_url: string;
  /** Description text. */
  description: string;
  /** Last processed message ID (server-assigned seq). */
  last_processed_message_id: number;
  /** Record creation time. */
  created_at: Date;
  /** Record last update time. */
  updated_at: Date;
  /** Last message timestamp (used for ordering). */
  last_message_at: Date;
  /** UserID1's read cursor position (D-012). MAX semantics. */
  last_read_message_id1: number;
  /** UserID2's read cursor position (D-012). MAX semantics. */
  last_read_message_id2: number;

  // HITL state machine fields (D-125)
  /** Agent status: idle, thinking, tool_calling, generating, asking_user, timeout. */
  agent_status: string;
  /** Agent identifier. */
  agent_id: string;
  /** Checkpoint identifier for HITL state. */
  checkpoint_id: string;
  /** Last agent activity timestamp. */
  agent_last_activity: Date;

  /** Soft-delete timestamp. Null means not deleted. */
  deleted_at: Date | null;

  /** RemoteCallings (transient, not stored in DB, D-137). */
  remote_callings?: RemoteCalling[];
}

// ---------------------------------------------------------------------------
// Message — single message within a conversation
// ---------------------------------------------------------------------------

/**
 * Message represents a single message within a conversation.
 * Mirrors Go model.Message (pkg/store/model/message.go).
 */
export interface Message {
  /** Primary key (UUID, size:36). */
  id: string;
  /** Client-generated unique ID (size:36). */
  client_message_id: string;
  /** Conversation ID this message belongs to (size:36). */
  conversation_id: string;
  /** Server-assigned message sequence within the conversation. */
  message_id: number;
  /** Sender user ID (size:64). */
  sender_id: string;
  /** Message content (text). */
  content: string;
  /** Message type: "text", "image", etc. Default "text". */
  type: string;
  /** Reply-to message_id (0 if not a reply). */
  reply_to: number;
  /** Message status: "sent", "delivered", "read", "failed". Default "sent". */
  status: string;
  /** Record creation time. */
  created_at: Date;
  /** Soft-delete timestamp. Null means not deleted. */
  deleted_at: Date | null;
}

// ---------------------------------------------------------------------------
// RemoteCalling — remote calling (client-side mirror, D-137)
// ---------------------------------------------------------------------------

/**
 * RemoteCalling represents a remote call initiated by an Agent (client-side mirror, D-137).
 * Unifies HITL questions and client function calls into a single model.
 *
 * Mirrors Go model.RemoteCalling (pkg/store/model/remote_calling.go).
 */
export interface RemoteCalling {
  /** Primary key (UUID, size:36). */
  id: string;
  /** Conversation ID this remote calling belongs to. */
  conversation_id: string;
  /** Checkpoint identifier. */
  checkpoint_id: string;
  /** Agent identifier. */
  agent_id: string;
  /** Method name (e.g. ask_user, pg_chatai_sendMessage). */
  method: string;
  /** JSON parameters. */
  params: string;
  /** Eino interrupt ID. */
  interrupt_id: string;
  /** Device ID (empty = any device, non-empty = specific device). */
  device_id: string;
  /** Status: "pending", "resolved", "cancelled", "expired". Default "pending". */
  status: string;
  /** Result on success. */
  result: string;
  /** Error message on failure. */
  error_message: string;
  /** Whether the call succeeded. */
  success: boolean;
  /** Record creation time. */
  created_at: Date;
  /** Resolution timestamp. */
  resolved_at: Date | null;
  /** Expiration timestamp. */
  expires_at: Date | null;
  /** Cancellation timestamp. */
  cancelled_at: Date | null;
  /** User ID that cancelled this remote calling. */
  cancelled_by: string;
  /** Reason for cancellation. */
  cancel_reason: string;
}

// ---------------------------------------------------------------------------
// RetryQueue — pending RemoteCalling retry task with exponential backoff (D-137)
// ---------------------------------------------------------------------------

/**
 * RetryQueue represents a pending RemoteCalling retry task with exponential backoff.
 * Used to persist failed agent_resume calls for retry.
 */
export interface RetryQueueItem {
  /** Auto-increment primary key. */
  id?: number;
  /** RemoteCalling ID. */
  remote_calling_id: string;
  /** Whether the call succeeded. */
  success: boolean;
  /** Result on success. */
  result: string;
  /** Error message on failure. */
  error_message: string;
  /** Agent ID for the resume call. */
  agent_id: string;
  /** Current retry count. */
  retry_count: number;
  /** Next retry timestamp. */
  next_retry_at: Date;
  /** Record creation time. */
  created_at: Date;
}

// ---------------------------------------------------------------------------
// SyncState — key-value pairs for client-side synchronization state
// ---------------------------------------------------------------------------

/** Well-known sync state keys. */
export const SyncStateKey = {
  LocalMaxSeq: 'local_max_seq',
  LatestSeq: 'latest_seq',
} as const;

/**
 * SyncState stores key-value pairs for client-side synchronization state,
 * such as local_max_seq and latest_seq trackers.
 *
 * Mirrors Go model.SyncState (pkg/store/model/sync_state.go).
 */
export interface SyncState {
  /** Primary key (key name, size:64). */
  key: string;
  /** Value (text). */
  value: string;
  /** Last update time. */
  updated_at: Date;
}

// ---------------------------------------------------------------------------
// Draft — message draft for a conversation (one per conversation)
// ---------------------------------------------------------------------------

/**
 * Draft represents a message draft for a conversation.
 * Each conversation can have at most one draft (ConversationID is uniqueIndex).
 *
 * Mirrors Go model.Draft (pkg/store/model/draft.go).
 */
export interface Draft {
  /** Primary key (UUID, size:36). */
  id: string;
  /** Conversation ID (uniqueIndex). */
  conversation_id: string;
  /** Draft content (text). */
  content: string;
  /** Record creation time. */
  created_at: Date;
  /** Record last update time. */
  updated_at: Date;
}

// ---------------------------------------------------------------------------
// RetryTask — pending RPC retry task with exponential backoff
// ---------------------------------------------------------------------------

/**
 * RetryTask represents a pending RPC retry task with exponential backoff.
 *
 * Mirrors Go model.RetryTask (pkg/store/model/retry_task.go).
 */
export interface RetryTask {
  /** Primary key (UUID, size:36). */
  id: string;
  /** RPC method name (size:64). */
  method: string;
  /** Serialized parameters (blob). */
  params: Uint8Array;
  /** Current attempt count. Default 0. */
  attempt: number;
  /** Maximum number of attempts. Default 5. */
  max_attempts: number;
  /** Next retry timestamp. */
  next_retry: Date;
  /** Task status: "pending", "processing", "failed", "completed". Default "pending". */
  status: string;
  /** Last error message (text). */
  last_error: string;
  /** Record creation time. */
  created_at: Date;
}

// ---------------------------------------------------------------------------
// RPCLog — single RPC call for observability and debugging
// ---------------------------------------------------------------------------

/**
 * RPCLog records a single RPC call for observability and debugging.
 *
 * Mirrors Go model.RPCLog (pkg/store/model/rpc_log.go).
 */
export interface RPCLog {
  /** Primary key (UUID, size:36). */
  id: string;
  /** Log type: "request" or "response". */
  type: string;
  /** Request identifier (size:64). */
  request_id: string;
  /** RPC method name (size:64). */
  method: string;
  /** Serialized request parameters (blob). */
  params: Uint8Array;
  /** Serialized response data (blob). */
  response: Uint8Array;
  /** HTTP/RPC status code. */
  status_code: number;
  /** Conversation ID (size:36). */
  conversation_id: string;
  /** Call duration in milliseconds. */
  duration: number;
  /** Error message (text). */
  error_msg: string;
  /** Record creation time. */
  created_at: Date;
}

// ---------------------------------------------------------------------------
// NotificationLog — received push notification for deduplication
// ---------------------------------------------------------------------------

/**
 * NotificationLog records a received push notification for deduplication
 * and auditing.
 *
 * Mirrors Go model.NotificationLog (pkg/store/model/notification_log.go).
 */
export interface NotificationLog {
  /** Primary key (UUID, size:36). */
  id: string;
  /** Sequence number (uniqueIndex). Used for deduplication (C6). */
  seq: number;
  /** Notification type (size:20). */
  type: string;
  /** Serialized notification payload (blob). */
  payload: Uint8Array;
  /** Record creation time. */
  created_at: Date;
}

// ---------------------------------------------------------------------------
// UserUpdate — incremental data change for a user's event stream
// ---------------------------------------------------------------------------

/**
 * UserUpdate represents an incremental data change for a user's event stream.
 * Unlike Conversation and Message, UserUpdate does not use soft delete --
 * expired records are hard-deleted during cleanup (D-016).
 *
 * Mirrors Go model.UserUpdate (pkg/store/model/user_update.go).
 */
export interface UserUpdate {
  /** Primary key (UUID, size:36). */
  id: string;
  /** User ID (size:64). */
  user_id: string;
  /** Sequence number for ordering. */
  seq: number;
  /** Update type: "message", etc. Default "message". */
  type: string;
  /** Serialized update payload (blob). */
  payload: Uint8Array;
  /** Record creation time. */
  created_at: Date;
}
