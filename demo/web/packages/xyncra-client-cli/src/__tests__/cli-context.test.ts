/**
 * Tests for cli-context.ts — CLIContext and flag resolution.
 */

import { mkdtempSync, rmSync, existsSync } from 'node:fs';
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

import { Command } from 'commander';

import { resolveStringFlag, CLIContext, registerGlobalFlags } from '../cli-context';

/** Create a test Command with global flags registered. */
function newTestCommand(): Command {
  const cmd = new Command('test');
  registerGlobalFlags(cmd);
  return cmd;
}

/**
 * Helper: parse argv against a fresh test Command and return the command
 * with resolved opts.
 */
function parseArgs(argv: string[]): Command {
  const cmd = newTestCommand();
  cmd.exitOverride();
  cmd.configureOutput({ writeErr: () => {}, writeOut: () => {} });
  // Default from=node: first two elements are node + script path.
  cmd.parse(['node', 'test', ...argv]);
  return cmd;
}

describe('resolveStringFlag', () => {
  const origEnv = { ...process.env };

  afterEach(() => {
    // Restore env.
    for (const key of Object.keys(process.env)) {
      if (!(key in origEnv)) delete process.env[key];
    }
    Object.assign(process.env, origEnv);
  });

  test('flag takes priority over env and default', () => {
    process.env.XYNCRA_USER_ID = 'env-user';
    const cmd = parseArgs(['--user-id', 'flag-user']);
    const got = resolveStringFlag(cmd, 'user-id', 'XYNCRA_USER_ID', 'default-user');
    expect(got).toBe('flag-user');
  });

  test('env takes priority over default', () => {
    process.env.XYNCRA_USER_ID = 'env-user';
    const cmd = parseArgs([]);
    const got = resolveStringFlag(cmd, 'user-id', 'XYNCRA_USER_ID', 'default-user');
    expect(got).toBe('env-user');
  });

  test('returns default when no flag or env', () => {
    delete process.env.XYNCRA_USER_ID;
    const cmd = parseArgs([]);
    const got = resolveStringFlag(cmd, 'user-id', 'XYNCRA_USER_ID', 'default-user');
    expect(got).toBe('default-user');
  });
});

describe('CLIContext.fromCommand', () => {
  let tmpHome: string;
  const mockHomedir = os.homedir as jest.Mock;
  const origHomedir = (jest.requireActual('node:os') as typeof os).homedir;

  beforeEach(() => {
    tmpHome = mkdtempSync(join(tmpdir(), 'xyncra-ctx-'));
    mockHomedir.mockReturnValue(tmpHome);
  });

  afterEach(() => {
    mockHomedir.mockImplementation(origHomedir);
    rmSync(tmpHome, { recursive: true, force: true });
    // Clean XYNCRA_ env vars.
    for (const k of Object.keys(process.env)) {
      if (k.startsWith('XYNCRA_')) delete process.env[k];
    }
  });

  test('throws when user-id is missing', () => {
    const cmd = parseArgs([]);
    expect(() => CLIContext.fromCommand(cmd)).toThrow(/user-id is required/);
  });

  test('resolves all fields from flags', () => {
    const cmd = parseArgs([
      '--user-id', 'testuser',
      '--device-id', 'testdevice',
      '--server', 'ws://example.com/ws',
    ]);

    const ctx = CLIContext.fromCommand(cmd);
    expect(ctx.userID).toBe('testuser');
    expect(ctx.deviceID).toBe('testdevice');
    expect(ctx.serverURL).toBe('ws://example.com/ws');

    // User dir should be created.
    const expectedUserDir = join(tmpHome, '.xyncra', 'testuser', 'testdevice');
    expect(ctx.userDir).toBe(expectedUserDir);
    expect(existsSync(expectedUserDir)).toBe(true);

    // Default paths derived from userDir.
    expect(ctx.dbPath).toBe(join(expectedUserDir, 'xyncra.db'));
    expect(ctx.logDir).toBe(join(expectedUserDir, 'logs') + '/');
  });

  test('resolves from env vars', () => {
    process.env.XYNCRA_USER_ID = 'envuser';
    process.env.XYNCRA_DEVICE_ID = 'envdevice';
    process.env.XYNCRA_SERVER = 'ws://env.example.com/ws';

    const cmd = parseArgs([]);
    const ctx = CLIContext.fromCommand(cmd);
    expect(ctx.userID).toBe('envuser');
    expect(ctx.deviceID).toBe('envdevice');
    expect(ctx.serverURL).toBe('ws://env.example.com/ws');
  });

  test('flag overrides env', () => {
    process.env.XYNCRA_USER_ID = 'envuser';
    const cmd = parseArgs(['--user-id', 'flaguser']);
    const ctx = CLIContext.fromCommand(cmd);
    expect(ctx.userID).toBe('flaguser');
  });

  test('uses default device ID when not provided', () => {
    const cmd = parseArgs(['--user-id', 'testuser']);
    const ctx = CLIContext.fromCommand(cmd);
    // Should be a non-empty 8-hex-char string.
    expect(ctx.deviceID).toMatch(/^[0-9a-f]{8}$/);
  });

  test('uses default server URL when not provided', () => {
    const cmd = parseArgs(['--user-id', 'testuser', '--device-id', 'dev1']);
    const ctx = CLIContext.fromCommand(cmd);
    expect(ctx.serverURL).toBe('ws://localhost:8080/ws');
  });

  test('getSocketPath / getLockPath derived from userDir', () => {
    const cmd = parseArgs(['--user-id', 'testuser', '--device-id', 'dev1']);
    const ctx = CLIContext.fromCommand(cmd);
    expect(ctx.getSocketPath()).toBe(join(ctx.userDir, 'xyncra.sock'));
    expect(ctx.getLockPath()).toBe(join(ctx.userDir, 'xyncra.lock'));
  });

  test('getServerURLWithUser appends user_id and device_id', () => {
    const cmd = parseArgs([
      '--user-id', 'testuser',
      '--device-id', 'dev1',
      '--server', 'ws://example.com/ws',
    ]);
    const ctx = CLIContext.fromCommand(cmd);
    const url = ctx.getServerURLWithUser();
    const parsed = new URL(url);
    expect(parsed.searchParams.get('user_id')).toBe('testuser');
    expect(parsed.searchParams.get('device_id')).toBe('dev1');
  });
});
