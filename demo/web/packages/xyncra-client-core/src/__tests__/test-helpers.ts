/**
 * Test helpers: mock factories, assertion utilities, and wait helpers.
 *
 * Mirrors Go pkg/client/testutil_test.go.
 */

import type { PackageDataUpdate } from '@xyncra/protocol';
import { IDBFactory } from 'fake-indexeddb';
import type { XyncraDatabase } from '../db';
import type {
  Conversation,
  Draft,
  Message,
  NotificationLog,
  Question,
  RetryTask,
  RPCLog,
  SyncState,
  UserUpdate,
} from '../db/models';
import type {
  Conversation as IConversation,
  ILogger,
  Message as IMessage,
  IUpdateHandler,
  IWebSocket,
  IWebSocketFactory,
} from '../interfaces';

// ---------------------------------------------------------------------------
// Model factories
// ---------------------------------------------------------------------------

let nextId = 1;
function uuid(): string {
  return `test-id-${nextId++}`;
}

/** Reset the auto-incrementing ID counter. Call in beforeEach(). */
export function resetIdCounter(): void {
  nextId = 1;
}

/**
 * Creates a Conversation with sensible defaults.
 * Override individual fields via the `overrides` parameter.
 */
export function createConversation(
  overrides: Partial<Conversation> = {},
): Conversation {
  const now = new Date();
  return {
    id: uuid(),
    user_id1: 'user1',
    user_id2: 'user2',
    type: '1-on-1',
    title: 'Test Conversation',
    pinned: false,
    muted: false,
    avatar_url: '',
    description: '',
    last_processed_message_id: 0,
    created_at: now,
    updated_at: now,
    last_message_at: now,
    last_read_message_id1: 0,
    last_read_message_id2: 0,
    agent_status: 'idle',
    agent_id: '',
    checkpoint_id: '',
    agent_last_activity: now,
    deleted_at: null,
    ...overrides,
  };
}

/**
 * Creates a Message with sensible defaults.
 */
export function createMessage(overrides: Partial<Message> = {}): Message {
  const now = new Date();
  return {
    id: uuid(),
    client_message_id: uuid(),
    conversation_id: 'conv-1',
    message_id: 1,
    sender_id: 'user1',
    content: 'Hello, world!',
    type: 'text',
    reply_to: 0,
    status: 'sent',
    created_at: now,
    deleted_at: null,
    ...overrides,
  };
}

/**
 * Creates a Question with sensible defaults.
 */
export function createQuestion(overrides: Partial<Question> = {}): Question {
  const now = new Date();
  return {
    id: uuid(),
    conversation_id: 'conv-1',
    checkpoint_id: 'cp-1',
    interrupt_id: 'intr-1',
    question_text: 'Are you sure?',
    status: 'pending',
    created_at: now,
    ...overrides,
  };
}

/**
 * Creates a SyncState record.
 */
export function createSyncState(key: string, value: string): SyncState {
  return {
    key,
    value,
    updated_at: new Date(),
  };
}

/**
 * Creates a Draft with sensible defaults.
 */
export function createDraft(overrides: Partial<Draft> = {}): Draft {
  const now = new Date();
  return {
    id: uuid(),
    conversation_id: 'conv-1',
    content: 'Draft content',
    created_at: now,
    updated_at: now,
    ...overrides,
  };
}

/**
 * Creates a RetryTask with sensible defaults.
 */
export function createRetryTask(overrides: Partial<RetryTask> = {}): RetryTask {
  const now = new Date();
  return {
    id: uuid(),
    method: 'test.method',
    params: new TextEncoder().encode(JSON.stringify({ foo: 'bar' })),
    attempt: 0,
    max_attempts: 5,
    next_retry: now,
    status: 'pending',
    last_error: '',
    created_at: now,
    ...overrides,
  };
}

/**
 * Creates an RPCLog with sensible defaults.
 */
