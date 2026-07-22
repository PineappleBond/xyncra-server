/**
 * @packageDocumentation
 * XyncraClient — high-level entry point for the Xyncra TypeScript client.
 *
 * Mirrors Go XyncraClient (pkg/client/client.go):
 *   - Manages a WebSocket connection to a Xyncra server via ConnectionManager.
 *   - Synchronises data via SyncManager.
 *   - Retries failed RPCs via RetryManager.
 *   - Exposes typed convenience methods for the supported RPC verbs.
 *   - Handles server-initiated requests (reverse RPC) with idempotency dedup.
 *
 * Key constraints (must be strictly enforced):
 *   C11 — Reconnect handshake: system.reconnect + system.register_functions (fail-open).
 *   C13 — Idempotency dedup check before handler invocation.
 *   C14 — lastReqSeq tracks highest inbound request seq for system.reconnect.
 *   C15 — RPC log save/update is best-effort (errors ignored).
 *
 * Concurrency model differences from Go:
 *   Go goroutine + sync.Mutex  → TS single-threaded (no mutex needed)
 *   Go chan *Response (pending) → TS Map<string, resolve callback>
 *   Go context.Context          → TS AbortController + AbortSignal
 *   Go sync.WaitGroup           → TS Promise.all() + running flags
 *   Go time.Ticker              → TS setTimeout loops
 */

import type {
  FunctionInfo,
  Package,
  PackageDataRequest,
  PackageDataResponse,
  PackageDataUpdate,
} from '@xyncra/protocol';
import type { ConnectionCallbacks } from './connection-manager';
import { ConnectionManager } from './connection-manager';
import {
  DefaultAdaptiveTimeoutMax,
  DefaultAdaptiveTimeoutMin,
  DefaultHeartbeatInterval,
  DefaultIdempotencyCacheSize,
  DefaultReconnectBaseDelay,
  DefaultReconnectMaxDelay,
  DefaultRetryPollInterval,
  DefaultRPCTimeout,
  DefaultRTTSamples,
  DefaultSyncBatchSize,
  DefaultSyncRetryInterval,
} from './constants';
import { XyncraDatabase } from './db';
import type {
  Conversation as DBConversation,
  Message as DBMessage,
  RPCLog,
} from './db/models';
import {
  ClientError,
  ConnectionError,
  ErrorCodeConnectionError,
  ErrorCodeTimeoutError,
  TimeoutError,
} from './errors';
import { IdempotencyCache } from './idempotency-cache';
import type { ILogger } from './interfaces';
import type { ClientOptions } from './options';
import { ResponseRetryQueue } from './response-retry-queue';
import { RetryManager } from './retry-manager';
import { RTTTracker } from './rtt-tracker';
import { SyncManager } from './sync-manager';

// ---------------------------------------------------------------------------
// RequestHandlerFunc
// ---------------------------------------------------------------------------

/**
 * RequestHandlerFunc processes a server-initiated request and returns response data.
 * Mirrors Go RequestHandlerFunc (pkg/client/client.go).
 */
export type RequestHandlerFunc = (
  request: PackageDataRequest,
) => Promise<unknown>;

// ---------------------------------------------------------------------------
// Result types
// ---------------------------------------------------------------------------

/** Result of the SendMessage RPC. */
export interface SendMessageResult {
  message: DBMessage;
  duplicate: boolean;
}

/** Result of the SyncUpdates RPC. */
export interface SyncUpdatesResult {
  updates: PackageDataUpdate[];
  has_more: boolean;
  latest_seq: number;
}

/** Result of the CreateConversation RPC. */
export interface CreateConversationResult {
  conversation: DBConversation;
  duplicate: boolean;
}

/** Result of the ListConversations query (local DB). */
export interface ListConversationsResult {
  conversations: DBConversation[];
  has_more: boolean;
}

/** Result of the GetMessages / FetchMoreMessages query. */
export interface GetMessagesResult {
  messages: DBMessage[];
  has_more: boolean;
}

/** Result of the SearchMessages query (local DB). */
export interface SearchMessagesResult {
  messages: DBMessage[];
  has_more: boolean;
}

/** Result of the GetConversation query (local DB + unread + questions, D-125). */
export interface GetConversationResult {
  conversation: DBConversation;
  unread_count: number;
  questions: Array<{
    id: string;
    conversation_id: string;
    checkpoint_id: string;
    interrupt_id: string;
    question_text: string;
    status: string;
    created_at: Date;
  }>;
}

/** Result of the DeleteConversation RPC. */
export interface DeleteConversationResult {
  status: string;
  deleted_message_count: number;
}

/** Result of the RestoreConversation RPC. */
export interface RestoreConversationResult {
  conversation: DBConversation;
  restored_message_count: number;
}

// ---------------------------------------------------------------------------
// XyncraClient
// ---------------------------------------------------------------------------

/**
 * XyncraClient is the high-level entry point for the xyncra-client library.
 * It manages a WebSocket connection to a Xyncra server, synchronises data via
 * the sync pipeline, retries failed RPCs, and exposes typed convenience methods
 * for the supported RPC verbs.
 *
 * Mirrors Go XyncraClient (pkg/client/client.go).
 */
