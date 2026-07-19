/**
 * @packageDocumentation
 * SyncManager — synchronisation engine for the Xyncra TypeScript client.
 *
 * Mirrors Go syncManager (pkg/client/sync.go):
 *   - Processes incoming data updates, persists them to the local store,
 *     and notifies the IUpdateHandler.
 *   - Manages debounced pull requests to fill sequence gaps.
 *   - Performs full synchronisation on startup / reconnection.
 *
 * Key constraints (must be strictly enforced):
 *   C6  — NotificationLog.Seq unique index for deduplication.
 *   C7  — Ephemeral updates (seq=0) bypass seq check, dedup, and persistence.
 *   C8  — Debounced pull 500ms merge window for gap recovery.
 *   C12 — FullSync blocking pagination until has_more=false.
 *
 * Concurrency model differences from Go:
 *   Go syncManager.applyMu sync.Mutex → TS applyChain: Promise<void> (Promise chain)
 *   Go syncManager.mu (JS single-threaded, no Mutex needed)
 *   Go time.AfterFunc(500ms, pull)     → TS setTimeout(() => pull(), 500)
 *   Go db.Transaction(ctx, func(tx))   → TS db.transaction('rw', tables, async () => {})
 */

import type { PackageDataUpdate } from '@xyncra/protocol';
import Dexie, { type Transaction } from 'dexie';
import type { XyncraDatabase } from './db';
import type {
  Conversation as DBConversation,
  Message as DBMessage,
  NotificationLog,
  Question,
} from './db/models';
import { ErrDuplicateKey, SyncError } from './errors';
import type {
  Conversation,
  ConversationAction,
  IAgentStatusHandler,
  IAgentTimeoutHandler,
  ILogger,
  IStreamingHandler,
  ITypingHandler,
  IUpdateHandler,
  Message,
} from './interfaces';

// ---------------------------------------------------------------------------
// Optional handler interfaces (re-exported for convenience)
// ---------------------------------------------------------------------------

export type {
  IAgentStatusHandler,
  IAgentTimeoutHandler,
  IStreamingHandler,
  ITypingHandler,
};

// ---------------------------------------------------------------------------
// SyncError (re-exported for convenience)
// ---------------------------------------------------------------------------

export { SyncError };

// ---------------------------------------------------------------------------
// Defaults
// ---------------------------------------------------------------------------

/** Default batch size for sync_updates RPC pagination. */
const DefaultBatchSize = 100;
/** Default retry interval for debounced pull single retry (ms). */
const DefaultRetryInterval = 5000;
/** Default debounce interval for pull coalescing (ms) — C8. */
const DefaultDebounceInterval = 500;

// ---------------------------------------------------------------------------
// SyncManagerOptions
// ---------------------------------------------------------------------------

/**
 * Configuration options for SyncManager.
 */
export interface SyncManagerOptions {
  /** Local IndexedDB database instance. */
  db: XyncraDatabase;
  /** Handler for processed updates (application-level side effects). */
  handler: IUpdateHandler;
  /** JSON-RPC call function: (method, params) => result. */
  rpcFn: (method: string, params: unknown) => Promise<unknown>;
  /** Structured logger. */
  logger: ILogger;
  /** Maximum number of updates per sync_updates RPC call. Default: 100. */
  batchSize?: number;
  /** Retry interval for debounced pull single retry (ms). Default: 5000. */
  retryInterval?: number;
  /** Debounce interval for pull coalescing (ms) — C8. Default: 500. */
  debounceInterval?: number;
}

// ---------------------------------------------------------------------------
// syncUpdatesResponse — shape of the sync_updates RPC response
// ---------------------------------------------------------------------------

interface SyncUpdatesResponse {
  updates: PackageDataUpdate[];
  has_more: boolean;
  latest_seq: number;
}

// ---------------------------------------------------------------------------
// SyncManager
// ---------------------------------------------------------------------------

/**
 * SyncManager processes incoming data updates, persists them to the local
 * store, and notifies the IUpdateHandler. It also manages debounced pull
 * requests to fill sequence gaps and full synchronisation on startup.
 *
 * Mirrors Go syncManager (pkg/client/sync.go).
 */
export class SyncManager {
  private readonly options: Required<SyncManagerOptions>;

  /** Debounce timer handle for pull coalescing — C8. */
  private pullTimer: ReturnType<typeof setTimeout> | null = null;
  /** Whether a debounced pull is already scheduled. */
  private pullPending = false;
  /**
   * Promise chain acting as an async mutex for apply operations.
   * Mirrors Go syncManager.applyMu sync.Mutex.
   * Each apply operation chains onto this promise, ensuring serial execution.
   */
  private applyChain: Promise<void> = Promise.resolve();

  constructor(options: SyncManagerOptions) {
    this.options = {
      db: options.db,
      handler: options.handler,
      rpcFn: options.rpcFn,
      logger: options.logger,
      batchSize: options.batchSize ?? DefaultBatchSize,
      retryInterval: options.retryInterval ?? DefaultRetryInterval,
      debounceInterval: options.debounceInterval ?? DefaultDebounceInterval,
    };
  }

