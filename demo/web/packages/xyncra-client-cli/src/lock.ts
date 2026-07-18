/**
 * Daemon lock management using POSIX `flock(2)` via `fs-ext`.
 *
 * Ensures only one daemon instance runs per user/device directory.
 * Stale locks (where the recorded PID is no longer alive) are cleaned
 * up automatically on the next acquisition attempt.
 *
 * JSON format on disk matches the Go reference exactly:
 * ```json
 * { "pid": 12345, "started_at": "2026-07-18T10:00:00Z", "device_id": "abc01234" }
 * ```
 *
 * @module
 */

import { openSync, closeSync, readFileSync, writeFileSync, unlinkSync, existsSync } from 'node:fs';
import { join } from 'node:path';
import { flock } from 'fs-ext';
import { lockPath as makeLockPath } from './paths.js';

// POSIX flock(2) operation flags — values match the C constants on
// all platforms fs-ext supports (Linux, macOS, BSD).
const LOCK_EX = 2;
const LOCK_NB = 4;
const LOCK_UN = 8;

/** Info written inside the lock file — mirrors the Go struct JSON tags. */
export interface LockInfo {
  pid: number;
  started_at: string; // ISO 8601
  device_id: string;
}

/** Error class for lock acquisition failures. */
export class LockError extends Error {
  constructor(message: string, options?: { cause?: unknown }) {
    super(message, options);
    this.name = 'LockError';
  }
}

/**
 * Read and parse a lock file. Returns `null` when the file does not exist
 * or cannot be parsed.
 */
export function readLockInfo(lp: string): LockInfo | null {
  try {
    const raw = readFileSync(lp, 'utf8');
    const parsed = JSON.parse(raw) as LockInfo;
    if (typeof parsed.pid !== 'number' || typeof parsed.started_at !== 'string') {
      return null;
    }
    return parsed;
  } catch {
    return null;
  }
}

/**
 * Write lock info JSON with restrictive permissions (`0o600`).
 */
export function writeLockInfo(lp: string, info: LockInfo): void {
  writeFileSync(lp, JSON.stringify(info, null, 2) + '\n', { mode: 0o600 });
}

/**
 * Check whether a process with the given PID is still alive.
 *
 * Uses `process.kill(pid, 0)` which does not send a signal but does
 * perform the permission/existence check.
 */
export function isProcessAlive(pid: number): boolean {
  if (pid <= 0) return false;
  try {
    process.kill(pid, 0);
    return true;
  } catch (err) {
    // ESRCH = no such process; EPERM = exists but no permission (still alive).
    const e = err as NodeJS.ErrnoException;
    if (e.code === 'EPERM') return true;
    return false;
  }
}

/**
 * Acquire an exclusive, non-blocking flock on `lockPath`.
 *
 * Flow (mirrors the Go reference):
 * 1. Open the file and try `LOCK_EX | LOCK_NB`.
 * 2. If acquired: write `info` JSON, return an unlock function.
 * 3. If not acquired: read the existing lock, check if the PID is alive.
 *    - Alive  -> throw "already running".
 *    - Stale  -> remove the file, retry once.
 *
 * The returned unlock function releases the flock and removes the lock file.
 */
export async function acquireLock(
  lp: string,
  info: LockInfo,
): Promise<() => Promise<void>> {
  const attempt = async (isRetry: boolean): Promise<(() => Promise<void>) | null> => {
    // Open (or create) the file. Mode 0o600 so only the owner can read it.
    const fd = openSync(lp, 'w', 0o600);

    try {
      await flockAsync(fd, LOCK_EX | LOCK_NB);
    } catch (err) {
      // Lock is held by someone else.
      closeSync(fd);
      if (!isRetry) return null; // signal: try stale-recovery path
      throw new LockError('failed to acquire lock after stale cleanup', { cause: err });
    }

    // We got the lock. Write our info.
    writeLockInfo(lp, info);

    const unlock = async (): Promise<void> => {
      try {
        await flockAsync(fd, LOCK_UN);
      } catch {
        // Ignore — we're tearing down anyway.
      }
      try {
        closeSync(fd);
      } catch {
        // Already closed — ignore.
      }
      try {
        unlinkSync(lp);
      } catch {
        // May already be gone — ignore.
      }
    };

    return unlock;
  };

  const first = await attempt(true);
  if (first) return first;

  // Could not acquire — check whether the holder is still alive.
  const existing = readLockInfo(lp);
  if (existing && isProcessAlive(existing.pid)) {
    throw new LockError(`listen already running (PID: ${existing.pid})`);
  }

  // Stale lock — remove and retry once.
  try {
    unlinkSync(lp);
  } catch {
    // Already gone — fine.
  }

  const second = await attempt(false);
  if (second) return second;

  throw new LockError('failed to acquire lock');
}

/** Promise wrapper around the callback-based `flock` from fs-ext. */
function flockAsync(fd: number, operation: number): Promise<void> {
  return new Promise<void>((resolve, reject) => {
    flock(fd, operation, (err) => {
      if (err) reject(err);
      else resolve();
    });
  });
}

/**
 * Remove daemon-related files (lock and socket) from a user directory.
 *
 * No-op for missing files. Throws on unexpected FS errors.
 */
export function cleanupDaemonFiles(userDir: string): void {
  const lp = makeLockPath(userDir);
  if (existsSync(lp)) {
    try {
      unlinkSync(lp);
    } catch {
      // Best effort.
    }
  }

  // Socket path is derived from userDir via paths.socketPath, but we
  // inline the same derivation here to avoid a circular import.
  const sock = join(userDir, 'xyncra.sock');
  if (existsSync(sock)) {
    try {
      unlinkSync(sock);
    } catch {
      // Best effort.
    }
  }
}
