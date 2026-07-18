/**
 * SyncManager unit tests.
 *
 * Tests cover:
 *   - C7: Ephemeral updates (seq=0) bypass persistence
 *   - C8: Debounced pull on gap detection
 *   - C12: FullSync pagination
 *   - C6: NotificationLog deduplication
 *   - Normal seq continuity
 *   - Skip already-processed updates
 */

import type { PackageDataUpdate } from '@xyncra/protocol';
import { SyncManager } from '../sync-manager';
import {
  createConversation,
  createFreshDatabase,
  createMockLogger,
  createMockUpdateHandler,
  createUpdate,
  resetIdCounter,
  sleep,
} from './test-helpers';

describe('SyncManager', () => {
  let db: ReturnType<typeof createFreshDatabase>;
  let handler: ReturnType<typeof createMockUpdateHandler>;
  let logger: ReturnType<typeof createMockLogger>;
  let rpcFn: jest.Mock;

  beforeEach(async () => {
    resetIdCounter();
    db = createFreshDatabase(`test-sync-${Date.now()}-${Math.random()}`);
    await db.open();
    handler = createMockUpdateHandler();
    logger = createMockLogger();
    rpcFn = jest.fn();
  });

  afterEach(async () => {
    await db.delete();
  });

  function createSyncManager(debounceInterval = 500) {
    return new SyncManager({
      db,
      handler,
      rpcFn,
      logger,
      debounceInterval,
    });
  }

  // ---------------------------------------------------------------------------
  // C7: Ephemeral updates (seq=0) bypass persistence
  // ---------------------------------------------------------------------------

  test('C7: ephemeral update (seq=0) bypasses persistence for typing', async () => {
    const syncMgr = createSyncManager();

    const update = createUpdate(0, 'typing', {
      user_id: 'user1',
      conversation_id: 'conv-1',
      is_typing: true,
    });

    await syncMgr.applyUpdate(update);

    // Handler should NOT be called for typing via notifyHandler
    // (typing handler is an optional extension, tested via IUpdateHandler cast)
    // NotificationLog should have no entries
    const logs = await db.notificationLogs.toArray();
    expect(logs).toHaveLength(0);

    // localMaxSeq should remain 0
    const seq = await db.syncStatesStore.getLocalMaxSeq();
    expect(seq).toBe(0);
  });

  test('C7: ephemeral conversation update triggers pull-on-notification', async () => {
    const syncMgr = createSyncManager();
    rpcFn.mockResolvedValue({
      conversation: createConversation({ id: 'conv-1', agent_status: 'idle' }),
      questions: [],
    });

    const update = createUpdate(0, 'conversation', {
      action: 'update',
      conversation_id: 'conv-1',
      updated_at: Math.floor(Date.now() / 1000),
    });

    await syncMgr.applyUpdate(update);

    // Should have called get_conversation RPC
    expect(rpcFn).toHaveBeenCalledWith('get_conversation', {
      conversation_id: 'conv-1',
    });

    // No persistence
    const logs = await db.notificationLogs.toArray();
    expect(logs).toHaveLength(0);
  });

  // ---------------------------------------------------------------------------
  // Normal seq continuity
  // ---------------------------------------------------------------------------

  test('normal update advances localMaxSeq', async () => {
    const syncMgr = createSyncManager();

    // Setup: localMaxSeq = 0, send seq=1
    const update = createUpdate(1, 'gap', {}); // gap type does nothing in DB

    await syncMgr.applyUpdate(update);

    const seq = await db.syncStatesStore.getLocalMaxSeq();
    expect(seq).toBe(1);
  });

  test('skip update with seq <= localMaxSeq', async () => {
    const syncMgr = createSyncManager();

    // Set localMaxSeq to 5
    await db.syncStatesStore.setLocalMaxSeq(5);

    // Send seq=3 (already processed)
    const update = createUpdate(3, 'gap', {});
    await syncMgr.applyUpdate(update);

    // Should remain at 5
    const seq = await db.syncStatesStore.getLocalMaxSeq();
    expect(seq).toBe(5);

    // NotificationLog should be empty (skipped)
    const logs = await db.notificationLogs.toArray();
    expect(logs).toHaveLength(0);
  });

  // ---------------------------------------------------------------------------
  // C8: Debounced pull on gap
  // ---------------------------------------------------------------------------

  test('C8: gap detection triggers debounced pull', async () => {
    const syncMgr = createSyncManager(100); // 100ms debounce
    rpcFn.mockResolvedValue({
      updates: [],
      has_more: false,
      latest_seq: 0,
    });

    // Set localMaxSeq = 5
    await db.syncStatesStore.setLocalMaxSeq(5);

    // Send seq=10 (gap: seq > localMaxSeq+1)
    const update = createUpdate(10, 'gap', {});
    await syncMgr.applyUpdate(update);

    // seq should not advance (gap detected)
    const seq = await db.syncStatesStore.getLocalMaxSeq();
    expect(seq).toBe(5);

    // Wait for debounce to fire
    await sleep(200);

    // rpcFn should have been called with sync_updates
    expect(rpcFn).toHaveBeenCalledWith('sync_updates', expect.any(Object));
  });

  // ---------------------------------------------------------------------------
  // C12: FullSync pagination
  // ---------------------------------------------------------------------------

  test('C12: FullSync fetches all pages until has_more=false', async () => {
    const syncMgr = createSyncManager();

    rpcFn
      .mockResolvedValueOnce({
        updates: [createUpdate(1, 'gap', {}), createUpdate(2, 'gap', {})],
        has_more: true,
        latest_seq: 10,
      })
      .mockResolvedValueOnce({
        updates: [createUpdate(3, 'gap', {})],
        has_more: false,
        latest_seq: 10,
      });

    await syncMgr.fullSync();

    expect(rpcFn).toHaveBeenCalledTimes(2);
    expect(rpcFn).toHaveBeenNthCalledWith(1, 'sync_updates', {
      after_seq: 0,
      limit: 100,
    });

    const seq = await db.syncStatesStore.getLocalMaxSeq();
    expect(seq).toBe(3);
  });

  // ---------------------------------------------------------------------------
  // C6: NotificationLog deduplication
  // ---------------------------------------------------------------------------

  test('C6: duplicate seq is detected and skipped', async () => {
    const syncMgr = createSyncManager();

    // First update at seq=1
    const update1 = createUpdate(1, 'gap', {});
    await syncMgr.applyUpdate(update1);

    let seq = await db.syncStatesStore.getLocalMaxSeq();
    expect(seq).toBe(1);

    // Now manually reset localMaxSeq to 0 to simulate re-processing
    await db.syncStatesStore.setLocalMaxSeq(0);

    // Try to apply seq=1 again - should be deduplicated by NotificationLog
    // Note: applyUpdate sees seq=1 > localMaxSeq=0, so it tries to apply
    // But NotificationLog.saveTx should throw ErrDuplicateKey
    const update1Again = createUpdate(1, 'gap', {});
    await syncMgr.applyUpdate(update1Again);

    // seq should be advanced to 1 again (dedup path)
    seq = await db.syncStatesStore.getLocalMaxSeq();
    expect(seq).toBe(1);
  });

  // ---------------------------------------------------------------------------
  // applyUpdates batch
  // ---------------------------------------------------------------------------

  test('applyUpdates processes updates in order', async () => {
    const syncMgr = createSyncManager();

    const updates = [
      createUpdate(1, 'gap', {}),
      createUpdate(2, 'gap', {}),
      createUpdate(3, 'gap', {}),
    ];

    await syncMgr.applyUpdates(updates);

    const seq = await db.syncStatesStore.getLocalMaxSeq();
    expect(seq).toBe(3);
  });

  // ---------------------------------------------------------------------------
  // Snake_case data flow: server sends snake_case JSON → stored correctly
  // ---------------------------------------------------------------------------

  test('fullSync stores snake_case conversation data queryable via Dexie indexes', async () => {
    const syncMgr = createSyncManager();

    // Simulate server response with snake_case keys (matching Go JSON tags)
    rpcFn.mockResolvedValueOnce({
      updates: [{
        seq: 1,
        type: 'conversation',
        payload: {
          action: 'create',
          conversation: createConversation({
            id: 'conv-snake-1',
            user_id1: 'alice',
            user_id2: 'bob',
            type: '1-on-1',
          }),
        },
      }],
      has_more: false,
      latest_seq: 1,
    });

    await syncMgr.fullSync();

    // Verify conversation was stored and is queryable by snake_case fields
    const convs = await db.conversationsStore.getByUser('alice', 0, 10);
    expect(convs).toHaveLength(1);
    expect(convs[0].id).toBe('conv-snake-1');
    expect(convs[0].user_id1).toBe('alice');
    expect(convs[0].user_id2).toBe('bob');
  });
});