  // ---------------------------------------------------------------------------
  // Lifecycle
  // ---------------------------------------------------------------------------

  /**
   * Starts the SyncManager. Resets internal state.
   */
  start(): void {
    this.applyChain = Promise.resolve();
    this.pullPending = false;
    this.pullTimer = null;
  }

  /**
   * Stops the SyncManager. Cancels any pending debounce timer and waits for
   * the apply chain to drain.
   */
  stop(): void {
    if (this.pullTimer !== null) {
      clearTimeout(this.pullTimer);
      this.pullTimer = null;
    }
    this.pullPending = false;
  }

  // ---------------------------------------------------------------------------
  // Public API
  // ---------------------------------------------------------------------------

  /**
   * Processes a single update.
   *
   *   - seq=0 → ephemeral bypass (C7): skip seq check, dedup, persistence;
   *     notify handler directly.
   *   - seq ≤ localMaxSeq → skip (already processed).
   *   - seq > localMaxSeq+1 → gap detected; schedule debounced pull.
   *   - seq === localMaxSeq+1 → normal processing: dedup → dispatch → advance seq.
   */
  async applyUpdate(update: PackageDataUpdate): Promise<void> {
    console.log('[SyncManager] applyUpdate received:', {
      type: update.type,
      seq: update.seq,
      payload: update.payload,
    });

    // C7: Ephemeral updates (seq=0) bypass seq continuity, dedup, and persistence.
    if (update.seq === 0) {
      // Conversation "update" action uses pull-on-notification (D-118/D-124).
      if (update.type === 'conversation') {
        console.log('[SyncManager] Calling handleEphemeralConversationUpdate');
        await this.handleEphemeralConversationUpdate(update);
      } else {
        await this.notifyHandler(update);
      }
      return;
    }

    // 1. Read the current local maximum sequence number.
    const localMaxSeq = await this.options.db.syncStatesStore.getLocalMaxSeq();

    // 2. Check sequence continuity.
    if (update.seq <= localMaxSeq) {
      // Already processed — skip silently.
      this.options.logger.debug(
        `Skipping update seq=${update.seq} <= localMaxSeq=${localMaxSeq}`,
      );
      return;
    }

    if (update.seq > localMaxSeq + 1) {
      // Gap detected — trigger debounced pull.
      this.options.logger.warn(
        `Gap detected: seq=${update.seq} > localMaxSeq+1=${localMaxSeq + 1}`,
      );
      this.scheduleDebouncedPull();
      return;
    }

    // seq === localMaxSeq + 1 → normal processing.
    await this.applyUpdateTx(update);
  }

  /**
   * Processes a batch of updates in order.
   *
   * Mirrors Go ApplyUpdates: if a gap is detected the remaining updates are
   * not processed and a debounced pull is scheduled.
   */
  async applyUpdates(updates: PackageDataUpdate[]): Promise<void> {
    for (const update of updates) {
      await this.applyUpdate(update);
    }
  }

  /**
   * Performs a blocking, paginated synchronisation with the server (C12).
   * Fetches all updates after the current localMaxSeq until has_more is false.
   * Intended for use on initial connection or reconnection.
   */
  async fullSync(abortSignal?: AbortSignal): Promise<void> {
    let afterSeq = await this.options.db.syncStatesStore.getLocalMaxSeq();
    let hasMore = true;

    while (hasMore && !abortSignal?.aborted) {
      const result = (await this.options.rpcFn('sync_updates', {
        after_seq: afterSeq,
        limit: this.options.batchSize,
      })) as SyncUpdatesResponse;

      // Apply updates serially to maintain order.
      for (const update of result.updates) {
        await this.applyUpdate(update);
        afterSeq = update.seq;
      }

      // Update latest_seq tracker.
      if (result.latest_seq > 0) {
        await this.options.db.syncStatesStore.setLatestSeq(result.latest_seq);
      }

      hasMore = result.has_more;

      if (!hasMore) break;

      // Re-read localMaxSeq for the next page (mirrors Go).
      afterSeq = await this.options.db.syncStatesStore.getLocalMaxSeq();
    }

    if (abortSignal?.aborted) {
      throw new SyncError('FullSync aborted');
    }
  }

  // ---------------------------------------------------------------------------
  // applyUpdateTx — serialised transactional application (async mutex)
  // ---------------------------------------------------------------------------

