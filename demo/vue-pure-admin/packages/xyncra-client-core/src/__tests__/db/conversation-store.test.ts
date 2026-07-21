/**
 * ConversationStore unit tests.
 *
 * Tests cover:
 *   - CRUD operations
 *   - C4: UpdateLastRead MAX semantics
 *   - Soft delete and restore with cascade
 *   - Transaction variants (Tx)
 *   - Edge cases: not found, duplicate key, getByUsers ordering
 */

import { ErrNotFound } from '../../errors';
import {
  createConversation,
  createFreshDatabase,
  createMessage,
  resetIdCounter,
} from '../test-helpers';

describe('ConversationStore', () => {
  let db: ReturnType<typeof createFreshDatabase>;

  beforeEach(async () => {
    resetIdCounter();
    db = createFreshDatabase(`test-conv-${Date.now()}-${Math.random()}`);
    await db.open();
  });

  afterEach(async () => {
    await db.delete();
  });

  // ---------------------------------------------------------------------------
  // CRUD
  // ---------------------------------------------------------------------------

  test('create and get', async () => {
    const conv = createConversation({ user_id1: 'alice', user_id2: 'bob' });
    await db.conversationsStore.create(conv);

    const retrieved = await db.conversationsStore.get(conv.id);
    expect(retrieved).toBeDefined();
    expect(retrieved!.user_id1).toBe('alice');
    expect(retrieved!.user_id2).toBe('bob');
  });

  test('get returns undefined for non-existent id', async () => {
    const result = await db.conversationsStore.get('non-existent');
    expect(result).toBeUndefined();
  });

  test('get excludes soft-deleted records', async () => {
    const conv = createConversation({ deleted_at: new Date() });
    await db.conversations.add(conv);

    const result = await db.conversationsStore.get(conv.id);
    expect(result).toBeUndefined();
  });

  test('getUnscoped includes soft-deleted records', async () => {
    const conv = createConversation({ deleted_at: new Date() });
    await db.conversations.add(conv);

    const result = await db.conversationsStore.getUnscoped(conv.id);
    expect(result).toBeDefined();
    expect(result!.deleted_at).not.toBeNull();
  });

  // ---------------------------------------------------------------------------
  // getByUsers
  // ---------------------------------------------------------------------------

  test('getByUsers finds conversation in either order', async () => {
    const conv = createConversation({ user_id1: 'alice', user_id2: 'bob' });
    await db.conversationsStore.create(conv);

    // Forward order
    let found = await db.conversationsStore.getByUsers('alice', 'bob');
    expect(found).toBeDefined();
    expect(found!.id).toBe(conv.id);

    // Reverse order
    found = await db.conversationsStore.getByUsers('bob', 'alice');
    expect(found).toBeDefined();
    expect(found!.id).toBe(conv.id);
  });

  test('getByUsers returns undefined when not found', async () => {
    const found = await db.conversationsStore.getByUsers('nobody', 'here');
    expect(found).toBeUndefined();
  });

  test('getByUsers excludes soft-deleted', async () => {
    const conv = createConversation({
      user_id1: 'alice',
      user_id2: 'bob',
      deleted_at: new Date(),
    });
    await db.conversations.add(conv);

    const found = await db.conversationsStore.getByUsers('alice', 'bob');
    expect(found).toBeUndefined();
  });

  // ---------------------------------------------------------------------------
  // getByUser
  // ---------------------------------------------------------------------------

  test('getByUser returns conversations sorted by last_message_at desc', async () => {
    const conv1 = createConversation({
      user_id1: 'alice',
      user_id2: 'bob',
      last_message_at: new Date('2026-01-01'),
    });
    const conv2 = createConversation({
      user_id1: 'alice',
      user_id2: 'charlie',
      last_message_at: new Date('2026-06-01'),
    });
    await db.conversationsStore.create(conv1);
    await db.conversationsStore.create(conv2);

    const result = await db.conversationsStore.getByUser('alice', 0, 20);
    expect(result).toHaveLength(2);
    expect(result[0].id).toBe(conv2.id); // newer first
  });

  test('getByUser excludes soft-deleted', async () => {
    const conv1 = createConversation({ user_id1: 'alice', user_id2: 'bob' });
    const conv2 = createConversation({
      user_id1: 'alice',
      user_id2: 'charlie',
      deleted_at: new Date(),
    });
    await db.conversationsStore.create(conv1);
    await db.conversations.add(conv2);

    const result = await db.conversationsStore.getByUser('alice', 0, 20);
    expect(result).toHaveLength(1);
    expect(result[0].id).toBe(conv1.id);
  });

  // ---------------------------------------------------------------------------
  // C4: UpdateLastRead MAX semantics
  // ---------------------------------------------------------------------------

  test('C4: updateLastRead only advances forward for user_id1', async () => {
    const conv = createConversation({
      user_id1: 'alice',
      user_id2: 'bob',
      last_read_message_id1: 0,
    });
    await db.conversationsStore.create(conv);

    // Advance to 10
    await db.conversationsStore.updateLastRead(conv.id, 'alice', 10);
    let updated = await db.conversationsStore.get(conv.id);
    expect(updated!.last_read_message_id1).toBe(10);

    // Advance to 20 (should succeed)
    await db.conversationsStore.updateLastRead(conv.id, 'alice', 20);
    updated = await db.conversationsStore.get(conv.id);
    expect(updated!.last_read_message_id1).toBe(20);

    // Try to go back to 5 (should be rejected - MAX semantics)
    await db.conversationsStore.updateLastRead(conv.id, 'alice', 5);
    updated = await db.conversationsStore.get(conv.id);
    expect(updated!.last_read_message_id1).toBe(20); // unchanged
  });

  test('C4: updateLastRead works for user_id2', async () => {
    const conv = createConversation({
      user_id1: 'alice',
      user_id2: 'bob',
      last_read_message_id2: 0,
    });
    await db.conversationsStore.create(conv);

    await db.conversationsStore.updateLastRead(conv.id, 'bob', 15);
    const updated = await db.conversationsStore.get(conv.id);
    expect(updated!.last_read_message_id2).toBe(15);
  });

  test('updateLastRead throws ErrNotFound for unknown user', async () => {
    const conv = createConversation({ user_id1: 'alice', user_id2: 'bob' });
    await db.conversationsStore.create(conv);

    await expect(
      db.conversationsStore.updateLastRead(conv.id, 'charlie', 10),
    ).rejects.toBe(ErrNotFound);
  });

  // ---------------------------------------------------------------------------
  // Soft delete and restore (cascade)
  // ---------------------------------------------------------------------------

  test('delete cascade soft-deletes conversation and messages', async () => {
    const conv = createConversation();
    await db.conversationsStore.create(conv);

    const msg = createMessage({ conversation_id: conv.id });
    await db.messagesStore.create(msg);

    await db.conversationsStore.delete(conv.id);

    // Conversation is no longer visible
    const convResult = await db.conversationsStore.get(conv.id);
    expect(convResult).toBeUndefined();

    // But visible via getUnscoped
    const convUnscoped = await db.conversationsStore.getUnscoped(conv.id);
    expect(convUnscoped).toBeDefined();
    expect(convUnscoped!.deleted_at).not.toBeNull();

    // Message is also soft-deleted
    const msgResult = await db.messagesStore.get(msg.id);
    expect(msgResult).toBeUndefined();
  });

  test('restore cascade restores conversation and messages', async () => {
    const conv = createConversation();
    await db.conversationsStore.create(conv);

    const msg = createMessage({ conversation_id: conv.id });
    await db.messagesStore.create(msg);

    // Delete first
    await db.conversationsStore.delete(conv.id);

    // Then restore
    await db.conversationsStore.restore(conv.id);

    // Conversation is visible again
    const convResult = await db.conversationsStore.get(conv.id);
    expect(convResult).toBeDefined();
    expect(convResult!.deleted_at).toBeNull();

    // Message is also restored
    const msgResult = await db.messagesStore.get(msg.id);
    expect(msgResult).toBeDefined();
  });

  test('restore is idempotent on non-deleted conversation', async () => {
    const conv = createConversation();
    await db.conversationsStore.create(conv);

    // Should not throw
    await db.conversationsStore.restore(conv.id);

    const result = await db.conversationsStore.get(conv.id);
    expect(result).toBeDefined();
  });

  test('delete throws ErrNotFound for non-existent', async () => {
    await expect(db.conversationsStore.delete('nope')).rejects.toBe(
      ErrNotFound,
    );
  });

  // ---------------------------------------------------------------------------
  // upsert
  // ---------------------------------------------------------------------------

  test('upsert creates new record', async () => {
    const conv = createConversation({ title: 'New' });
    await db.conversationsStore.upsert(conv);

    const result = await db.conversationsStore.get(conv.id);
    expect(result).toBeDefined();
    expect(result!.title).toBe('New');
  });

  test('upsert updates existing record', async () => {
    const conv = createConversation({ title: 'Original' });
    await db.conversationsStore.create(conv);

    conv.title = 'Updated';
    await db.conversationsStore.upsert(conv);

    const result = await db.conversationsStore.get(conv.id);
    expect(result!.title).toBe('Updated');
  });

  // ---------------------------------------------------------------------------
  // searchByTitle
  // ---------------------------------------------------------------------------

  test('searchByTitle finds case-insensitive matches', async () => {
    const conv1 = createConversation({
      user_id1: 'alice',
      user_id2: 'bob',
      title: 'Hello World',
    });
    const conv2 = createConversation({
      user_id1: 'alice',
      user_id2: 'charlie',
      title: 'Goodbye',
    });
    await db.conversationsStore.create(conv1);
    await db.conversationsStore.create(conv2);

    const results = await db.conversationsStore.searchByTitle(
      'alice',
      'hello',
      10,
    );
    expect(results).toHaveLength(1);
    expect(results[0].id).toBe(conv1.id);
  });

  test('searchByTitle returns empty for empty query', async () => {
    const conv = createConversation({ user_id1: 'alice', title: 'Test' });
    await db.conversationsStore.create(conv);

    const results = await db.conversationsStore.searchByTitle('alice', '', 10);
    expect(results).toHaveLength(0);
  });

  // ---------------------------------------------------------------------------
  // updateLastMessage
  // ---------------------------------------------------------------------------

  test('updateLastMessage updates fields', async () => {
    const conv = createConversation();
    await db.conversationsStore.create(conv);

    const newDate = new Date('2026-07-01');
    await db.conversationsStore.updateLastMessage(conv.id, newDate, 42);

    const result = await db.conversationsStore.get(conv.id);
    expect(result!.last_message_at).toEqual(newDate);
    expect(result!.last_processed_message_id).toBe(42);
  });

  test('updateLastMessage throws ErrNotFound for unknown', async () => {
    await expect(
      db.conversationsStore.updateLastMessage('nope', new Date(), 1),
    ).rejects.toBe(ErrNotFound);
  });
});
