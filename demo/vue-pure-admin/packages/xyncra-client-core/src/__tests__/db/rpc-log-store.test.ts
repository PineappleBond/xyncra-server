/**
 * RPCLogStore unit tests.
 */

import { ErrNotFound } from '../../errors';
import {
  createFreshDatabase,
  createRPCLog,
  resetIdCounter,
} from '../test-helpers';

describe('RPCLogStore', () => {
  let db: ReturnType<typeof createFreshDatabase>;

  beforeEach(async () => {
    resetIdCounter();
    db = createFreshDatabase(`test-rpclog-${Date.now()}-${Math.random()}`);
    await db.open();
  });

  afterEach(async () => {
    await db.delete();
  });

  test('save and getByRequestID', async () => {
    const log = createRPCLog({ request_id: 'req-123', method: 'send_message' });
    await db.rpcLogsStore.save(log);

    const result = await db.rpcLogsStore.getByRequestID('req-123');
    expect(result.method).toBe('send_message');
  });

  test('getByRequestID throws ErrNotFound', async () => {
    await expect(db.rpcLogsStore.getByRequestID('nonexistent')).rejects.toBe(
      ErrNotFound,
    );
  });

  test('update modifies existing record', async () => {
    const log = createRPCLog({ request_id: 'req-1', status_code: 0 });
    await db.rpcLogsStore.save(log);

    log.status_code = 200;
    log.duration = 150;
    await db.rpcLogsStore.update(log);

    const result = await db.rpcLogsStore.getByRequestID('req-1');
    expect(result.status_code).toBe(200);
    expect(result.duration).toBe(150);
  });

  test('list returns logs sorted by created_at desc', async () => {
    await db.rpcLogsStore.save(
      createRPCLog({ method: 'a', created_at: new Date('2026-01-01') }),
    );
    await db.rpcLogsStore.save(
      createRPCLog({ method: 'b', created_at: new Date('2026-06-01') }),
    );

    const results = await db.rpcLogsStore.list({});
    expect(results).toHaveLength(2);
    expect(results[0].method).toBe('b'); // newer first
  });

  test('list filters by method', async () => {
    await db.rpcLogsStore.save(createRPCLog({ method: 'send_message' }));
    await db.rpcLogsStore.save(createRPCLog({ method: 'heartbeat' }));

    const results = await db.rpcLogsStore.list({ method: 'heartbeat' });
    expect(results).toHaveLength(1);
    expect(results[0].method).toBe('heartbeat');
  });

  test('list filters by time range', async () => {
    await db.rpcLogsStore.save(
      createRPCLog({ created_at: new Date('2026-01-15') }),
    );
    await db.rpcLogsStore.save(
      createRPCLog({ created_at: new Date('2026-06-15') }),
    );
    await db.rpcLogsStore.save(
      createRPCLog({ created_at: new Date('2026-12-15') }),
    );

    const results = await db.rpcLogsStore.list({
      start_time: new Date('2026-03-01'),
      end_time: new Date('2026-09-01'),
    });
    expect(results).toHaveLength(1);
  });

  test('aggregate computes per-method stats', async () => {
    await db.rpcLogsStore.save(
      createRPCLog({
        method: 'send_message',
        status_code: 0,
        duration: 100,
        created_at: new Date('2026-06-01'),
      }),
    );
    await db.rpcLogsStore.save(
      createRPCLog({
        method: 'send_message',
        status_code: -1,
        duration: 200,
        created_at: new Date('2026-06-02'),
      }),
    );
    await db.rpcLogsStore.save(
      createRPCLog({
        method: 'heartbeat',
        status_code: 0,
        duration: 50,
        created_at: new Date('2026-06-03'),
      }),
    );

    const rows = await db.rpcLogsStore.aggregate(
      new Date('2026-01-01'),
      new Date('2026-12-31'),
    );
    expect(rows).toHaveLength(2);

    const sendMsg = rows.find((r) => r.method === 'send_message');
    expect(sendMsg).toBeDefined();
    expect(sendMsg!.count).toBe(2);
    expect(sendMsg!.success).toBe(1);
    expect(sendMsg!.error_count).toBe(1);
    expect(sendMsg!.avg_ms).toBeCloseTo(150, 0);
  });

  test('cleanupBefore deletes old logs', async () => {
    await db.rpcLogsStore.save(
      createRPCLog({ created_at: new Date('2026-01-01') }),
    );
    await db.rpcLogsStore.save(
      createRPCLog({ created_at: new Date('2026-06-01') }),
    );

    const deleted = await db.rpcLogsStore.cleanupBefore(new Date('2026-03-01'));
    expect(deleted).toBe(1);

    const remaining = await db.rpcLogsStore.list({});
    expect(remaining).toHaveLength(1);
  });
});