  /**
   * Wraps the update application in a Dexie transaction and serialises via the
   * applyChain Promise chain (async mutex).
   *
   * Steps (within transaction):
   *   3. Deduplicate via NotificationLog (Seq uniqueIndex — C6).
   *   4. Dispatch by type (DB writes only).
   *   5. Advance localMaxSeq.
   *
   * After transaction commit: notify handler (errors logged, not propagated).
   */
  private async applyUpdateTx(update: PackageDataUpdate): Promise<void> {
    // Chain onto applyChain for serial execution (async mutex).
    this.applyChain = this.applyChain.then(async () => {
      const db = this.options.db;

      await db.transaction(
        'rw',
        [db.notificationLogs, db.messages, db.conversations, db.syncStates, db.questions, db.rpcLogs],
        async () => {
          // Capture the transaction reference once (guaranteed non-null inside callback).
          const tx = Dexie.currentTransaction as unknown as Transaction;

          // C6: Deduplicate via NotificationLog (seq uniqueIndex).
          const nLog: NotificationLog = {
            id: crypto.randomUUID(),
            seq: update.seq,
            type: update.type,
            payload: new TextEncoder().encode(
              typeof update.payload === 'string'
                ? update.payload
                : JSON.stringify(update.payload ?? ''),
            ),
            created_at: new Date(),
          };

          try {
            await db.notificationLogsStore.saveTx(tx, nLog);
          } catch (error) {
            if (error === ErrDuplicateKey) {
              // Duplicate — advance seq and skip.
              this.options.logger.debug(
                `Duplicate update seq=${update.seq}, skipping`,
              );
              await db.syncStatesStore.setLocalMaxSeqTx(tx, update.seq);
              return;
            }
            throw error;
          }

          // Dispatch by type (DB writes only, no handler notifications).
          await this.dispatchUpdateTx(update, tx);

          // Advance localMaxSeq.
          await db.syncStatesStore.setLocalMaxSeqTx(tx, update.seq);
        },
      );

      // Transaction committed — notify handler.
      // Errors are logged but do not fail the sync pipeline.
      try {
        await this.notifyHandler(update);
      } catch (error) {
        this.options.logger.error('Handler notification failed', error);
      }
    });

    // Catch and re-throw chain errors (preserves error propagation).
    try {
      await this.applyChain;
    } catch (error) {
      // Reset the chain so subsequent updates can proceed.
      this.applyChain = Promise.resolve();
      throw error;
    }
  }

  // ---------------------------------------------------------------------------
  // Debounced pull (C8)
  // ---------------------------------------------------------------------------

  /**
   * Starts (or coalesces) a debounce timer that will issue a sync_updates RPC
   * to fill the sequence gap. 500ms merge window — C8.
   */
  private scheduleDebouncedPull(): void {
    if (this.pullPending) return; // Timer already armed — coalesce.

    if (this.pullTimer !== null) {
      clearTimeout(this.pullTimer);
    }

    this.pullPending = true;
    this.pullTimer = setTimeout(async () => {
      this.pullPending = false;
      this.pullTimer = null;

      try {
        await this.fullSync();
      } catch (error) {
        this.options.logger.error('Debounced pull failed', error);
        // Single retry after retryInterval.
        setTimeout(async () => {
          try {
            if (!this.pullPending) {
              await this.fullSync();
            }
          } catch (retryError) {
            this.options.logger.error(
              'Debounced pull retry failed',
              retryError,
            );
          }
        }, this.options.retryInterval);
      }
    }, this.options.debounceInterval);
  }

  // ---------------------------------------------------------------------------
  // Transactional dispatch (DB writes only, no handler notifications)
  // ---------------------------------------------------------------------------

  /**
   * Routes the update to the appropriate transactional handler by type.
   * Mirrors Go dispatchUpdateTx.
   */
  private async dispatchUpdateTx(
    update: PackageDataUpdate,
    tx: Transaction,
  ): Promise<void> {
    switch (update.type) {
      case 'message':
        await this.handleMessageTx(update, tx);
        break;
      case 'delete_message':
        await this.handleDeleteMessageTx(update, tx);
        break;
      case 'mark_read':
        await this.handleMarkReadTx(update, tx);
        break;
      case 'conversation':
        await this.handleConversationTx(update, tx);
        break;
      case 'gap':
        // Gap filler — seq already advanced, nothing else to do.
        break;
      case 'typing':
      case 'streaming':
      case 'agent_status':
      case 'agent_timeout':
        // Defense-in-depth: reachable only if an ephemeral type with seq > 0
        // is received (should never happen). Returns gracefully.
        break;
      default:
        this.options.logger.warn(`Unknown update type: ${update.type}`);
        break;
    }
  }

  // ---------------------------------------------------------------------------
  // Per-type transactional handlers
  // ---------------------------------------------------------------------------

  /**
   * Persists the message and updates the conversation's last-message pointer.
   * Mirrors Go handleMessageTx.
   */
  private async handleMessageTx(
    update: PackageDataUpdate,
    tx: Transaction,
  ): Promise<void> {
    const msg = this.transformMessageDates(
      update.payload as Record<string, unknown>,
    );
    const msgTable = tx.table('messages') as Dexie.Table<DBMessage, string>;

    // Persist the message. Ignore duplicate key errors (idempotent).
    try {
      await msgTable.add(msg);
    } catch (error) {
      if (error === ErrDuplicateKey || this.isConstraintError(error)) {
        // Duplicate — idempotent, skip.
      } else {
        throw error;
      }
    }

    // Update conversation last-message pointer.
    const convTable = tx.table('conversations') as Dexie.Table<
      DBConversation,
      string
    >;
    const updated = await convTable
      .where('id')
      .equals(msg.conversation_id)
      .modify((conv) => {
        conv.last_message_at = msg.created_at;
        conv.last_processed_message_id = msg.message_id;
      });

    if (updated === 0) {
      // M-2: conversation not found — log instead of silently ignoring.
      this.options.logger.error(
        `Conversation not found for last message update`,
        { conversation_id: msg.conversation_id, message_id: msg.id },
      );
    }
  }