export class XyncraClient {
  // ---- Sub-modules ----
  private readonly db: XyncraDatabase;
  private readonly connMgr: ConnectionManager;
  private readonly syncMgr: SyncManager;
  private readonly retryMgr: RetryManager;
  private readonly idempotencyCache: IdempotencyCache;
  private readonly rttTracker: RTTTracker;
  private readonly responseRetryQueue: ResponseRetryQueue;

  // ---- RPC dispatch state ----

  /** Pending RPC requests waiting for a response, keyed by request ID. */
  private readonly pending: Map<
    string,
    (response: PackageDataResponse) => void
  > = new Map();

  /** Registered handlers for server-initiated requests (D-092). */
  private readonly requestHandlers: Map<string, RequestHandlerFunc> = new Map();

  /**
   * Highest PackageDataRequest.Seq received from the server.
   * Used in system.reconnect handshake (C14).
   */
  private lastReqSeq = 0;

  // ---- Lifecycle state ----

  private closed = false;
  private abortController: AbortController | null = null;

  /** Resolved when the client has fully shut down (mirrors Go's done channel). */
  private doneResolve: (() => void) | null = null;
  private readonly donePromise: Promise<void>;

  /** Ensure shutdown logic runs at most once. */
  private shutdownRan = false;

  // ---- Configuration ----

  private readonly logger: ILogger;
  private readonly options: ClientOptions;
  private readonly onError?: (method: string, message: string, code: number) => void;

  // ---- Resolved option values (with defaults applied) ----

  private readonly heartbeatInterval: number;
  private readonly rpcTimeout: number;
  private readonly adaptiveTimeoutMin: number;
  private readonly adaptiveTimeoutMax: number;

  // ---------------------------------------------------------------------------
  // Constructor
  // ---------------------------------------------------------------------------

  constructor(options: ClientOptions) {
    // Validate required fields.
    if (!options.serverURL) {
      throw new Error('client: serverURL is required');
    }
    if (!options.userID) {
      throw new Error('client: userID is required');
    }

    this.options = options;
    this.logger = options.logger;
    this.onError = options.onError;

    // Resolve tunable options with defaults.
    this.heartbeatInterval =
      options.heartbeatInterval ?? DefaultHeartbeatInterval;
    this.rpcTimeout = options.rpcTimeout ?? DefaultRPCTimeout;
    this.adaptiveTimeoutMin =
      options.reconnectBaseDelay !== undefined
        ? DefaultAdaptiveTimeoutMin
        : DefaultAdaptiveTimeoutMin;
    this.adaptiveTimeoutMax = DefaultAdaptiveTimeoutMax;

    // Done promise (mirrors Go's done channel).
    this.donePromise = new Promise<void>((resolve) => {
      this.doneResolve = resolve;
    });

    // --- Database ---
    const dbName =
      options.dbPath ?? `xyncra-${options.userID}-${options.deviceID}`;
    this.db = new XyncraDatabase(dbName, options.idbProvider?.getIDBFactory());

    // --- Utility modules ---
    this.idempotencyCache = new IdempotencyCache(
      options.idempotencyCacheSize ?? DefaultIdempotencyCacheSize,
    );
    this.rttTracker = new RTTTracker(options.rttSamples ?? DefaultRTTSamples);
    this.responseRetryQueue = new ResponseRetryQueue(
      options.reconnectBaseDelay ?? DefaultReconnectBaseDelay,
      options.reconnectMaxDelay ?? DefaultReconnectMaxDelay,
    );

    // --- ConnectionManager ---
    const connCallbacks: ConnectionCallbacks = {
      onResponse: (resp) => this.dispatchResponse(resp),
      onUpdates: (updates) => this.dispatchUpdates(updates),
      onRequest: (req) => {
        void this.handleIncomingRequest(req);
      },
      onConnect: () => {
        this.logger.info('Connected');
      },
      onDisconnect: (replaced: boolean) => {
        if (replaced) {
          this.logger.info('Connection replaced by newer instance (4001)');
          // Cancel the abort controller so that blocking operations
          // (e.g. FullSync RPC) unblock immediately (D-111).
          if (this.abortController) {
            this.abortController.abort();
          }
        } else {
          this.logger.warn('Disconnected');
        }
      },
    };

    this.connMgr = new ConnectionManager({
      serverURL: options.serverURL,
      userID: options.userID,
      deviceID: options.deviceID,
      wsFactory: options.wsFactory,
      logger: this.logger,
      callbacks: connCallbacks,
      pingInterval: options.pingInterval,
      pongWait: options.pongWait,
      writeWait: options.writeWait,
      sendBufSize: options.sendBufSize,
      reconnectBaseDelay:
        options.reconnectBaseDelay ?? DefaultReconnectBaseDelay,
      reconnectMaxDelay: options.reconnectMaxDelay ?? DefaultReconnectMaxDelay,
      maxMessageSize: options.maxMessageSize,
    });

    // --- SyncManager ---
    this.syncMgr = new SyncManager({
      db: this.db,
      handler: options.updateHandler,
      rpcFn: (method, params) => this.call(method, params),
      logger: this.logger,
      batchSize: options.syncBatchSize ?? DefaultSyncBatchSize,
      retryInterval: options.syncRetryInterval ?? DefaultSyncRetryInterval,
      debounceInterval: options.debounceInterval ?? DefaultSyncRetryInterval,
    });

    // --- RetryManager ---
    this.retryMgr = new RetryManager({
      db: this.db,
      rpcFn: (method, params) => this.call(method, params),
      logger: this.logger,
      pollInterval: options.retryPollInterval ?? DefaultRetryPollInterval,
    });
  }

