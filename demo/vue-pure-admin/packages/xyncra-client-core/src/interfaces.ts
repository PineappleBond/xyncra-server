/**
 * @packageDocumentation
 * Core interfaces for the Xyncra TypeScript client.
 *
 * All environment dependencies (WebSocket, IndexedDB, logging) are abstracted
 * behind interfaces to keep the core package environment-agnostic (TS-D-002).
 */

import type { PackageDataUpdate } from '@xyncra/protocol';
import type { RemoteCalling } from './db/models';

// ---------------------------------------------------------------------------
// Conversation action
// ---------------------------------------------------------------------------

/**
 * ConversationAction describes the kind of change behind an onConversation call.
 * Mirrors the `action` field of a "conversation" type PackageDataUpdate.
 */
export type ConversationAction = 'created' | 'updated' | 'removed';

// ---------------------------------------------------------------------------
// Domain model types (placeholders)
// ---------------------------------------------------------------------------

// TODO: These domain model types will be defined in the store layer (Phase 2 Step 4+).
// They are placed here temporarily so IUpdateHandler can reference them.
// Once the store package exists, import from there and remove these definitions.

/**
 * Message represents a chat message in the local data model.
 * Field names use camelCase (TypeScript convention); database columns use snake_case.
 */
export interface Message {
  id: string;
  conversationId: string;
  senderId: string;
  content: string;
  type?: string; // 'text' | 'tool_calling' | 'summary'
  clientMessageId: string;
  replyToId?: string;
  createdAt: string; // ISO timestamp
  updatedAt?: string; // ISO timestamp
  deletedAt?: string; // ISO timestamp (soft delete)
}

/**
 * Conversation represents a 1-on-1 conversation in the local data model.
 * Field names use camelCase (TypeScript convention); database columns use snake_case.
 */
export interface Conversation {
  id: string;
  userId1: string;
  userId2: string;
  title?: string;
  lastMessageId?: string;
  lastMessageAt?: string; // ISO timestamp
  lastReadMessageId1?: string;
  lastReadMessageId2?: string;
  createdAt: string; // ISO timestamp
  updatedAt?: string; // ISO timestamp
  deletedAt?: string; // ISO timestamp (soft delete)
  // HITL fields (D-125 / D-137)
  agentStatus?: string; // 'idle' | 'thinking' | 'tool_calling' | 'generating' | 'asking_user' | 'timeout'
  agentId?: string;
  checkpointId?: string;
  remoteCallings?: RemoteCalling[];
}

// ---------------------------------------------------------------------------
// WebSocket abstraction (TS-D-002: environment-agnostic)
// ---------------------------------------------------------------------------

/**
 * IWebSocket abstracts the WebSocket transport.
 *
 * This allows injecting browser WebSocket, Node.js `ws`, or a mock
 * implementation for testing — keeping the core package environment-agnostic.
 *
 * readyState constants mirror the browser WebSocket API:
 *   0 = CONNECTING, 1 = OPEN, 2 = CLOSING, 3 = CLOSED
 */
export interface IWebSocket {
  /** Send data over the WebSocket connection. */
  send(data: string | Uint8Array): void;
  /** Close the WebSocket connection with an optional code and reason. */
  close(code?: number, reason?: string): void;
  /** Register a handler for incoming messages. */
  onmessage(handler: (data: string | Uint8Array) => void): void;
  /** Register a handler for connection close events. */
  onclose(handler: (code: number, reason: string) => void): void;
  /** Register a handler for errors. */
  onerror(handler: (error: Error) => void): void;
  /** Register a handler for connection open events. */
  onopen(handler: () => void): void;
  /** Current connection state (0=CONNECTING, 1=OPEN, 2=CLOSING, 3=CLOSED). */
  readyState: number;
}

/**
 * IWebSocketFactory creates WebSocket instances for a given URL.
 * Injected via ClientOptions to keep the core package environment-agnostic (TS-D-002).
 */
export interface IWebSocketFactory {
  create(url: string): IWebSocket;
}

// ---------------------------------------------------------------------------
// IndexedDB abstraction (TS-D-002: environment-agnostic)
// ---------------------------------------------------------------------------

