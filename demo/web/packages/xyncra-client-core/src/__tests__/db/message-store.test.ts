/**
 * MessageStore unit tests.
 *
 * Tests cover:
 *   - CRUD operations
 *   - C5: Message uniqueness via composite index (client_message_id, sender_id)
 *   - Soft delete and restore
 *   - List/search by conversation, time range
 *   - Transaction variants (Tx)
 */

import { ErrNotFound } from '../../errors';
import {
  createFreshDatabase,
  createMessage,
  resetIdCounter,
} from '../test-helpers';

describe('MessageStore', () => {
  let db: ReturnType<typeof createFreshDatabase>;

  beforeEach(async () => {
    resetIdCounter();
    db = createFreshDatabase(`test-msg-${Date.now()}-${Math.random()}`);
    await db.open();
  });

  afterEach(async () => {
    await db.delete();
  });

  // ---------------------------------------------------------------------------
  // CRUD
  // ---------------------------------------------------------------------------

  test('create and get', async () => {
    const msg = createMessage({ sender_id: 'alice', content: 'Hi' });
    await db.messagesStore.create(msg);

    const result = await db.messagesStore.get(msg.id);
    expect(result).toBeDefined();
    expect(result!.sender_id).toBe('alice');
    expect(result!.content).toBe('Hi');
  });

  test('get returns undefined for non-existent', async () => {
    const result = await db.messagesStore.get('nope');
    expect(result).toBeUndefined();
  });

  test('get excludes soft-deleted', async () => {
    const msg = createMessage({ deleted_at: new Date() });
    await db.messages.add(msg);

    const result = await db.messagesStore.get(msg.id);
    expect(result).toBeUndefined();
  });

  // ---------------------------------------------------------------------------
  // C5: Composite uniqueness
  // ---------------------------------------------------------------------------

  test('C5: getByClientMessageID finds by composite key', async () => {
    const msg = createMessage({
      client_message_id: 'cmid-1',
      sender_id: 'alice',
      conversation_id: 'conv-1',
    });
    await db.messagesStore.create(msg);

    const result = await db.messagesStore.getByClientMessageID(
      'cmid-1',
      'alice',
    );
    expect(result).toBeDefined();
    expect(result!.id).toBe(msg.id);
  });

  test('C5: getByClientMessageID returns undefined for wrong sender', async () => {
    const msg = createMessage({
      client_message_id: 'cmid-1',
      sender_id: 'alice',
    });
    await db.messagesStore.create(msg);

    const result = await db.messagesStore.getByClientMessageID('cmid-1', 'bob');
    expect(result).toBeUndefined();
  });

  test('C5: upsert updates existing by composite key', async () => {
    const msg1 = createMessage({
      client_message_id: 'cmid-1',
      sender_id: 'alice',
      content: 'original',
    });
    await db.messagesStore.create(msg1);

    const msg2 = createMessage({
      client_message_id: 'cmid-1',
      sender_id: 'alice',
      content: 'updated',
    });
    await db.messagesStore.upsert(msg2);

    const result = await db.messagesStore.getByClientMessageID(
      'cmid-1',
      'alice',
    );
    expect(result).toBeDefined();
    expect(result!.content).toBe('updated');
  });

  // ---------------------------------------------------------------------------
  // listByConversation
  // ---------------------------------------------------------------------------

  test('listByConversation returns messages after afterMessageID', async () => {
    for (let i = 1; i <= 5; i++) {
      await db.messagesStore.create(
        createMessage({ conversation_id: 'conv-1', message_id: i }),
      );
    }

    const result = await db.messagesStore.listByConversation('conv-1', 2, 10);
    expect(result).toHaveLength(3);
    expect(result[0].message_id).toBe(3);
  });

  test('listByConversation filters soft-deleted', async () => {
    await db.messagesStore.create(
      createMessage({ conversation_id: 'conv-1', message_id: 1 }),
    );
    await db.messages.add(
      createMessage({
        conversation_id: 'conv-1',
        message_id: 2,
        deleted_at: new Date(),
      }),
    );
    await db.messagesStore.create(
      createMessage({ conversation_id: 'conv-1', message_id: 3 }),
    );

    const result = await db.messagesStore.listByConversation('conv-1', 0, 10);
    expect(result).toHaveLength(2);
  });

  // ---------------------------------------------------------------------------
  // searchByConversation
  // ---------------------------------------------------------------------------

  test('searchByConversation finds content matches', async () => {
    await db.messagesStore.create(
      createMessage({
        conversation_id: 'conv-1',
        message_id: 1,
        content: 'hello world',
      }),
    );
    await db.messagesStore.create(
      createMessage({
        conversation_id: 'conv-1',
        message_id: 2,
        content: 'goodbye',
      }),
    );

    const result = await db.messagesStore.searchByConversation(
      'conv-1',
      'hello',
      0,
      10,
    );
    expect(result).toHaveLength(1);
    expect(result[0].content).toBe('hello world');
  });

  test('searchByConversation returns empty for empty query', async () => {
    await db.messagesStore.create(
      createMessage({
        conversation_id: 'conv-1',
        message_id: 1,
        content: 'test',
      }),
    );

    const result = await db.messagesStore.searchByConversation(
      'conv-1',
      '',
      0,
      10,
    );
    expect(result).toHaveLength(0);
  });

  // ---------------------------------------------------------------------------
  // listByTimeRange
  // ---------------------------------------------------------------------------

  test('listByTimeRange filters by date', async () => {
    await db.messagesStore.create(
      createMessage({
        conversation_id: 'conv-1',
        message_id: 1,
        created_at: new Date('2026-01-15'),
      }),
    );
    await db.messagesStore.create(
      createMessage({
        conversation_id: 'conv-1',
        message_id: 2,
        created_at: new Date('2026-06-15'),
      }),
    );
    await db.messagesStore.create(
      createMessage({
        conversation_id: 'conv-1',
        message_id: 3,
        created_at: new Date('2026-12-15'),
      }),
    );

    const result = await db.messagesStore.listByTimeRange(
      'conv-1',
      new Date('2026-03-01'),
      new Date('2026-09-01'),
      10,
    );
    expect(result).toHaveLength(1);
    expect(result[0].message_id).toBe(2);
  });

  // ---------------------------------------------------------------------------
  // Soft delete and restore
  // ---------------------------------------------------------------------------

  test('delete soft-deletes a message', async () => {
    const msg = createMessage();
    await db.messagesStore.create(msg);

    await db.messagesStore.delete(msg.id);

    const result = await db.messagesStore.get(msg.id);
    expect(result).toBeUndefined();
  });

  test('restore undeletes a message', async () => {
    const msg = createMessage();
    await db.messagesStore.create(msg);
    await db.messagesStore.delete(msg.id);

    await db.messagesStore.restore(msg.id);

    const result = await db.messagesStore.get(msg.id);
    expect(result).toBeDefined();
  });

  test('delete throws ErrNotFound for non-existent', async () => {
    await expect(db.messagesStore.delete('nope')).rejects.toBe(ErrNotFound);
  });

  // ---------------------------------------------------------------------------
  // listRecentByConversation
  // ---------------------------------------------------------------------------

  test('listRecentByConversation returns newest first', async () => {
    for (let i = 1; i <= 5; i++) {
      await db.messagesStore.create(
        createMessage({ conversation_id: 'conv-1', message_id: i }),
      );
    }

    const result = await db.messagesStore.listRecentByConversation('conv-1', 3);
    expect(result).toHaveLength(3);
    expect(result[0].message_id).toBe(5); // newest first
    expect(result[2].message_id).toBe(3);
  });

  // ---------------------------------------------------------------------------
  // countUnread
  // ---------------------------------------------------------------------------

  test('countUnread counts messages after given id', async () => {
    for (let i = 1; i <= 5; i++) {
      await db.messagesStore.create(
        createMessage({ conversation_id: 'conv-1', message_id: i }),
      );
    }

    const count = await db.messagesStore.countUnread('conv-1', 3);
    expect(count).toBe(2); // messages 4 and 5
  });
});
