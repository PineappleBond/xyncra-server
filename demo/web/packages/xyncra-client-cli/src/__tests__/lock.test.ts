/**
 * Tests for lock.ts — daemon lock management using POSIX flock(2).
 */

import { mkdtempSync, rmSync, writeFileSync, existsSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import {
  readLockInfo,
  writeLockInfo,
  isProcessAlive,
  acquireLock,
  LockError,
  type LockInfo,
} from '../lock';

describe('readLockInfo', () => {
  let tmpDir: string;

  beforeEach(() => {
    tmpDir = mkdtempSync(join(tmpdir(), 'xyncra-lock-'));
  });

  afterEach(() => {
    rmSync(tmpDir, { recursive: true, force: true });
  });

  test('returns null for non-existent file', () => {
    const result = readLockInfo(join(tmpDir, 'no-such-file.lock'));
    expect(result).toBeNull();
  });

  test('returns null for invalid JSON', () => {
    const lp = join(tmpDir, 'bad.lock');
    writeFileSync(lp, 'not json at all');
    expect(readLockInfo(lp)).toBeNull();
  });

  test('returns null when required fields are missing', () => {
    const lp = join(tmpDir, 'missing.lock');
    writeFileSync(lp, JSON.stringify({ foo: 'bar' }));
    expect(readLockInfo(lp)).toBeNull();
  });

  test('parses valid JSON lock info', () => {
    const lp = join(tmpDir, 'valid.lock');
    const info: LockInfo = {
      pid: 12345,
      started_at: '2026-07-18T10:00:00Z',
      device_id: 'my-device',
    };
    writeFileSync(lp, JSON.stringify(info));
    const result = readLockInfo(lp);
    expect(result).toEqual(info);
  });
});

describe('writeLockInfo + readLockInfo round-trip', () => {
  let tmpDir: string;

  beforeEach(() => {
    tmpDir = mkdtempSync(join(tmpdir(), 'xyncra-lock-'));
  });

  afterEach(() => {
    rmSync(tmpDir, { recursive: true, force: true });
  });

  test('round-trip preserves all fields', () => {
    const lp = join(tmpDir, 'rt.lock');
    const info: LockInfo = {
      pid: 12345,
      started_at: '2026-07-18T10:00:00.000Z',
      device_id: 'my-device',
    };
    writeLockInfo(lp, info);

    // File should exist with the JSON content.
    const raw = readFileSync(lp, 'utf8');
    expect(JSON.parse(raw)).toEqual(info);

    const result = readLockInfo(lp);
    expect(result).toEqual(info);
  });
});

describe('isProcessAlive', () => {
  test('current process returns true', () => {
    expect(isProcessAlive(process.pid)).toBe(true);
  });

  test('non-existent PID (99999999) returns false', () => {
    expect(isProcessAlive(99999999)).toBe(false);
  });

  test('invalid PID 0 returns false', () => {
    expect(isProcessAlive(0)).toBe(false);
  });

  test('invalid PID -1 returns false', () => {
    expect(isProcessAlive(-1)).toBe(false);
  });
});

describe('acquireLock', () => {
  let tmpDir: string;

  beforeEach(() => {
    tmpDir = mkdtempSync(join(tmpdir(), 'xyncra-lock-'));
  });

  afterEach(() => {
    rmSync(tmpDir, { recursive: true, force: true });
  });

  const makeInfo = (pid?: number, device = 'test-device'): LockInfo => ({
    pid: pid ?? process.pid,
    started_at: new Date().toISOString(),
    device_id: device,
  });

  test('acquires successfully', async () => {
    const lp = join(tmpDir, 'test.lock');
    const info = makeInfo();

    const unlock = await acquireLock(lp, info);
    try {
      expect(typeof unlock).toBe('function');
      expect(existsSync(lp)).toBe(true);

      // Lock file should have our info.
      const read = readLockInfo(lp);
      expect(read).not.toBeNull();
      expect(read!.pid).toBe(process.pid);
      expect(read!.device_id).toBe('test-device');
    } finally {
      await unlock();
    }
  });

  test('duplicate fails when lock is held', async () => {
    const lp = join(tmpDir, 'test.lock');
    const info = makeInfo();

    const unlock1 = await acquireLock(lp, info);
    try {
      await expect(acquireLock(lp, info)).rejects.toThrow(LockError);
    } finally {
      await unlock1();
    }
  });

  test('release and re-acquire works', async () => {
    const lp = join(tmpDir, 'test.lock');
    const info = makeInfo();

    const unlock1 = await acquireLock(lp, info);
    await unlock1();

    // Now should be able to re-acquire.
    const unlock2 = await acquireLock(lp, info);
    try {
      expect(existsSync(lp)).toBe(true);
    } finally {
      await unlock2();
    }
  });

  test('stale lock cleanup — fake lock with PID 99999999', async () => {
    const lp = join(tmpDir, 'test.lock');
    const staleInfo: LockInfo = {
      pid: 99999999,
      started_at: new Date(Date.now() - 3600_000).toISOString(),
      device_id: 'stale-device',
    };
    writeFileSync(lp, JSON.stringify(staleInfo) + '\n', { mode: 0o600 });

    const info = makeInfo(process.pid, 'test-device');
    const unlock = await acquireLock(lp, info);
    try {
      const read = readLockInfo(lp);
      expect(read).not.toBeNull();
      expect(read!.pid).toBe(process.pid);
      expect(read!.device_id).toBe('test-device');
    } finally {
      await unlock();
    }
  });
});
