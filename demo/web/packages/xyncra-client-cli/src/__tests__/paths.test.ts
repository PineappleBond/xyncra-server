/**
 * Tests for paths.ts — filesystem path helpers for the Xyncra CLI runtime.
 */

import { mkdtempSync, rmSync, existsSync, statSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

jest.mock('node:os', () => {
  const actual = jest.requireActual('node:os');
  return {
    ...actual,
    homedir: jest.fn(actual.homedir),
  };
});

import * as os from 'node:os';

import {
  defaultDeviceID,
  ensureUserDir,
  socketPath,
  lockPath,
  dbPathDefault,
  logDirDefault,
  serverURLWithUser,
} from '../paths';

describe('defaultDeviceID', () => {
  test('deterministic across multiple calls', () => {
    const a = defaultDeviceID();
    const b = defaultDeviceID();
    expect(a).toBe(b);
  });

  test('returns exactly 8 hex chars matching ^[0-9a-f]{8}$', () => {
    const id = defaultDeviceID();
    expect(id).toMatch(/^[0-9a-f]{8}$/);
  });

  test('returns non-empty string (hostname fallback still works)', () => {
    // Cannot easily override os.hostname() without mocking, but ensure non-empty.
    const id = defaultDeviceID();
    expect(id).not.toBe('');
    expect(id.length).toBe(8);
  });
});

describe('ensureUserDir', () => {
  let tmpHome: string;
  const mockHomedir = os.homedir as jest.Mock;
  const origHomedir = (jest.requireActual('node:os') as typeof os).homedir;

  beforeEach(() => {
    tmpHome = mkdtempSync(join(tmpdir(), 'xyncra-paths-'));
    mockHomedir.mockReturnValue(tmpHome);
  });

  afterEach(() => {
    mockHomedir.mockImplementation(origHomedir);
    rmSync(tmpHome, { recursive: true, force: true });
  });

  test('creates directory with correct path', () => {
    const dir = ensureUserDir('user1', 'dev1');
    const expected = join(tmpHome, '.xyncra', 'user1', 'dev1');
    expect(dir).toBe(expected);
    expect(existsSync(dir)).toBe(true);
    expect(statSync(dir).isDirectory()).toBe(true);
  });

  test('idempotent — calling 3 times does not error', () => {
    expect(() => {
      ensureUserDir('user1', 'dev1');
      ensureUserDir('user1', 'dev1');
      ensureUserDir('user1', 'dev1');
    }).not.toThrow();
  });
});

describe('path derivation helpers', () => {
  const userDir = '/tmp/test-user-dir';

  test('socketPath returns userDir/xyncra.sock', () => {
    expect(socketPath(userDir)).toBe(join(userDir, 'xyncra.sock'));
  });

  test('lockPath returns userDir/xyncra.lock', () => {
    expect(lockPath(userDir)).toBe(join(userDir, 'xyncra.lock'));
  });

  test('dbPathDefault returns userDir/xyncra.db', () => {
    expect(dbPathDefault(userDir)).toBe(join(userDir, 'xyncra.db'));
  });

  test('logDirDefault returns userDir/logs/ with trailing slash', () => {
    const result = logDirDefault(userDir);
    expect(result).toBe(join(userDir, 'logs') + '/');
    expect(result.endsWith('/')).toBe(true);
  });
});

describe('serverURLWithUser', () => {
  test('appends user_id and device_id query params', () => {
    const result = serverURLWithUser('ws://example.com/ws', 'user1', 'dev1');
    const url = new URL(result);
    expect(url.searchParams.get('user_id')).toBe('user1');
    expect(url.searchParams.get('device_id')).toBe('dev1');
    expect(url.origin + url.pathname).toBe('ws://example.com/ws');
  });

  test('preserves existing query params', () => {
    const result = serverURLWithUser('ws://example.com/ws?foo=bar', 'user1', 'dev1');
    const url = new URL(result);
    expect(url.searchParams.get('foo')).toBe('bar');
    expect(url.searchParams.get('user_id')).toBe('user1');
  });

  test('overwrites existing user_id/device_id params', () => {
    const result = serverURLWithUser('ws://example.com/ws?user_id=old', 'new', 'dev1');
    const url = new URL(result);
    expect(url.searchParams.get('user_id')).toBe('new');
  });
});