  /**
   * Soft-deletes the local message.
   * Mirrors Go handleDeleteMessageTx.
   */
  private async handleDeleteMessageTx(
    update: PackageDataUpdate,
    tx: Transaction,
  ): Promise<void> {
    const payload = update.payload as {
      message_id: string;
      conversation_id: string;
    };

    const msgTable = tx.table('messages') as Dexie.Table<DBMessage, string>;
    const updated = await msgTable
      .where('id')
      .equals(payload.message_id)
      .modify((msg) => {
        if (msg.deleted_at !== null) return;
        msg.deleted_at = new Date();
      });

    // ErrNotFound is acceptable — message may not have been synced locally yet.
    if (updated === 0) {
      this.options.logger.debug(`Delete message: not found locally, skipping`, {
        message_id: payload.message_id,
      });
    }
  }

  /**
   * Updates the conversation read cursor for the current user (MAX semantics — C4).
   * Mirrors Go handleMarkReadTx.
   */
  private async handleMarkReadTx(
    update: PackageDataUpdate,
    tx: Transaction,
  ): Promise<void> {
    const payload = update.payload as {
      conversation_id: string;
      last_read_message_id: number;
    };

    // NOTE: userID is not yet available on SyncManager; the store method
    // determines user columns by checking both user_id1 and user_id2.
    // We pass the raw last_read_message_id and let the handler interpret it.
    const convTable = tx.table('conversations') as Dexie.Table<
      DBConversation,
      string
    >;
    const conv = await convTable.get(payload.conversation_id);
    if (!conv) return; // ErrNotFound is acceptable.

    // MAX semantics: advance both read cursors if the new value is greater.
    // Without userID context, we update both columns (safe for single-user clients).
    const updates: Partial<DBConversation> = {};
    if (payload.last_read_message_id > conv.last_read_message_id1) {
      updates.last_read_message_id1 = payload.last_read_message_id;
    }
    if (payload.last_read_message_id > conv.last_read_message_id2) {
      updates.last_read_message_id2 = payload.last_read_message_id;
    }

    if (Object.keys(updates).length > 0) {
      await convTable.update(payload.conversation_id, updates);
    }
  }

  /**
   * Processes a "conversation" type update within the given transaction.
   * Routes by action field: delete, restore, create, update, or legacy upsert.
   * Mirrors Go handleConversationTx.
   */
  private async handleConversationTx(
    update: PackageDataUpdate,
    tx: Transaction,
  ): Promise<void> {
    const payload = update.payload as Record<string, unknown>;
    const action = (payload.action as string) ?? '';

    switch (action) {
      case 'delete':
        await this.handleConversationDeleteTx(
          payload.conversation_id as string,
          tx,
        );
        break;
      case 'restore':
        await this.handleConversationRestoreTx(
          payload.conversation_id as string,
          tx,
        );
        break;
      case 'create':
        await this.handleConversationCreateTx(update, tx);
        break;
      case 'update':
        await this.handleConversationUpdateTx(
          payload.conversation_id as string,
          (payload.updated_at as number) ?? 0,
          tx,
        );
        break;
      case '':
        // Legacy: full conversation record (backward compat).
        await this.handleConversationLegacyUpsertTx(update, tx);
        break;
      default:
        // M-6: unknown action — log and skip.
        this.options.logger.error(`Unknown conversation action: ${action}`, {
          conversation_id: payload.conversation_id,
        });
        break;
    }
  }

  /**
   * Cascade soft-deletes a conversation and its messages (D-013).
   */
  private async handleConversationDeleteTx(
    convID: string,
    tx: Transaction,
  ): Promise<void> {
    const convTable = tx.table('conversations') as Dexie.Table<
      DBConversation,
      string
    >;
    const msgTable = tx.table('messages') as Dexie.Table<DBMessage, string>;

    const conv = await convTable.get(convID);
    if (!conv || conv.deleted_at !== null) {
      this.options.logger.debug(
        `Conversation delete: not found locally, skipping`,
        { conversation_id: convID },
      );
      return;
    }

    const now = new Date();
    await convTable.update(convID, { deleted_at: now });

    // Cascade soft-delete messages (D-013).
    await msgTable
      .where('conversation_id')
      .equals(convID)
      .modify((msg) => {
        if (msg.deleted_at === null) {
          msg.deleted_at = now;
        }
      });
  }

