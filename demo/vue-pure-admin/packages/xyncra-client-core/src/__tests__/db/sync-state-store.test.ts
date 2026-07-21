/**
 * SyncStateStore unit tests.
 */

import { ErrNotFound } from '../../errors';
import { createFreshDatabase, resetIdCounter } from '../test-helpers';

describe('SyncStateStore', () => {
  let db: ReturnType<typeof createFreshDatabase>;

  beforeEach(async () => {
    resetIdCounter();
    db = createFreshDatabase(`test-syncstate-${Date.now()}-${Math.random()}`);
    await db.open();
  });

  afterEach(async () => {
    await db.delete();
  });

  test('set and get', async () => {
    await db.syncStatesStore.set('test_key', 'test_value');
    const result = await db.syncStatesStore.get('test_key');
    expect(result).toBe('test_value');
  });

  test('get throws ErrNotFound for missing key', async () => {
    await expect(db.syncStatesStore.get('missing')).rejects.toBe(ErrNotFound);
  });

  test('set is an upsert (overwrites existing)', async () => {
    await db.syncStatesStore.set('key', 'v1');
    await db.syncStatesStore.set('key', 'v2');
    const result = await db.syncStatesStore.get('key');
    expect(result).toBe('v2');
  });

  test('getLocalMaxSeq returns 0 when not set', async () => {
    const result = await db.syncStatesStore.getLocalMaxSeq();
    expect(result).toBe(0);
  });

  test('setLocalMaxSeq and getLocalMaxSeq', async () => {
    await db.syncStatesStore.setLocalMaxSeq(42);
    const result = await db.syncStatesStore.getLocalMaxSeq();
    expect(result).toBe(42);
  });

  test('getLatestSeq returns 0 when not set', async () => {
    const result = await db.syncStatesStore.getLatestSeq();
    expect(result).toBe(0);
  });

  test('setLatestSeq and getLatestSeq', async () => {
    await db.syncStatesStore.setLatestSeq(100);
    const result = await db.syncStatesStore.getLatestSeq();
    expect(result).toBe(100);
  });

  test('setLocalMaxSeqTx works within a transaction', async () => {
    await db.transaction('rw', db.syncStates, async () => {
      const Dexie = (await import('dexie')).default;
      const tx = Dexie.currentTransaction;
      await db.syncStatesStore.setLocalMaxSeqTx(tx as any, 55);
    });

    const result = await db.syncStatesStore.getLocalMaxSeq();
    expect(result).toBe(55);
  });
});