  // ---------------------------------------------------------------------------
  // Lifecycle
  // ---------------------------------------------------------------------------

  /**
   * Starts background loops and blocks until the client is stopped.
   *
   * The initial WebSocket connection is established asynchronously inside the
   * connection monitor loop, which retries indefinitely on failure.
   *
   * @param abortSignal - Optional external abort signal to trigger shutdown.
   */
  async start(abortSignal?: AbortSignal): Promise<void> {
    if (this.closed) {
      throw new Error('client: already closed');
    }

    this.abortController = new AbortController();
    const signal = this.abortController.signal;

    // 1. Start sync and retry managers first so they are ready to handle
    //    updates that may arrive as soon as the connection is established.
    this.syncMgr.start();
    this.retryMgr.start();

    // 2. Launch background loops (fire-and-forget).
    void this.heartbeatLoop(signal);
    void this.responseRetryLoop(signal);
    void this.connectionMonitor(signal);

    // 3. Block until shutdown.
    await new Promise<void>((resolve) => {
      // Internal abort (e.g. stop() called, or 4001 detected).
      signal.addEventListener(
        'abort',
        () => {
          resolve();
        },
        { once: true },
      );

      // External abort signal.
      if (abortSignal) {
        if (abortSignal.aborted) {
          this.stop();
          resolve();
          return;
        }
        abortSignal.addEventListener(
          'abort',
          () => {
            this.stop();
            resolve();
          },
          { once: true },
        );
      }
    });
  }

  /**
   * Gracefully shuts down the client. Idempotent.
   *
   * Launches shutdown asynchronously so it never blocks the caller.
   * Use done() to wait for full shutdown completion.
   */
  stop(): void {
    if (this.closed) return;
    this.closed = true;
    if (this.abortController) {
      this.abortController.abort();
    }
    // Run shutdown asynchronously to avoid deadlock if called from a
    // tracked loop (mirrors Go's `go c.shutdown()` pattern).
    void this.shutdown();
  }

  /** Returns a Promise that resolves when the client has fully shut down. */
  done(): Promise<void> {
    return this.donePromise;
  }

  /** Returns the device identifier used by this client. */
  get deviceID(): string {
    return this.connMgr.getDeviceID();
  }

  /** Returns the underlying database instance for direct store access. */
  getDb(): XyncraDatabase {
    return this.db;
  }

  /** No-op retained for API compatibility (mirrors Go's Reconnect). */
  reconnect(): void {
    // no-op
  }

  // ---------------------------------------------------------------------------
  // RPC (generic)
  // ---------------------------------------------------------------------------