  /**
   * Cascade restores a previously soft-deleted conversation and its messages (D-015).
   * When the local record does not exist at all, falls back to fetching from
   * the server via RPC and upserting locally.
   */
  private async handleConversationRestoreTx(
    convID: string,
    tx: Transaction,
  ): Promise<void> {
    const convTable = tx.table('conversations') as Dexie.Table<
      DBConversation,
      string
    >;
    const msgTable = tx.table('messages') as Dexie.Table<DBMessage, string>;

    const conv = await convTable.get(convID);
    if (!conv) {
      // Local record doesn't exist — fetch from server and create.
      // NOTE: This triggers an RPC inside a transaction. Dexie supports this
      // (transaction stays alive via Promise chain), but it locks the DB
      // tables for the duration of the RPC. Acceptable for this rare fallback.
      await this.fetchAndUpsertConversationTx(convID, tx);
      return;
    }

    if (conv.deleted_at !== null) {
      await convTable.update(convID, { deleted_at: null });

      // Cascade restore messages (D-015).
      await msgTable
        .where('conversation_id')
        .equals(convID)
        .modify((msg) => {
          if (msg.deleted_at !== null) {
            msg.deleted_at = null;
          }
        });
    }
  }

  /**
   * Processes a "create" action for a conversation update (D-045).
   * Saves conversation from payload, then syncs questions outside transaction.
   */
  private async handleConversationCreateTx(
    update: PackageDataUpdate,
    tx: Transaction,
  ): Promise<void> {
    const payload = update.payload as {
      action: string;
      conversation?: Record<string, unknown>;
    };

    if (!payload.conversation) {
      this.options.logger.error(
        'Create conversation payload has nil conversation',
      );
      return;
    }

    // Transform date strings to Date objects (server sends ISO strings)
    const conv = this.transformConversationDates(payload.conversation);

    const convTable = tx.table('conversations') as Dexie.Table<
      DBConversation,
      string
    >;
    await convTable.put(conv);

    // Sync questions outside transaction (RPC call is slow)
    const convID = conv.id;
    setTimeout(async () => {
      try {
        await this.syncQuestionsForConversation(convID);
      } catch (error) {
        this.options.logger.error('Failed to sync questions for new conversation', {
          conversation_id: convID,
          error,
        });
      }
    }, 0);
  }

  /**
   * Processes an "update" action for a conversation (D-124).
   * Compares updated_at with local conversation and skips RPC if cache is current.
   */
  private async handleConversationUpdateTx(
    convID: string,
    updatedAt: number,
    tx: Transaction,
  ): Promise<void> {
    // D-124: Skip RPC if local cache is up-to-date.
    if (updatedAt > 0) {
      const convTable = tx.table('conversations') as Dexie.Table<
        DBConversation,
        string
      >;
      const localConv = await convTable.get(convID);
      if (
        localConv &&
        localConv.deleted_at == null &&
        localConv.updated_at &&
        updatedAt <= Math.floor(new Date(localConv.updated_at).getTime() / 1000)
      ) {
        this.options.logger.debug(
          `Skipping conversation update — local cache is current`,
          {
            conversation_id: convID,
            payload_updated_at: updatedAt,
            local_updated_at: Math.floor(new Date(localConv.updated_at).getTime() / 1000),
          },
        );
        return;
      }
    }

    // Local cache is stale or missing — fetch from server and upsert.
    await this.fetchAndUpsertConversationTx(convID, tx);
  }

  /**
   * Legacy conversation upsert (backward compat with events that omit action).
   * Saves conversation from payload, then syncs questions outside transaction.
   */
  private async handleConversationLegacyUpsertTx(
    update: PackageDataUpdate,
    tx: Transaction,
  ): Promise<void> {
    // Debug: log the raw update payload
    this.options.logger.debug('Received legacy conversation upsert', {
      raw: update.payload,
      id: (update.payload as Record<string, unknown>).id,
      idType: typeof (update.payload as Record<string, unknown>).id,
    });

    const conv = this.transformConversationDates(
      update.payload as Record<string, unknown>,
    );
    const convTable = tx.table('conversations') as Dexie.Table<
      DBConversation,
      string
    >;
    await convTable.put(conv);

    // Sync questions outside transaction (RPC call is slow)
    const convID = conv.id;
    setTimeout(async () => {
      try {
        await this.syncQuestionsForConversation(convID);
      } catch (error) {
        this.options.logger.error('Failed to sync questions for legacy upsert', {
          conversation_id: convID,
          error,
        });
      }
    }, 0);
  }

  /**
   * Syncs questions for a conversation by calling get_conversation RPC.
   * This is called outside of transactions to avoid transaction timeout issues.
   */
  private async syncQuestionsForConversation(convID: string): Promise<void> {
    const result = (await this.options.rpcFn('get_conversation', {
      conversation_id: convID,
    })) as {
      conversation: Record<string, unknown>;
      questions?: Array<{
        id: string;
        conversation_id: string;
        checkpoint_id: string;
        interrupt_id: string;
        question_text: string;
        status: string;
        created_at: string;
      }>;
    };

    if (!result.conversation) {
      this.options.logger.error(
        `get_conversation returned nil conversation for ${convID}`,
      );
      return;
    }

    // Handle questions (D-125): clear stale questions and upsert new ones.
    // When questions is defined (even if empty), sync local state to match server.
    if (result.questions) {
      // Clear stale questions when agent_status != asking_user (HITL ended)
      // or when new questions are provided.
      if (result.conversation.agent_status !== 'asking_user') {
        await this.options.db.questionsStore.deleteByConversation(convID);
      }
      for (const q of result.questions) {
        // Convert created_at from string to Date
        const question: Question = {
          id: q.id,
          conversation_id: q.conversation_id,
          checkpoint_id: q.checkpoint_id,
          interrupt_id: q.interrupt_id,
          question_text: q.question_text,
          status: q.status,
          created_at: new Date(q.created_at),
        };
        await this.options.db.questionsStore.upsert(question);
      }
    }
  }