export function createRPCLog(overrides: Partial<RPCLog> = {}): RPCLog {
  const now = new Date();
  return {
    id: uuid(),
    type: 'request',
    request_id: uuid(),
    method: 'test.method',
    params: new TextEncoder().encode(JSON.stringify({})),
    response: new Uint8Array(0),
    status_code: 0,
    conversation_id: '',
    duration: 100,
    error_msg: '',
    created_at: now,
    ...overrides,
  };
}

/**
 * Creates a NotificationLog with sensible defaults.
 */
export function createNotificationLog(
  overrides: Partial<NotificationLog> = {},
): NotificationLog {
  const now = new Date();
  return {
    id: uuid(),
    seq: 1,
    type: 'message',
    payload: new TextEncoder().encode(JSON.stringify({})),
    created_at: now,
    ...overrides,
  };
}

/**
 * Creates a UserUpdate with sensible defaults.
 */
export function createUserUpdate(
  overrides: Partial<UserUpdate> = {},
): UserUpdate {
  const now = new Date();
  return {
    id: uuid(),
    user_id: 'user1',
    seq: 1,
    type: 'message',
    payload: new TextEncoder().encode(JSON.stringify({})),
    created_at: now,
    ...overrides,
  };
}

/**
 * Creates a PackageDataUpdate for sync tests.
 */
export function createUpdate(
  seq: number,
  type: string,
  payload: unknown,
): PackageDataUpdate {
  return { seq, type, payload };
}

// ---------------------------------------------------------------------------
// Mock objects
// ---------------------------------------------------------------------------

/**
 * Creates a mock IWebSocket.
 */
export function createMockWebSocket(): IWebSocket & {
  sentMessages: Array<string | Uint8Array>;
  triggerOpen: () => void;
  triggerClose: (code?: number, reason?: string) => void;
  triggerMessage: (data: string) => void;
  triggerError: (error: Error) => void;
} {
  const sentMessages: Array<string | Uint8Array> = [];
  let messageHandler: ((data: string | Uint8Array) => void) | null = null;
  let closeHandler: ((code: number, reason: string) => void) | null = null;
  let errorHandler: ((error: Error) => void) | null = null;
  let openHandler: (() => void) | null = null;
  let state = 0; // CONNECTING

  const mock = {
    sentMessages,
    get readyState() {
      return state;
    },
    set readyState(v: number) {
      state = v;
    },
    send(data: string | Uint8Array) {
      sentMessages.push(data);
    },
    close(code = 1000, reason = '') {
      state = 3;
      closeHandler?.(code, reason);
    },
    onmessage(handler: (data: string | Uint8Array) => void) {
      messageHandler = handler;
    },
    onclose(handler: (code: number, reason: string) => void) {
      closeHandler = handler;
    },
    onerror(handler: (error: Error) => void) {
      errorHandler = handler;
    },
    onopen(handler: () => void) {
      openHandler = handler;
    },
    triggerOpen() {
      state = 1; // OPEN
      openHandler?.();
    },
    triggerClose(code = 1000, reason = '') {
      state = 3;
      closeHandler?.(code, reason);
    },
    triggerMessage(data: string) {
      messageHandler?.(data);
    },
    triggerError(error: Error) {
      errorHandler?.(error);
    },
  };

  return mock;
}

/**
 * Creates a mock IWebSocketFactory that returns the given mock WebSocket.
 */
export function createMockWebSocketFactory(
  ws: ReturnType<typeof createMockWebSocket>,
): IWebSocketFactory {
  return {
    create(_url: string): IWebSocket {
      return ws;
    },
  };
}

/**
 * Creates a mock ILogger that captures all log calls.
 */
export function createMockLogger(): ILogger & {
  debugCalls: Array<{ message: string; args: unknown[] }>;
  infoCalls: Array<{ message: string; args: unknown[] }>;
  warnCalls: Array<{ message: string; args: unknown[] }>;
  errorCalls: Array<{ message: string; args: unknown[] }>;
} {
  const debugCalls: Array<{ message: string; args: unknown[] }> = [];
  const infoCalls: Array<{ message: string; args: unknown[] }> = [];
  const warnCalls: Array<{ message: string; args: unknown[] }> = [];
  const errorCalls: Array<{ message: string; args: unknown[] }> = [];

  return {
    debugCalls,
    infoCalls,
    warnCalls,
    errorCalls,
    debug(message: string, ...args: unknown[]) {
      debugCalls.push({ message, args });
    },
    info(message: string, ...args: unknown[]) {
      infoCalls.push({ message, args });
    },
    warn(message: string, ...args: unknown[]) {
      warnCalls.push({ message, args });
    },
    error(message: string, ...args: unknown[]) {
      errorCalls.push({ message, args });
    },
  };
}