  /**
   * Performs a synchronous RPC call to the server.
   *
   * Generates a unique request ID, sends a PackageTypeRequest, and waits for
   * the matching response or a timeout.
   *
   * @param method - RPC method name.
   * @param params - RPC parameters (serialised as JSON).
   * @returns The response payload (data field).
   * @throws {ClientError} On server error, timeout, or connection failure.
   */
  async call(method: string, params: unknown): Promise<unknown> {
    if (this.closed) {
      throw new ClientError('client is closed', ErrorCodeConnectionError);
    }

    this.logger.info('call: invoking', { method });
    const reqID = crypto.randomUUID();

    // Register pending callback before sending.
    const responsePromise = new Promise<PackageDataResponse>((resolve) => {
      this.pending.set(reqID, resolve);
    });

    // Build and send the request package.
    const request: PackageDataRequest = {
      id: reqID,
      method,
      params,
    };
    const pkg: Package = {
      type: 0, // PackageTypeRequest
      version: 1,
      data: request,
    };

    const startTime = Date.now();

    // Extract conversation_id for logging.
    let conversationID = '';
    if (params && typeof params === 'object' && 'conversation_id' in params) {
      conversationID = String(
        (params as Record<string, unknown>).conversation_id ?? '',
      );
    }

    try {
      this.connMgr.sendPackage(pkg);
    } catch (error) {
      this.pending.delete(reqID);
      // Enqueue for retry on connection error.
      void this.retryMgr.enqueue(method, params);
      throw new ConnectionError('send package failed', error as Error);
    }

    // C15: Persist an initial RPC log entry (best-effort).
    const rpcLog: RPCLog = {
      id: crypto.randomUUID(),
      type: 'request',
      request_id: reqID,
      method,
      params: new TextEncoder().encode(JSON.stringify(params ?? {})),
      conversation_id: conversationID,
      response: new Uint8Array(0),
      status_code: 0,
      duration: 0,
      error_msg: '',
      created_at: new Date(startTime),
    };
    this.db.rpcLogsStore.save(rpcLog).catch((err) => {
      this.logger.debug('RPC log save failed', err);
    });

    // Compute adaptive timeout (C10).
    const timeout = this.rttTracker.adaptiveTimeout(
      this.rpcTimeout,
      this.adaptiveTimeoutMin,
      this.adaptiveTimeoutMax,
    );

    // Wait for response or timeout.
    let response: PackageDataResponse;
    try {
      response = await withTimeout(responsePromise, timeout);
    } catch (error) {
      // Timeout or abort.
      this.pending.delete(reqID);
      const duration = Date.now() - startTime;

      // Update RPC log (best-effort).
      rpcLog.duration = duration;
      rpcLog.error_msg = error instanceof Error ? error.message : String(error);
      rpcLog.status_code = ErrorCodeTimeoutError;
      rpcLog.type = 'response';
      this.db.rpcLogsStore.update(rpcLog).catch((err) => {
        this.logger.debug('RPC log update failed', err);
      });

      // Enqueue for retry.
      void this.retryMgr.enqueue(method, params);
      throw new TimeoutError(`rpc ${method} timed out`, error as Error);
    }

    // Record RTT.
    const rtt = Date.now() - startTime;
    this.rttTracker.record(rtt);

    // Update RPC log with final state (best-effort).
    const duration = Date.now() - startTime;
    rpcLog.duration = duration;
    rpcLog.type = 'response';
    rpcLog.status_code = response.code;

    if (response.code === 0) {
      // Success.
      rpcLog.response = new TextEncoder().encode(
        JSON.stringify(response.data ?? null),
      );
      this.db.rpcLogsStore.update(rpcLog).catch((err) => {
        this.logger.debug('RPC log update failed', err);
      });
      return response.data;
    }

    // Server returned an error.
    rpcLog.error_msg = response.msg;
    this.db.rpcLogsStore.update(rpcLog).catch((err) => {
      this.logger.debug('RPC log update failed', err);
    });

    // Notify error callback if provided
    if (this.onError) {
      this.onError(method, response.msg, response.code);
    }

    throw new ClientError(response.msg, response.code);
  }

  // ---------------------------------------------------------------------------
  // RPC (convenience methods)
  // ---------------------------------------------------------------------------

  /** Sends a heartbeat ping to the server. */
  async heartbeat(): Promise<void> {
    await this.call('heartbeat', null);
  }

  /**
   * Sends a chat message to the server.
   * clientMessageId is a UUID used for idempotency.
   */
  async sendMessage(
    conversationId: string,
    content: string,
    clientMessageId?: string,
    replyTo?: number,
  ): Promise<SendMessageResult> {
    const result = (await this.call('send_message', {
      conversation_id: conversationId,
      content,
      client_message_id: clientMessageId ?? crypto.randomUUID(),
      reply_to: replyTo ?? 0,
    })) as Record<string, unknown>;

    // Optimistically persist the user message to IndexedDB so it is
    // immediately available for queries (Bug fix: user messages missing
    // from IndexedDB). The server broadcast will arrive later but the
    // SyncManager's handleMessageTx catches duplicate-key errors, so
    // this is safe.
    const msgData = result.message as Record<string, unknown> | undefined;
    if (msgData) {
      try {
        const dbMessage: DBMessage = {
          id: (msgData.id as string) ?? '',
          client_message_id: (msgData.client_message_id as string) ?? '',
          conversation_id: (msgData.conversation_id as string) ?? '',
          message_id: (msgData.message_id as number) ?? 0,
          sender_id: (msgData.sender_id as string) ?? '',
          content: (msgData.content as string) ?? '',
          type: (msgData.type as string) ?? 'text',
          reply_to: (msgData.reply_to as number) ?? 0,
          status: (msgData.status as string) ?? 'sent',
          created_at: msgData.created_at
            ? new Date(msgData.created_at as string)
            : new Date(),
          deleted_at: msgData.deleted_at
            ? new Date(msgData.deleted_at as string)
            : null,
        };
        await this.db.messagesStore.upsert(dbMessage);
      } catch (err) {
        this.logger.debug('sendMessage: optimistic local persist failed', err);
      }
    }

    return result as unknown as SendMessageResult;
  }