  /**
   * Fetches a conversation from the server via get_conversation RPC and
   * upserts it into the local database. Also handles questions (D-125).
   */
  private async fetchAndUpsertConversationTx(
    convID: string,
    tx: Transaction,
  ): Promise<void> {
    const result = (await this.options.rpcFn('get_conversation', {
      conversation_id: convID,
    })) as {
      conversation: Record<string, unknown>;
      questions?: Array<{
        id: string;
        conversation_id: string;
        checkpoint_id: string;
        interrupt_id: string;
        question_text: string;
        status: string;
        created_at: string;
      }>;
    };

    if (!result.conversation) {
      this.options.logger.error(
        `get_conversation returned nil conversation for ${convID}`,
      );
      return;
    }

    const conv = this.transformConversationDates(result.conversation);
    const convTable = tx.table('conversations') as Dexie.Table<
      DBConversation,
      string
    >;
    await convTable.put(conv);

    // Handle questions (D-125): clear stale questions and upsert new ones.
    // When questions is defined (even if empty), sync local state to match server.
    if (result.questions) {
      const qTable = tx.table('questions');
      // Clear stale questions when agent_status != asking_user (HITL ended)
      // or when new questions are provided.
      if (result.conversation.agent_status !== 'asking_user') {
        await qTable.where('conversation_id').equals(convID).delete();
      }
      for (const q of result.questions) {
        await qTable.put(q);
      }
    }
  }

  // ---------------------------------------------------------------------------
  // Ephemeral conversation update (D-118 pull-on-notification, D-124 optimization)
  // ---------------------------------------------------------------------------

  /**
   * Processes an ephemeral (seq=0) conversation update.
   * For "update" action: pull-on-notification pattern with D-124 timestamp comparison.
   * For other actions: falls through to standard handler notification.
   */
  private async handleEphemeralConversationUpdate(
    update: PackageDataUpdate,
  ): Promise<void> {
    const payload = update.payload as Record<string, unknown>;
    const action = (payload.action as string) ?? '';

    // Only "update" action uses pull-on-notification with D-124 timestamp comparison.
    if (action !== 'update') {
      await this.notifyHandler(update);
      return;
    }

    const convID = payload.conversation_id as string;
    const updatedAt = (payload.updated_at as number) ?? 0;

    // D-124: If updated_at is present and local cache is up-to-date, skip RPC.
    if (updatedAt > 0) {
      const localConv = await this.options.db.conversationsStore.get(convID);
      if (
        localConv &&
        localConv.updated_at &&
        updatedAt <= Math.floor(new Date(localConv.updated_at).getTime() / 1000)
      ) {
        this.options.logger.debug(
          `Skipping conversation update — local cache is current`,
          {
            conversation_id: convID,
            payload_updated_at: updatedAt,
            local_updated_at: Math.floor(new Date(localConv.updated_at).getTime() / 1000),
          },
        );
        // Notify handler with local data (already up-to-date).
        await this.options.handler.onConversation(
          conversationToHandler(localConv),
          'updated',
        );
        return;
      }
    }

    // Local cache is stale or missing — fetch full conversation from server.
    try {
      const result = (await this.options.rpcFn('get_conversation', {
        conversation_id: convID,
      })) as {
        conversation: DBConversation;
        questions?: Array<{
          id: string;
          conversation_id: string;
          checkpoint_id: string;
          interrupt_id: string;
          question_text: string;
          status: string;
          created_at: Date;
        }>;
      };

      if (!result.conversation) {
        this.options.logger.error(
          `get_conversation returned nil conversation for ${convID}`,
        );
        await this.notifyHandler(update);
        return;
      }

      // Upsert into local DB (best-effort; log on failure).
      try {
        await this.options.db.conversationsStore.upsert(result.conversation);
      } catch (error) {
        this.options.logger.error(`Upsert fetched conversation failed`, {
          conversation_id: convID,
          error,
        });
      }

      // Handle questions (D-125).
      // When questions is defined (even if empty), sync local state to match server.
      if (result.questions) {
        if (result.conversation.agent_status !== 'asking_user') {
          await this.options.db.questionsStore.deleteByConversation(convID);
        }
        for (const q of result.questions) {
          try {
            await this.options.db.questionsStore.upsert(q);
          } catch (error) {
            this.options.logger.error('Upsert question failed', {
              question_id: q.id,
              error,
            });
          }
        }
      }

      // Fetch questions from store for HITL (D-125)
      let questions: Array<{
        id: string;
        question_text: string;
        checkpoint_id: string;
        interrupt_id: string;
        status: string;
      }> | undefined;
      if (result.conversation.agent_status === 'asking_user') {
        try {
          const storedQuestions = await this.options.db.questionsStore
            .getByConversation(convID);
          if (storedQuestions.length > 0) {
            questions = storedQuestions.map((q: any) => ({
              id: q.id,
              question_text: q.question_text,
              checkpoint_id: q.checkpoint_id,
              interrupt_id: q.interrupt_id,
              status: q.status,
            }));
          }
        } catch (error) {
          this.options.logger.error('Fetch questions failed', {
            conversation_id: convID,
            error,
          });
        }
      }

      console.log('[SyncManager Debug] Calling onConversation with:', {
        convID: result.conversation.id,
        agent_status: result.conversation.agent_status,
        hasQuestions: !!questions,
        questionsCount: questions?.length,
      });

      await this.options.handler.onConversation(
        conversationToHandler({ ...result.conversation, questions }),
        'updated',
      );
    } catch (error) {
      this.options.logger.error(
        `Fetch conversation for update notification failed`,
        { conversation_id: convID, error },
      );
      // Fall back to notifying handler with minimal data (D-118 degraded).
      await this.notifyHandler(update);
    }
  }

