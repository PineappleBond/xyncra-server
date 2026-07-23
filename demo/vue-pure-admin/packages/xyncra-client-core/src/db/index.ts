/**
 * @packageDocumentation
 * XyncraDatabase — Dexie-based IndexedDB wrapper.
 *
 * Mirrors Go ClientDB (pkg/store/clientdb.go):
 *   - Aggregates 9 domain sub-stores
 *   - Defines all tables and indexes
 *   - Accepts optional IDBFactory injection (TS-D-002, TS-D-003)
 *
 * The constructor optionally accepts an IDBFactory to allow environment
 * injection: browser's `indexedDB` in production, `fake-indexeddb` in tests.
 *
 * TS-D-012: `dbPath` semantics changed from file path to IndexedDB database name.
 */

import Dexie, { type Transaction } from 'dexie';
import { ConversationStore } from './conversation-store';
import { DraftStore } from './draft-store';
import { MessageStore } from './message-store';
import type {
  Conversation,
  Draft,
  Message,
  NotificationLog,
  RemoteCalling,
  RetryQueueItem,
  RetryTask,
  RPCLog,
  SyncState,
  UserUpdate,
} from './models';
import { NotificationLogStore } from './notification-log-store';
import { QueueStore } from './queue-store';
import { RemoteCallingStore } from './remote-calling-store';
import { RetryQueueStore } from './retry-queue-store';
import { RPCLogStore } from './rpc-log-store';
import { SyncStateStore } from './sync-state-store';
import { UserUpdateStore } from './user-update-store';

// ---------------------------------------------------------------------------
// Database class
// ---------------------------------------------------------------------------

/**
 * XyncraDatabase is the top-level data access entry point for the Xyncra
 * client. It aggregates 9 domain stores and manages IndexedDB table
 * definitions and indexes.
 *
 * Mirrors Go ClientDB (pkg/store/clientdb.go).
 */
export class XyncraDatabase extends Dexie {
  // Dexie table references (typed). These are populated by Dexie when
  // this.version(1).stores(...) is called in the constructor.
  conversations!: Dexie.Table<Conversation, string>;
  messages!: Dexie.Table<Message, string>;
  remoteCallings!: Dexie.Table<RemoteCalling, string>;
  retryQueue!: Dexie.Table<RetryQueueItem, number>;
  syncStates!: Dexie.Table<SyncState, string>;
  drafts!: Dexie.Table<Draft, string>;
  retryTasks!: Dexie.Table<RetryTask, string>;
  rpcLogs!: Dexie.Table<RPCLog, string>;
  notificationLogs!: Dexie.Table<NotificationLog, number>;
  userUpdates!: Dexie.Table<UserUpdate, string>;

  // Domain sub-stores (mirrors Go ClientDB struct fields).
  readonly conversationsStore: ConversationStore;
  readonly messagesStore: MessageStore;
  readonly remoteCallingsStore: RemoteCallingStore;
  readonly retryQueueStore: RetryQueueStore;
  readonly syncStatesStore: SyncStateStore;
  readonly draftsStore: DraftStore;
  readonly queueStore: QueueStore;
  readonly rpcLogsStore: RPCLogStore;
  readonly notificationLogsStore: NotificationLogStore;
  readonly userUpdatesStore: UserUpdateStore;