/**
 * IIndexedDBProvider abstracts the IndexedDB factory.
 *
 * In browsers, this returns the global `indexedDB`.
 * In tests, this returns a `fake-indexeddb` instance.
 * Injected via ClientOptions to keep the core package environment-agnostic.
 */
export interface IIndexedDBProvider {
  getIDBFactory(): IDBFactory;
}

// ---------------------------------------------------------------------------
// Logger abstraction
// ---------------------------------------------------------------------------

/** Log severity levels. */
export type LogLevel = 'debug' | 'info' | 'warn' | 'error';

/**
 * ILogger abstracts structured logging.
 * Injected via ClientOptions to allow any logging backend.
 */
export interface ILogger {
  debug(message: string, ...args: unknown[]): void;
  info(message: string, ...args: unknown[]): void;
  warn(message: string, ...args: unknown[]): void;
  error(message: string, ...args: unknown[]): void;
}

// ---------------------------------------------------------------------------
// UpdateHandler — processes data updates from the sync pipeline
// ---------------------------------------------------------------------------

/**
 * IUpdateHandler receives processed data updates from the sync pipeline.
 * Implementations are provided by the caller to apply updates to a local
 * store or in-memory cache.
 *
 * Mirrors Go's UpdateHandler interface (pkg/client/options.go).
 */
export interface IUpdateHandler {
  /** Called when a new or updated message is received. */
  onMessage(message: Message): Promise<void>;
  /** Called when a message deletion is received. */
  onDeleteMessage(messageId: string, conversationId: string): Promise<void>;
  /** Called when a read cursor advance is received. */
  onMarkRead(conversationId: string, messageId: string): Promise<void>;
  /**
   * Called when a conversation state change is received.
   * @param conversation The affected conversation. For `removed` actions only
   *        the `id` field is guaranteed to be populated.
   * @param action The kind of change: `created`, `updated`, or `removed`.
   */
  onConversation(
    conversation: Conversation,
    action: ConversationAction,
  ): Promise<void>;
  /** Called when a sequence gap is detected during sync. */
  onGap(seq: number): Promise<void>;
}

// ---------------------------------------------------------------------------
// Optional extension handler interfaces
// ---------------------------------------------------------------------------

/**
 * ITypingHandler receives ephemeral typing indicator updates.
 * UpdateHandler implementations may optionally adopt this interface
 * (detected via type assertion at runtime).
 *
 * Mirrors Go's TypingHandler interface.
 */
export interface ITypingHandler {
  onTyping(
    userId: string,
    conversationId: string,
    isTyping: boolean,
    isAgent: boolean,
  ): Promise<void>;
}

/**
 * IStreamingHandler receives ephemeral streaming text updates (D-051).
 * UpdateHandler implementations may optionally adopt this interface.
 *
 * Mirrors Go's StreamingHandler interface.
 */
export interface IStreamingHandler {
  onStreaming(
    userId: string,
    conversationId: string,
    streamId: string,
    text: string,
    isDone: boolean,
    isAgent: boolean,
  ): Promise<void>;
}

/**
 * IAgentStatusHandler receives agent status change events (D-087).
 * UpdateHandler implementations may optionally adopt this interface.
 *
 * Mirrors Go's AgentStatusHandler interface.
 */
export interface IAgentStatusHandler {
  onAgentStatus(
    userId: string,
    conversationId: string,
    status: string,
  ): Promise<void>;
}

/**
 * IAgentTimeoutHandler receives agent timeout ephemeral update events (D-087).
 * UpdateHandler implementations may optionally adopt this interface.
 *
 * Mirrors Go's AgentTimeoutHandler interface.
 */
export interface IAgentTimeoutHandler {
  onAgentTimeout(
    userId: string,
    conversationId: string,
    reason: string,
  ): Promise<void>;
}

/**
 * IFunctionCallHandler receives function call ephemeral update events.
 * UpdateHandler implementations may optionally adopt this interface
 * (detected via type assertion at runtime).
 *
 * Sent when the agent invokes a tool/function, allowing clients to display
 * function call information in the UI.
 */
export interface IFunctionCallHandler {
  onFunctionCall(
    userId: string,
    conversationId: string,
    name: string,
    args: string,
    result: string,
    error: string,
    durationMs: number,
    isDone: boolean,
  ): Promise<void>;
}