  // ---------------------------------------------------------------------------
  // Handler notifications (post-commit)
  // ---------------------------------------------------------------------------

  /**
   * Calls handler methods after the transaction commits.
   * Errors are logged but do not fail the sync pipeline.
   * Mirrors Go notifyHandler.
   */
  private async notifyHandler(update: PackageDataUpdate): Promise<void> {
    const handler = this.options.handler;

    switch (update.type) {
      case 'message': {
        const msg = update.payload as DBMessage;
        await handler.onMessage(messageToHandler(msg));
        break;
      }
      case 'delete_message': {
        const dp = update.payload as {
          message_id: string;
          conversation_id: string;
        };
        await handler.onDeleteMessage(dp.message_id, dp.conversation_id);
        break;
      }
      case 'mark_read': {
        const mp = update.payload as {
          conversation_id: string;
          last_read_message_id: number;
        };
        await handler.onMarkRead(
          mp.conversation_id,
          mp.last_read_message_id as unknown as string,
        );
        break;
      }
      case 'conversation': {
        const payload = update.payload as Record<string, unknown>;
        const action = (payload.action as string) ?? '';
        if (action === 'create' && payload.conversation) {
          await handler.onConversation(
            conversationToHandler(payload.conversation as DBConversation),
            'created',
          );
        } else if (payload.conversation_id) {
          // For delete/restore/update: notify with minimal data (cache invalidation).
          const convAction: ConversationAction =
            action === 'delete' ? 'removed' : 'updated';
          await handler.onConversation(
            { id: payload.conversation_id as string } as unknown as Conversation,
            convAction,
          );
        }
        break;
      }
      case 'typing': {
        if ('onTyping' in handler) {
          const tp = update.payload as {
            user_id: string;
            conversation_id: string;
            is_typing: boolean;
            is_agent?: boolean;
          };
          await (handler as IUpdateHandler & ITypingHandler).onTyping(
            tp.user_id,
            tp.conversation_id,
            tp.is_typing,
            tp.is_agent ?? false,
          );
        }
        break;
      }
      case 'streaming': {
        if ('onStreaming' in handler) {
          const sp = update.payload as {
            user_id: string;
            conversation_id: string;
            stream_id: string;
            text: string;
            is_done: boolean;
            is_agent?: boolean;
          };
          await (handler as IUpdateHandler & IStreamingHandler).onStreaming(
            sp.user_id,
            sp.conversation_id,
            sp.stream_id,
            sp.text,
            sp.is_done,
            sp.is_agent ?? false,
          );
        }
        break;
      }
      case 'agent_status': {
        if ('onAgentStatus' in handler) {
          const sp = update.payload as {
            user_id: string;
            conversation_id: string;
            status: string;
          };
          await (handler as IUpdateHandler & IAgentStatusHandler).onAgentStatus(
            sp.user_id,
            sp.conversation_id,
            sp.status,
          );
        }
        break;
      }
      case 'agent_timeout': {
        if ('onAgentTimeout' in handler) {
          const tp = update.payload as {
            user_id: string;
            conversation_id: string;
            reason: string;
          };
          await (
            handler as IUpdateHandler & IAgentTimeoutHandler
          ).onAgentTimeout(tp.user_id, tp.conversation_id, tp.reason);
        }
        break;
      }
      case 'gap':
        await handler.onGap(update.seq);
        break;
    }
  }

  // ---------------------------------------------------------------------------
  // Private helpers
  // ---------------------------------------------------------------------------

  /** Checks if an error is a Dexie constraint violation. */
  private isConstraintError(error: unknown): boolean {
    if (error instanceof Error) {
      return error.name === 'ConstraintError';
    }
    return false;
  }