  /** Creates a new 1-on-1 conversation with the specified user. */
  async createConversation(
    userId2: string,
    title?: string,
  ): Promise<CreateConversationResult> {
    const result = (await this.call('create_conversation', {
      user_id: userId2,
      title: title ?? '',
    })) as Record<string, unknown>;

    // Optimistically persist the conversation to IndexedDB so it is
    // immediately available for listConversations after page refresh.
    // The server broadcast will arrive later but the SyncManager's
    // handleConversationCreateTx catches duplicate-key errors, so this is safe.
    const convData = result.conversation as Record<string, unknown> | undefined;
    if (convData) {
      try {
        const dbConv: DBConversation = {
          id: (convData.id as string) ?? '',
          user_id1: (convData.user_id1 as string) ?? '',
          user_id2: (convData.user_id2 as string) ?? '',
          type: (convData.type as string) ?? '1-on-1',
          title: (convData.title as string) ?? '',
          pinned: (convData.pinned as boolean) ?? false,
          muted: (convData.muted as boolean) ?? false,
          avatar_url: (convData.avatar_url as string) ?? '',
          description: (convData.description as string) ?? '',
          last_processed_message_id: (convData.last_processed_message_id as number) ?? 0,
          created_at: convData.created_at
            ? new Date(convData.created_at as string)
            : new Date(),
          updated_at: convData.updated_at
            ? new Date(convData.updated_at as string)
            : new Date(),
          last_message_at: convData.last_message_at
            ? new Date(convData.last_message_at as string)
            : new Date(),
          last_read_message_id1: (convData.last_read_message_id1 as number) ?? 0,
          last_read_message_id2: (convData.last_read_message_id2 as number) ?? 0,
          agent_status: (convData.agent_status as string) ?? 'idle',
          agent_id: (convData.agent_id as string) ?? '',
          checkpoint_id: (convData.checkpoint_id as string) ?? '',
          agent_last_activity: convData.agent_last_activity
            ? new Date(convData.agent_last_activity as string)
            : new Date(),
          deleted_at: convData.deleted_at ? new Date(convData.deleted_at as string) : null,
        };
        await this.db.conversationsStore.upsert(dbConv);
      } catch (err) {
        this.logger.debug('createConversation: optimistic local persist failed', err);
      }
    }

    return result as unknown as CreateConversationResult;
  }

  /**
   * Returns a paginated list of conversations for the current user.
   * Reads from the local database (D-035).
   */
  async listConversations(
    offset?: number,
    limit?: number,
  ): Promise<ListConversationsResult> {
    const effectiveLimit = limit ?? 20;
    const convs = await this.db.conversationsStore.getByUser(
      this.options.userID,
      offset ?? 0,
      effectiveLimit + 1,
    );
    const hasMore = convs.length > effectiveLimit;
    const result = hasMore ? convs.slice(0, effectiveLimit) : convs;
    return { conversations: result, has_more: hasMore };
  }

  /**
   * Returns messages for the given conversation, optionally starting after
   * the specified message ID. Reads from the local database (D-035).
   */
  async getMessages(
    conversationId: string,
    afterMessageId?: number,
    limit?: number,
  ): Promise<GetMessagesResult> {
    const effectiveLimit = limit ?? 50;
    const msgs = await this.db.messagesStore.listByConversation(
      conversationId,
      afterMessageId ?? 0,
      effectiveLimit + 1,
    );
    const hasMore = msgs.length > effectiveLimit;
    const result = hasMore ? msgs.slice(0, effectiveLimit) : msgs;
    return { messages: result, has_more: hasMore };
  }

  /**
   * Fetches messages from the server via RPC, persists them locally, and
   * returns the results (D-126).
   */
  async fetchMoreMessages(
    conversationId: string,
    afterMessageId?: number,
    limit?: number,
  ): Promise<GetMessagesResult> {
    const result = (await this.call('get_messages', {
      conversation_id: conversationId,
      after_message_id: afterMessageId ?? 0,
      limit: limit ?? 50,
    })) as { messages: DBMessage[]; has_more: boolean };

    // Persist fetched messages to local DB (best-effort).
    for (const msg of result.messages) {
      try {
        await this.db.messagesStore.upsert(msg);
      } catch (error) {
        this.logger.error('Upsert message to local DB failed', {
          message_id: msg.id,
          error,
        });
      }
    }
    return { messages: result.messages, has_more: result.has_more };
  }

  /**
   * Searches for messages matching the given query within a conversation.
   * Reads from the local database (D-035).
   */
  async searchMessages(
    conversationId: string,
    query: string,
    afterMessageId?: number,
    limit?: number,
  ): Promise<SearchMessagesResult> {
    const effectiveLimit = limit ?? 50;
    const msgs = await this.db.messagesStore.searchByConversation(
      conversationId,
      query,
      afterMessageId ?? 0,
      effectiveLimit + 1,
    );
    const hasMore = msgs.length > effectiveLimit;
    const result = hasMore ? msgs.slice(0, effectiveLimit) : msgs;
    return { messages: result, has_more: hasMore };
  }

  /**
   * Returns the conversation identified by conversationId, including the
   * current unread count and HITL questions (D-125).
   * Reads from the local database (D-035).
   */
  async getConversation(
    conversationId: string,
  ): Promise<GetConversationResult> {
    const conv = await this.db.conversationsStore.get(conversationId);
    if (!conv) {
      throw new Error(`client: conversation not found: ${conversationId}`);
    }

    // Determine the read cursor for the current user.
    const lastRead =
      conv.user_id1 === this.options.userID
        ? conv.last_read_message_id1
        : conv.last_read_message_id2;

    const unreadCount = await this.db.messagesStore.countUnread(
      conversationId,
      lastRead,
    );

    const questions =
      await this.db.questionsStore.getByConversation(conversationId);

    return {
      conversation: conv,
      unread_count: unreadCount,
      questions,
    };
  }

