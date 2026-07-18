/**
 * Filesystem path helpers for the Xyncra CLI runtime.
 *
 * Mirrors the Go reference logic: user state lives under
 * `~/.xyncra/{userID}/{deviceID}/` and all per-runtime paths
 * (socket, lock, db, logs) are derived from that base directory.
 *
 * @module
 */

import { createHash } from 'node:crypto';
import { hostname, homedir } from 'node:os';
import { mkdirSync } from 'node:fs';
import { join } from 'node:path';

/**
 * Compute a stable device identifier from the machine hostname.
 *
 * Returns the first 8 hex characters (4 bytes) of the SHA-256 hash
 * of the hostname. Falls back to `"unknown"` when `os.hostname()` throws
 * (e.g. in some container environments).
 *
 * Matches the Go reference:
 * ```go
 * h := sha256.Sum256([]byte(hostname))
 * return fmt.Sprintf("%x", h[:4])
 * ```
 */
export function defaultDeviceID(): string {
  let host: string;
  try {
    host = hostname();
  } catch {
    host = 'unknown';
  }
  if (!host) host = 'unknown';
  const hash = createHash('sha256').update(host).digest();
  return hash.subarray(0, 4).toString('hex');
}

/**
 * Ensure the per-user/per-device directory exists and return its path.
 *
 * Creates `~/.xyncra/{userID}/{deviceID}/` with mode `0o700` if missing.
 */
export function ensureUserDir(userID: string, deviceID: string): string {
  const dir = join(homedir(), '.xyncra', userID, deviceID);
  mkdirSync(dir, { recursive: true, mode: 0o700 });
  return dir;
}

/** Path to the Unix domain socket the daemon listens on. */
export function socketPath(userDir: string): string {
  return join(userDir, 'xyncra.sock');
}

/** Path to the daemon lock file. */
export function lockPath(userDir: string): string {
  return join(userDir, 'xyncra.lock');
}

/** Default path for the local SQLite / Dexie database file. */
export function dbPathDefault(userDir: string): string {
  return join(userDir, 'xyncra.db');
}

/** Default directory for log files (trailing slash preserved). */
export function logDirDefault(userDir: string): string {
  return join(userDir, 'logs') + '/';
}

/**
 * Append `user_id` and `device_id` query parameters to a server URL.
 *
 * Uses the WHATWG URL parser so existing query strings are preserved.
 */
export function serverURLWithUser(
  serverURL: string,
  userID: string,
  deviceID: string,
): string {
  const url = new URL(serverURL);
  url.searchParams.set('user_id', userID);
  url.searchParams.set('device_id', deviceID);
  return url.toString();
}