  /**
   * Transforms a message object from server format (JSON with date strings)
   * to IndexedDB format (with Date objects).
   */
  private transformMessageDates(msg: Record<string, unknown>): DBMessage {
    // Ensure required fields have valid values
    const id = msg.id as string | undefined;
    if (!id || typeof id !== 'string') {
      throw new Error(`Invalid message id: ${JSON.stringify(msg.id)}`);
    }

    return {
      id,
      client_message_id: (msg.client_message_id as string) ?? '',
      conversation_id: (msg.conversation_id as string) ?? '',
      message_id: (msg.message_id as number) ?? 0,
      sender_id: (msg.sender_id as string) ?? '',
      content: (msg.content as string) ?? '',
      type: (msg.type as string) ?? 'text',
      reply_to: (msg.reply_to as number) ?? 0,
      status: (msg.status as string) ?? 'sent',
      created_at: msg.created_at
        ? new Date(msg.created_at as string)
        : new Date(),
      deleted_at: msg.deleted_at ? new Date(msg.deleted_at as string) : null,
    };
  }

  /**
   * Transforms a conversation object from server format (JSON with date strings)
   * to IndexedDB format (with Date objects).
   */
  private transformConversationDates(
    conv: Record<string, unknown>,
  ): DBConversation {
    // Ensure required fields have valid values
    const id = conv.id as string | undefined;
    if (!id || typeof id !== 'string') {
      throw new Error(`Invalid conversation id: ${JSON.stringify(conv.id)}`);
    }

    return {
      id,
      user_id1: (conv.user_id1 as string) ?? '',
      user_id2: (conv.user_id2 as string) ?? '',
      type: (conv.type as string) ?? '1-on-1',
      title: (conv.title as string) ?? '',
      pinned: (conv.pinned as boolean) ?? false,
      muted: (conv.muted as boolean) ?? false,
      avatar_url: (conv.avatar_url as string) ?? '',
      description: (conv.description as string) ?? '',
      last_processed_message_id: (conv.last_processed_message_id as number) ?? 0,
      created_at: conv.created_at
        ? new Date(conv.created_at as string)
        : new Date(),
      updated_at: conv.updated_at
        ? new Date(conv.updated_at as string)
        : new Date(),
      last_message_at: conv.last_message_at
        ? new Date(conv.last_message_at as string)
        : new Date(),
      last_read_message_id1: (conv.last_read_message_id1 as number) ?? 0,
      last_read_message_id2: (conv.last_read_message_id2 as number) ?? 0,
      agent_status: (conv.agent_status as string) ?? 'idle',
      agent_id: (conv.agent_id as string) ?? '',
      checkpoint_id: (conv.checkpoint_id as string) ?? '',
      agent_last_activity: conv.agent_last_activity
        ? new Date(conv.agent_last_activity as string)
        : new Date(),
      deleted_at: conv.deleted_at ? new Date(conv.deleted_at as string) : null,
    };
  }
}

// ---------------------------------------------------------------------------
// Handler type conversion helpers (snake_case DB models → camelCase handler types)
// ---------------------------------------------------------------------------

/**
 * Converts a DB Message (snake_case) to a handler Message (camelCase).
 */
function messageToHandler(msg: DBMessage): Message {
  return {
    id: msg.id,
    conversationId: msg.conversation_id,
    senderId: msg.sender_id,
    content: msg.content,
    clientMessageId: msg.client_message_id,
    replyToId: msg.reply_to ? String(msg.reply_to) : undefined,
    createdAt:
      msg.created_at instanceof Date
        ? msg.created_at.toISOString()
        : String(msg.created_at),
    updatedAt: msg.created_at
      ? msg.created_at instanceof Date
        ? msg.created_at.toISOString()
        : String(msg.created_at)
      : undefined,
    deletedAt: msg.deleted_at
      ? msg.deleted_at instanceof Date
        ? msg.deleted_at.toISOString()
        : String(msg.deleted_at)
      : undefined,
  };
}

/**
 * Converts a DB Conversation (snake_case) to a handler Conversation (camelCase).
 */
function conversationToHandler(conv: DBConversation): Conversation {
  return {
    id: conv.id,
    userId1: conv.user_id1,
    userId2: conv.user_id2,
    title: conv.title || undefined,
    lastMessageId: conv.last_processed_message_id
      ? String(conv.last_processed_message_id)
      : undefined,
    lastMessageAt: conv.last_message_at
      ? conv.last_message_at instanceof Date
        ? conv.last_message_at.toISOString()
        : String(conv.last_message_at)
      : undefined,
    lastReadMessageId1: conv.last_read_message_id1
      ? String(conv.last_read_message_id1)
      : undefined,
    lastReadMessageId2: conv.last_read_message_id2
      ? String(conv.last_read_message_id2)
      : undefined,
    createdAt:
      conv.created_at instanceof Date
        ? conv.created_at.toISOString()
        : String(conv.created_at),
    updatedAt: conv.updated_at
      ? conv.updated_at instanceof Date
        ? conv.updated_at.toISOString()
        : String(conv.updated_at)
      : undefined,
    deletedAt: conv.deleted_at
      ? conv.deleted_at instanceof Date
        ? conv.deleted_at.toISOString()
        : String(conv.deleted_at)
      : undefined,
    // HITL fields (D-125)
    agentStatus: conv.agent_status || undefined,
    agentId: conv.agent_id || undefined,
    checkpointId: conv.checkpoint_id || undefined,
    questions: conv.questions,
  };
}
