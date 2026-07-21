/**
 * UserUpdateStore unit tests.
 */

import {
  createFreshDatabase,
  createUserUpdate,
  resetIdCounter,
} from '../test-helpers';

describe('UserUpdateStore', () => {
  let db: ReturnType<typeof createFreshDatabase>;

  beforeEach(async () => {
    resetIdCounter();
    db = createFreshDatabase(`test-userupdate-${Date.now()}-${Math.random()}`);
    await db.open();
  });

  afterEach(async () => {
    await db.delete();
  });

  test('create bulk inserts updates', async () => {
    const updates = [
      createUserUpdate({ user_id: 'user1', seq: 1 }),
      createUserUpdate({ user_id: 'user1', seq: 2 }),
      createUserUpdate({ user_id: 'user1', seq: 3 }),
    ];
    await db.userUpdatesStore.create(updates);

    const results = await db.userUpdatesStore.listByUser('user1', 0, 10);
    expect(results).toHaveLength(3);
  });

  test('create with empty array is a no-op', async () => {
    await db.userUpdatesStore.create([]);
    const results = await db.userUpdatesStore.listByUser('user1', 0, 10);
    expect(results).toHaveLength(0);
  });

  test('listByUser returns updates after afterSeq, ordered by seq asc', async () => {
    const updates = [
      createUserUpdate({ user_id: 'user1', seq: 1 }),
      createUserUpdate({ user_id: 'user1', seq: 5 }),
      createUserUpdate({ user_id: 'user1', seq: 3 }),
      createUserUpdate({ user_id: 'user1', seq: 10 }),
    ];
    await db.userUpdatesStore.create(updates);

    const results = await db.userUpdatesStore.listByUser('user1', 3, 10);
    expect(results).toHaveLength(2); // seq 5 and 10
    expect(results[0].seq).toBe(5);
    expect(results[1].seq).toBe(10);
  });

  test('listByUser respects limit', async () => {
    const updates = [];
    for (let i = 1; i <= 10; i++) {
      updates.push(createUserUpdate({ user_id: 'user1', seq: i }));
    }
    await db.userUpdatesStore.create(updates);

    const results = await db.userUpdatesStore.listByUser('user1', 0, 3);
    expect(results).toHaveLength(3);
  });

  test('listByUserRange returns updates in (afterSeq, maxSeq]', async () => {
    const updates = [];
    for (let i = 1; i <= 10; i++) {
      updates.push(createUserUpdate({ user_id: 'user1', seq: i }));
    }
    await db.userUpdatesStore.create(updates);

    const results = await db.userUpdatesStore.listByUserRange('user1', 3, 7);
    expect(results).toHaveLength(4); // seq 4,5,6,7
    expect(results[0].seq).toBe(4);
    expect(results[3].seq).toBe(7);
  });

  test('listByUserRange returns empty when maxSeq <= afterSeq', async () => {
    const updates = [createUserUpdate({ user_id: 'user1', seq: 1 })];
    await db.userUpdatesStore.create(updates);

    const results = await db.userUpdatesStore.listByUserRange('user1', 5, 3);
    expect(results).toHaveLength(0);
  });

  test('getLatestSeq returns 0 when empty', async () => {
    const result = await db.userUpdatesStore.getLatestSeq('user1');
    expect(result).toBe(0);
  });

  test('getLatestSeq returns highest seq for user', async () => {
    const updates = [
      createUserUpdate({ user_id: 'user1', seq: 5 }),
      createUserUpdate({ user_id: 'user1', seq: 10 }),
      createUserUpdate({ user_id: 'user1', seq: 3 }),
    ];
    await db.userUpdatesStore.create(updates);

    const result = await db.userUpdatesStore.getLatestSeq('user1');
    expect(result).toBe(10);
  });

  test('cleanupExpiredBefore removes old updates', async () => {
    const updates = [
      createUserUpdate({
        user_id: 'user1',
        seq: 1,
        created_at: new Date('2026-01-01'),
      }),
      createUserUpdate({
        user_id: 'user1',
        seq: 2,
        created_at: new Date('2026-06-01'),
      }),
    ];
    await db.userUpdatesStore.create(updates);

    const deleted = await db.userUpdatesStore.cleanupExpiredBefore(
      new Date('2026-03-01'),
    );
    expect(deleted).toBe(1);

    const remaining = await db.userUpdatesStore.listByUser('user1', 0, 10);
    expect(remaining).toHaveLength(1);
    expect(remaining[0].seq).toBe(2);
  });
});