  /**
   * Soft-deletes a conversation and returns the number of cascade-deleted messages.
   */
  async deleteConversation(
    conversationId: string,
  ): Promise<DeleteConversationResult> {
    const result = (await this.call('delete_conversation', {
      conversation_id: conversationId,
    })) as Record<string, unknown>;
    return result as unknown as DeleteConversationResult;
  }

  /**
   * Restores a previously soft-deleted conversation and returns the number
   * of cascade-restored messages.
   */
  async restoreConversation(
    conversationId: string,
  ): Promise<RestoreConversationResult> {
    const result = (await this.call('restore_conversation', {
      conversation_id: conversationId,
    })) as Record<string, unknown>;
    return result as unknown as RestoreConversationResult;
  }

  /** Soft-deletes a message by its ID. */
  async deleteMessage(messageId: string): Promise<void> {
    await this.call('delete_message', { message_id: messageId });
  }

  /**
   * Advances the read cursor for the current user in the given conversation
   * to the specified message ID.
   */
  async markAsRead(conversationId: string, messageId: number): Promise<void> {
    await this.call('mark_as_read', {
      conversation_id: conversationId,
      message_id: messageId,
    });
  }

  /** Fetches incremental updates from the server. */
  async syncUpdates(
    afterSeq: number,
    limit?: number,
  ): Promise<SyncUpdatesResult> {
    const result = (await this.call('sync_updates', {
      after_seq: afterSeq,
      limit: limit ?? 100,
    })) as Record<string, unknown>;
    return result as unknown as SyncUpdatesResult;
  }

  /**
   * Triggers a blocking, paginated synchronisation with the server, fetching
   * all updates after the current localMaxSeq until has_more is false.
   */
  async fullSync(): Promise<void> {
    const signal = this.abortController?.signal;
    await this.syncMgr.fullSync(signal);
  }

  // ---------------------------------------------------------------------------
  // Reverse RPC (D-092)
  // ---------------------------------------------------------------------------

  /**
   * Registers a handler for server-initiated requests with the given method name.
   */
  registerRequestHandler(method: string, handler: RequestHandlerFunc): void {
    this.requestHandlers.set(method, handler);
  }

  // ---------------------------------------------------------------------------
  // Internal dispatch
  // ---------------------------------------------------------------------------

  /**
   * Routes an incoming server response to the pending RPC caller identified
   * by the response's ID.
   */
  private dispatchResponse(response: PackageDataResponse): void {
    const resolve = this.pending.get(response.id);
    if (resolve) {
      this.pending.delete(response.id);
      resolve(response);
    } else {
      this.logger.warn(`No pending request for response id=${response.id}`);
    }
  }

  /**
   * Forwards a batch of server-pushed updates to the sync manager.
   */
  private async dispatchUpdates(updates: {
    updates: PackageDataUpdate[];
  }): Promise<void> {
    try {
      await this.syncMgr.applyUpdates(updates.updates);
    } catch (error) {
      this.logger.error('Apply updates failed', error);
    }
  }

  // ---------------------------------------------------------------------------
  // C11: Reconnect handshake
  // ---------------------------------------------------------------------------

  /**
   * Sends system.register_functions followed by system.reconnect after a
   * (re)connect. Functions are registered FIRST so the server has handlers
   * ready before it replays pending requests from the PendingStore.
   * Errors are logged but do not prevent FullSync from proceeding
   * (graceful degradation, D-072).
   */
  private async performReconnectHandshake(): Promise<void> {
    const signal = this.abortController?.signal;
    if (signal?.aborted) return;

    this.logger.info('performReconnectHandshake: starting', { functionCount: this.options.functions?.length ?? 0 });

    // Step 1: Re-register functions BEFORE reconnect so the server has
    // handlers ready when it replays pending requests (fixes race condition
    // where PendingStore replay arrives before client registers handlers).
    await this.reregisterFunctions();

    // Step 2: system.reconnect with last_seen_seq.
    try {
      await this.call('system.reconnect', {
        last_seen_seq: this.lastReqSeq,
      });
    } catch (error) {
      this.logger.error('system.reconnect handshake failed', error);
    }
  }

  /**
   * setFunctions updates the function list used by the reconnect handshake's
   * reregisterFunctions (D-101). The initial system.register_functions may be
   * dropped if sent before the socket is open (sendPackage silently drops when
   * not connected), so the handshake — which runs after the socket is open —
   * is the reliable re-send path. Callers (e.g. XyncraProvider) should keep
   * this in sync with their local function registry.
   */
  setFunctions(fns: FunctionInfo[]): void {
    this.options.functions = fns;
    this.logger.info('setFunctions called', { count: fns.length, closed: this.closed });
    // Immediately register functions with the server when they change
    // This ensures the agent can call newly registered functions without
    // waiting for a reconnect
    if (fns.length > 0 && !this.closed) {
      this.logger.info('setFunctions: registering functions immediately', { count: fns.length });
      void this.reregisterFunctions();
    }
  }

