/**
 * NotificationLogStore unit tests.
 *
 * Tests cover:
 *   - C6: Seq unique index for deduplication
 *   - List and filter operations
 *   - Transaction variant (saveTx)
 */

import Dexie from 'dexie';
import { ErrDuplicateKey, ErrNotFound } from '../../errors';
import {
  createFreshDatabase,
  createNotificationLog,
  resetIdCounter,
} from '../test-helpers';

describe('NotificationLogStore', () => {
  let db: ReturnType<typeof createFreshDatabase>;

  beforeEach(async () => {
    resetIdCounter();
    db = createFreshDatabase(`test-notiflog-${Date.now()}-${Math.random()}`);
    await db.open();
  });

  afterEach(async () => {
    await db.delete();
  });

  // ---------------------------------------------------------------------------
  // C6: Seq unique index
  // ---------------------------------------------------------------------------

  test('C6: save succeeds with unique seq', async () => {
    const log = createNotificationLog({ seq: 1 });
    await db.notificationLogsStore.save(log);

    const results = await db.notificationLogsStore.list({});
    expect(results).toHaveLength(1);
  });

  test('C6: duplicate seq throws ErrDuplicateKey', async () => {
    const log1 = createNotificationLog({ seq: 42 });
    await db.notificationLogsStore.save(log1);

    const log2 = createNotificationLog({ seq: 42 });
    await expect(db.notificationLogsStore.save(log2)).rejects.toBe(
      ErrDuplicateKey,
    );
  });

  // ---------------------------------------------------------------------------
  // List operations
  // ---------------------------------------------------------------------------

  test('list returns logs sorted by created_at desc', async () => {
    await db.notificationLogsStore.save(
      createNotificationLog({ seq: 1, created_at: new Date('2026-01-01') }),
    );
    await db.notificationLogsStore.save(
      createNotificationLog({ seq: 2, created_at: new Date('2026-06-01') }),
    );

    const results = await db.notificationLogsStore.list({});
    expect(results).toHaveLength(2);
    expect(results[0].seq).toBe(2); // newer first
  });

  test('list filters by type', async () => {
    await db.notificationLogsStore.save(
      createNotificationLog({ seq: 1, type: 'message' }),
    );
    await db.notificationLogsStore.save(
      createNotificationLog({ seq: 2, type: 'conversation' }),
    );

    const results = await db.notificationLogsStore.list({ type: 'message' });
    expect(results).toHaveLength(1);
    expect(results[0].type).toBe('message');
  });

  test('listBySeqRange returns logs in range', async () => {
    for (let i = 1; i <= 5; i++) {
      await db.notificationLogsStore.save(createNotificationLog({ seq: i }));
    }

    const results = await db.notificationLogsStore.listBySeqRange(2, 4);
    expect(results).toHaveLength(3);
    expect(results[0].seq).toBe(2);
    expect(results[2].seq).toBe(4);
  });

  // ---------------------------------------------------------------------------
  // getLatestSeq
  // ---------------------------------------------------------------------------

  test('getLatestSeq returns 0 when empty', async () => {
    const result = await db.notificationLogsStore.getLatestSeq();
    expect(result).toBe(0);
  });

  test('getLatestSeq returns highest seq', async () => {
    await db.notificationLogsStore.save(createNotificationLog({ seq: 5 }));
    await db.notificationLogsStore.save(createNotificationLog({ seq: 10 }));
    await db.notificationLogsStore.save(createNotificationLog({ seq: 3 }));

    const result = await db.notificationLogsStore.getLatestSeq();
    expect(result).toBe(10);
  });

  // ---------------------------------------------------------------------------
  // saveTx
  // ---------------------------------------------------------------------------

  test('saveTx works within a transaction', async () => {
    await db.transaction('rw', db.notificationLogs, async () => {
      const tx = Dexie.currentTransaction;
      const log = createNotificationLog({ seq: 99 });
      await db.notificationLogsStore.saveTx(tx as any, log);
    });

    const results = await db.notificationLogsStore.list({});
    expect(results).toHaveLength(1);
    expect(results[0].seq).toBe(99);
  });

  test('saveTx duplicate seq throws ErrDuplicateKey', async () => {
    await db.notificationLogsStore.save(createNotificationLog({ seq: 1 }));

    await db.transaction('rw', db.notificationLogs, async () => {
      const tx = Dexie.currentTransaction;
      const log = createNotificationLog({ seq: 1 });
      await expect(
        db.notificationLogsStore.saveTx(tx as any, log),
      ).rejects.toBe(ErrDuplicateKey);
    });
  });

  // ---------------------------------------------------------------------------
  // Cleanup
  // ---------------------------------------------------------------------------

  test('cleanupBefore deletes old logs', async () => {
    await db.notificationLogsStore.save(
      createNotificationLog({ seq: 1, created_at: new Date('2026-01-01') }),
    );
    await db.notificationLogsStore.save(
      createNotificationLog({ seq: 2, created_at: new Date('2026-06-01') }),
    );

    const deleted = await db.notificationLogsStore.cleanupBefore(
      new Date('2026-03-01'),
    );
    expect(deleted).toBe(1);
  });
});