  /**
   * Opens (or creates) the IndexedDB database.
   *
   * @param dbName - IndexedDB database name (TS-D-012: was file path in Go).
   *                 If omitted, defaults to "xyncra".
   * @param idbFactory - Optional IDBFactory for environment injection (TS-D-002).
   *                     In browsers, pass `indexedDB`. In tests, pass
   *                     `fake-indexeddb`. If omitted, Dexie uses the
   *                     global `indexedDB`.
   */
  constructor(dbName?: string, idbFactory?: IDBFactory) {
    // Dexie options use `indexedDB` to inject a custom IDBFactory (TS-D-003).
    super(
      dbName ?? 'xyncra',
      idbFactory
        ? { indexedDB: idbFactory as unknown as { open: Function } }
        : undefined,
    );

    // -----------------------------------------------------------------------
    // Schema version 1 — initial tables and indexes
    // -----------------------------------------------------------------------
    // Index definitions mirror Go GORM struct tags.
    //
    // Dexie index syntax:
    //   "field"        → simple index
    //   "&field"       → unique index
    //   "[a+b]"        → compound index (non-unique)
    //   "&[a+b]"       → compound unique index
    //   "++id"         → auto-increment primary key
    //   "id"           → primary key (non-auto-increment)
    //
    // Index mapping from Go GORM tags:
    //   conversations:
    //     - idx_conversation_users_unique → &[user_id1+user_id2]
    //     - idx_conversation_user1_deleted → user_id1 (soft-delete filtered in JS)
    //     - idx_conversation_user2_deleted → user_id2
    //     - idx_conversation_lastmsg_deleted → last_message_at
    //     - type, created_at, agent_status → simple indexes
    //   messages:
    //     - idx_msg_client_id_sender → &[client_message_id+sender_id]
    //     - idx_message_conv_msg_deleted → [conversation_id+message_id]
    //     - sender_id, created_at → simple indexes
    //   notificationLogs:
    //     - seq uniqueIndex → &seq (but seq is already the primary key)
    //   userUpdates:
    //     - idx_user_update_user_seq → [user_id+seq]
    // -----------------------------------------------------------------------

    this.version(1).stores({
      conversations:
        'id, user_id1, user_id2, &[user_id1+user_id2], type, created_at, last_message_at, agent_status',
      messages:
        'id, &[client_message_id+sender_id], conversation_id, [conversation_id+message_id], sender_id, created_at, message_id',
      questions: 'id, conversation_id, created_at',
      syncStates: 'key',
      drafts: 'id, &conversation_id',
      retryTasks: 'id, method, status, next_retry, created_at',
      rpcLogs:
        'id, type, request_id, method, status_code, conversation_id, created_at',
      notificationLogs: 'seq, type, created_at',
      userUpdates: 'id, user_id, [user_id+seq], type, created_at',
    });

    // Version 2: Replace questions with remote_callings (D-137).
    // Project not launched — no data migration needed, Dexie auto-drops and rebuilds.
    this.version(2).stores({
      conversations:
        'id, user_id1, user_id2, &[user_id1+user_id2], type, created_at, last_message_at, agent_status',
      messages:
        'id, &[client_message_id+sender_id], conversation_id, [conversation_id+message_id], sender_id, created_at, message_id',
      remoteCallings:
        'id, conversation_id, status, checkpoint_id, [conversation_id+status]',
      retryQueue: '++id, remote_calling_id, next_retry_at',
      syncStates: 'key',
      drafts: 'id, &conversation_id',
      retryTasks: 'id, method, status, next_retry, created_at',
      rpcLogs:
        'id, type, request_id, method, status_code, conversation_id, created_at',
      notificationLogs: 'seq, type, created_at',
      userUpdates: 'id, user_id, [user_id+seq], type, created_at',
    });

    // Version 3: Add [conversation_id+device_id] compound index to remoteCallings.
    // Enables efficient queries when filtering remote callings by device within a
    // conversation (e.g., device-specific function calls in multi-device scenarios).
    // Project not launched — no data migration needed, Dexie auto-drops and rebuilds.
    this.version(3).stores({
      conversations:
        'id, user_id1, user_id2, &[user_id1+user_id2], type, created_at, last_message_at, agent_status',
      messages:
        'id, &[client_message_id+sender_id], conversation_id, [conversation_id+message_id], sender_id, created_at, message_id',
      remoteCallings:
        'id, conversation_id, status, checkpoint_id, [conversation_id+status], [conversation_id+device_id]',
      retryQueue: '++id, remote_calling_id, next_retry_at',
      syncStates: 'key',
      drafts: 'id, &conversation_id',
      retryTasks: 'id, method, status, next_retry, created_at',
      rpcLogs:
        'id, type, request_id, method, status_code, conversation_id, created_at',
      notificationLogs: 'seq, type, created_at',
      userUpdates: 'id, user_id, [user_id+seq], type, created_at',
    });

    // Initialize domain sub-stores.
    this.conversationsStore = new ConversationStore(this);
    this.messagesStore = new MessageStore(this);
    this.remoteCallingsStore = new RemoteCallingStore(this);
    this.retryQueueStore = new RetryQueueStore(this);
    this.syncStatesStore = new SyncStateStore(this);
    this.draftsStore = new DraftStore(this);
    this.queueStore = new QueueStore(this);
    this.rpcLogsStore = new RPCLogStore(this);
    this.notificationLogsStore = new NotificationLogStore(this);
    this.userUpdatesStore = new UserUpdateStore(this);
  }

  /**
   * Closes the database connection.
   */
  close(): void {
    super.close();
  }
}

export { ConversationStore } from './conversation-store';
export { DraftStore } from './draft-store';
export { MessageStore } from './message-store';
// Re-export all models and sub-stores for convenience.
export * from './models';
export { NotificationLogStore } from './notification-log-store';
export { QueueStore } from './queue-store';
export { RemoteCallingStore } from './remote-calling-store';
export { RetryQueueStore } from './retry-queue-store';
export { RPCLogStore } from './rpc-log-store';
export { SyncStateStore } from './sync-state-store';
export { UserUpdateStore } from './user-update-store';