/**
 * Creates a mock IUpdateHandler that records all calls.
 */
export function createMockUpdateHandler(): IUpdateHandler & {
  onMessageCalls: IMessage[];
  onDeleteMessageCalls: Array<{ messageId: string; conversationId: string }>;
  onMarkReadCalls: Array<{ conversationId: string; messageId: string }>;
  onConversationCalls: IConversation[];
  onGapCalls: number[];
} {
  const onMessageCalls: IMessage[] = [];
  const onDeleteMessageCalls: Array<{
    messageId: string;
    conversationId: string;
  }> = [];
  const onMarkReadCalls: Array<{ conversationId: string; messageId: string }> =
    [];
  const onConversationCalls: IConversation[] = [];
  const onGapCalls: number[] = [];

  return {
    onMessageCalls,
    onDeleteMessageCalls,
    onMarkReadCalls,
    onConversationCalls,
    onGapCalls,
    async onMessage(message: IMessage) {
      onMessageCalls.push(message);
    },
    async onDeleteMessage(messageId: string, conversationId: string) {
      onDeleteMessageCalls.push({ messageId, conversationId });
    },
    async onMarkRead(conversationId: string, messageId: string) {
      onMarkReadCalls.push({ conversationId, messageId });
    },
    async onConversation(conversation: IConversation) {
      onConversationCalls.push(conversation);
    },
    async onGap(seq: number) {
      onGapCalls.push(seq);
    },
  };
}

// ---------------------------------------------------------------------------
// Wait helpers
// ---------------------------------------------------------------------------

/**
 * Waits for a condition to become true, polling at interval ms.
 * Throws if the timeout is exceeded.
 */
export async function waitFor(
  condition: () => boolean,
  timeout = 5000,
  interval = 50,
): Promise<void> {
  const start = Date.now();
  while (!condition()) {
    if (Date.now() - start > timeout) {
      throw new Error(`waitFor: timed out after ${timeout}ms`);
    }
    await sleep(interval);
  }
}

/**
 * Sleeps for the specified number of milliseconds.
 */
export function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

// ---------------------------------------------------------------------------
// Assertion helpers
// ---------------------------------------------------------------------------

/**
 * Asserts that the local_max_seq in the database matches the expected value.
 */
export async function assertSyncState(
  db: XyncraDatabase,
  expectedSeq: number,
): Promise<void> {
  const actual = await db.syncStatesStore.getLocalMaxSeq();
  expect(actual).toBe(expectedSeq);
}

// ---------------------------------------------------------------------------
// Fresh database factory (avoids cross-test leakage via fake-indexeddb)
// ---------------------------------------------------------------------------

/**
 * Creates a fresh XyncraDatabase backed by a brand-new FDBFactory instance.
 *
 * Each FDBFactory has its own isolated in-memory database map, so databases
 * created with different factories cannot interfere with each other. This
 * avoids "lower version" errors when test suites reuse the same DB name.
 *
 * Usage:
 *   let db: XyncraDatabase;
 *   beforeEach(async () => {
 *     db = createFreshDatabase('test-name');
 *     await db.open();
 *   });
 *   afterEach(async () => { await db.delete(); });
 */
export function createFreshDatabase(dbName: string): XyncraDatabase {
  // Lazy import to avoid circular dependency at module load time.
  // eslint-disable-next-line @typescript-eslint/no-var-requires
  const { XyncraDatabase: XyncraDB } = require('../db');
  const factory = new IDBFactory() as unknown as IDBFactory;
  return new XyncraDB(dbName, factory);
}