  /**
   * Sends system.register_functions after reconnect so the server knows which
   * functions this device provides (D-098, D-101).
   * Follows fail-open semantics (D-072): errors are logged but do not block
   * FullSync.
   */
  private async reregisterFunctions(): Promise<void> {
    const fns = this.options.functions;
    if (!fns || fns.length === 0) {
      this.logger.info('reregisterFunctions: no functions to register');
      return;
    }
    this.logger.info('reregisterFunctions: sending functions to server', { count: fns.length, names: fns.map(f => f.name) });

    const params: Record<string, unknown> = {
      functions: fns,
    };
    if (this.options.deviceInfo) {
      params.device_info = this.options.deviceInfo;
    }

    try {
      // Independent timeout so registration cannot stall FullSync.
      this.logger.info('reregisterFunctions: calling system.register_functions...');
      const result = await withTimeout(this.call('system.register_functions', params), 10_000);
      this.logger.info('reregisterFunctions: success', { result });
    } catch (error) {
      this.logger.error('reregisterFunctions: failed', {
        error: error instanceof Error ? error.message : String(error),
        count: fns.length,
      });
    }
  }

  // ---------------------------------------------------------------------------
  // C13/C14: handleIncomingRequest
  // ---------------------------------------------------------------------------

  /**
   * Processes a server-initiated request by looking up the registered handler,
   * invoking it, and sending back a response package.
   *
   * C14: Tracks the highest request seq (for system.reconnect).
   * C13: Idempotency dedup check before handler invocation.
   */
  private async handleIncomingRequest(
    request: PackageDataRequest,
  ): Promise<void> {
    // C14: Track highest request seq.
    if (request.seq !== undefined && request.seq > 0) {
      if (request.seq > this.lastReqSeq) {
        this.lastReqSeq = request.seq;
      }
    }

    // C13: Idempotency dedup check.
    const idempotencyKey = request.idempotency_key ?? '';
    if (idempotencyKey && this.idempotencyCache.contains(idempotencyKey)) {
      this.logger.debug('Deduplicating replayed request', {
        idempotency_key: idempotencyKey,
        method: request.method,
      });
      const response: PackageDataResponse = {
        id: request.id,
        code: 0,
        msg: 'duplicate (idempotency cache hit)',
        data: null,
      };
      this.sendResponse(response);
      return;
    }

    // Look up handler.
    const handler = this.requestHandlers.get(request.method);

    let response: PackageDataResponse;
    if (!handler) {
      response = {
        id: request.id,
        code: -1, // ResponseCodeError
        msg: `unknown method: ${request.method}`,
        data: null,
      };
    } else {
      try {
        const data = await handler(request);
        response = {
          id: request.id,
          code: 0, // ResponseCodeOK
          msg: 'ok',
          data,
        };
      } catch (error) {
        response = {
          id: request.id,
          code: -1, // ResponseCodeError
          msg: error instanceof Error ? error.message : String(error),
          data: null,
        };
      }
    }

    // Record idempotency key on successful processing.
    if (idempotencyKey) {
      this.idempotencyCache.put(idempotencyKey);
    }

    this.sendResponse(response);
  }

  // ---------------------------------------------------------------------------
  // sendResponse (with retry queue fallback)
  // ---------------------------------------------------------------------------

  /**
   * Sends a response package back to the server. If the send fails, the
   * response is enqueued in the ResponseRetryQueue for later retry.
   */
  private sendResponse(response: PackageDataResponse): void {
    try {
      const pkg: Package = {
        type: 1, // PackageTypeResponse
        version: 1,
        data: response,
      };
      this.connMgr.sendPackage(pkg);
    } catch (error) {
      this.logger.warn('Send response failed, enqueueing retry', error);
      this.responseRetryQueue.enqueue(response);
    }
  }

  // ---------------------------------------------------------------------------
  // Background loops
  // ---------------------------------------------------------------------------

  /**
   * Sends periodic heartbeat RPCs to keep the server-side session alive.
   */
  private async heartbeatLoop(signal: AbortSignal): Promise<void> {
    while (!this.closed && !signal.aborted) {
      await sleep(this.heartbeatInterval, signal);
      if (this.closed || signal.aborted) return;

      try {
        await this.heartbeat();
      } catch (error) {
        this.logger.warn('Heartbeat failed', error);
      }
    }
  }

  /**
   * Periodically drains the response retry queue and attempts to re-send
   * failed responses.
   */
  private async responseRetryLoop(signal: AbortSignal): Promise<void> {
    while (!this.closed && !signal.aborted) {
      await sleep(1000, signal);
      if (this.closed || signal.aborted) return;

      const now = Date.now();
      const responses = this.responseRetryQueue.drain(now);

      for (const response of responses) {
        try {
          const pkg: Package = {
            type: 1, // PackageTypeResponse
            version: 1,
            data: response,
          };
          this.connMgr.sendPackage(pkg);
        } catch (error) {
          this.logger.warn('Retry send response failed', error);
          // Re-enqueue with backoff; drain() already removed the entry.
          this.responseRetryQueue.enqueueWithBackoff(
            { response, nextRetryAt: now, attempt: 1 },
            now,
          );
        }
      }
    }
  }

  /**
   * Core connection monitor loop.
   *
   * Phase 1: Initial connection with infinite retries.
   * Phase 2: Watch for disconnects, reconnect with backoff, full sync.
   *
   * 4001 detected: graceful exit (D-111).
   */
  private async connectionMonitor(signal: AbortSignal): Promise<void> {
    // Phase 1 — initial connection with infinite retries.
    while (!this.closed && !signal.aborted) {
      try {
        await this.connMgr.connect(signal);
        this.logger.info('Initial connection established');
        await this.performReconnectHandshake();
        try {
          await this.syncMgr.fullSync(signal);
        } catch (syncError) {
          this.logger.error('Initial full sync failed', syncError);
        }
        // Notify UI that initial sync is complete (even if database is empty).
        this.options.onSyncComplete?.();
        break; // Connected — move to Phase 2.
      } catch (error) {
        if (this.closed || signal.aborted) return;
        this.logger.error('Initial connection failed, retrying...', error);
        await sleep(
          this.options.reconnectBaseDelay ?? DefaultReconnectBaseDelay,
          signal,
        );
      }
    }

    // Phase 2 — standard reconnect loop.
    while (!this.closed && !signal.aborted) {
      try {
        // Wait for disconnect.
        await this.connMgr.disconnected();
      } catch {
        // disconnected() resolved (not rejected), continue.
      }

      if (this.closed || signal.aborted) return;

      // C2: If device was replaced (4001), graceful exit.
      if (this.connMgr.isReplaced()) {
        this.logger.info(
          'Connection replaced by newer device instance (4001), initiating graceful exit (D-111)',
        );
        this.stop();
        return;
      }

      this.logger.info('Connection lost, reconnecting...');

      // Reconnect with backoff.
      let reconnected = false;
      while (!reconnected && !this.closed && !signal.aborted) {
        try {
          await this.connMgr.reconnect(signal);
          this.logger.info('Reconnected successfully');
          await this.performReconnectHandshake();
          try {
            await this.syncMgr.fullSync(signal);
          } catch (syncError) {
            this.logger.error('Full sync after reconnect failed', syncError);
          }
          // Notify UI that sync is complete after reconnection.
          // Without this, the Vue plugin's connectionStatus stays at 'connecting'
          // after a reconnect, preventing function re-registration and causing
          // the agent to report "device offline" when calling pg_* functions.
          this.options.onSyncComplete?.();
          reconnected = true;
        } catch (error) {
          if (this.closed || signal.aborted) return;
          this.logger.error('Reconnect failed', error);
        }
      }
    }
  }

  // ---------------------------------------------------------------------------
  // Shutdown
  // ---------------------------------------------------------------------------

  /**
   * Performs ordered teardown of all subsystems.
   * Safe to call multiple times; shutdownOnce ensures single execution.
   */
  private async shutdown(): Promise<void> {
    if (this.shutdownRan) return;
    this.shutdownRan = true;

    // 1. Close the connection (stops read/write pumps).
    this.connMgr.close();

    // 2. Stop sync and retry managers.
    this.syncMgr.stop();
    this.retryMgr.stop();

    // 3. Fail all pending RPCs.
    for (const [id, resolve] of this.pending) {
      resolve({
        id,
        code: ErrorCodeConnectionError,
        msg: 'client shutting down',
        data: null,
      });
    }
    this.pending.clear();

    // 4. Close the database.
    this.db.close();

    // 5. Resolve done promise.
    if (this.doneResolve) {
      this.doneResolve();
    }

    this.logger.info('Client stopped');
  }
}

// ---------------------------------------------------------------------------
// Module-level helper functions
// ---------------------------------------------------------------------------

/**
 * Wraps a Promise with a timeout. Rejects with a TimeoutError if the timeout
 * elapses before the Promise settles.
 */
function withTimeout<T>(promise: Promise<T>, ms: number): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const timer = setTimeout(() => {
      reject(new TimeoutError(`Operation timed out after ${ms}ms`));
    }, ms);

    promise.then(
      (value) => {
        clearTimeout(timer);
        resolve(value);
      },
      (error) => {
        clearTimeout(timer);
        reject(error);
      },
    );
  });
}

/**
 * Sleeps for the specified duration, or resolves immediately if the abort
 * signal is triggered.
 */
function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise<void>((resolve) => {
    if (signal?.aborted) {
      resolve();
      return;
    }
    const timer = setTimeout(() => {
      if (signal) {
        signal.removeEventListener('abort', onAbort);
      }
      resolve();
    }, ms);

    const onAbort = () => {
      clearTimeout(timer);
      resolve();
    };

    if (signal) {
      signal.addEventListener('abort', onAbort, { once: true });
    }
  });
}
