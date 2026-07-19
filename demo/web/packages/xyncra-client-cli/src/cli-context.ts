/**
 * CLIContext holds resolved configuration for the CLI.
 *
 * Priority for resolving values: flag > env var > default (D-034).
 * Mirrors Go internal/cli/app.go CLIContext.
 *
 * @module
 */

import type { Command } from 'commander';
import {
  dbPathDefault,
  defaultDeviceID,
  ensureUserDir,
  lockPath,
  logDirDefault,
  serverURLWithUser,
  socketPath,
} from './paths.js';

/**
 * Resolve a string flag with priority: flag > env var > default.
 *
 * Mirrors Go resolveStringFlag() in internal/cli/app.go.
 */
export function resolveStringFlag(
  cmd: Command,
  flagName: string,
  envName: string,
  defaultVal: string,
): string {
  // Check if flag was explicitly set on command line.
  const opts = cmd.optsWithGlobals();
  const flagKey = flagName.replace(/-([a-z])/g, (_, c: string) =>
    c.toUpperCase(),
  );
  if (opts[flagKey] !== undefined && opts[flagKey] !== '') {
    return opts[flagKey] as string;
  }
  // Check environment variable.
  const envVal = process.env[envName];
  if (envVal !== undefined && envVal !== '') {
    return envVal;
  }
  return defaultVal;
}

/**
 * CLIContext holds all resolved CLI configuration values.
 *
 * Mirrors Go CLIContext struct in internal/cli/app.go.
 */
export class CLIContext {
  readonly userID: string;
  readonly deviceID: string;
  readonly serverURL: string;
  readonly dbPath: string;
  readonly logDir: string;
  readonly userDir: string;

  constructor(opts: {
    userID: string;
    deviceID: string;
    serverURL: string;
    dbPath: string;
    logDir: string;
    userDir: string;
  }) {
    this.userID = opts.userID;
    this.deviceID = opts.deviceID;
    this.serverURL = opts.serverURL;
    this.dbPath = opts.dbPath;
    this.logDir = opts.logDir;
    this.userDir = opts.userDir;
  }

  /** Path to the Unix domain socket. */
  getSocketPath(): string {
    return socketPath(this.userDir);
  }

  /** Path to the lock file. */
  getLockPath(): string {
    return lockPath(this.userDir);
  }

  /** Default database path. */
  getDBPathDefault(): string {
    return dbPathDefault(this.userDir);
  }

  /** Default log directory. */
  getLogDirDefault(): string {
    return logDirDefault(this.userDir);
  }

  /** Server URL with user_id and device_id query params. */
  getServerURLWithUser(): string {
    return serverURLWithUser(this.serverURL, this.userID, this.deviceID);
  }

  /**
   * Build a CLIContext from a Commander command, resolving all global flags.
   *
   * Mirrors Go NewCLIContext() in internal/cli/app.go.
   * Priority: flag > env var > default (D-034).
   */
  static fromCommand(cmd: Command): CLIContext {
    // Resolve user-id (required).
    const userID = resolveStringFlag(cmd, 'user-id', 'XYNCRA_USER_ID', '');
    if (!userID) {
      throw new Error(
        'context: user-id is required (set via --user-id flag or XYNCRA_USER_ID env var)',
      );
    }

    // Resolve device-id (default: SHA256(hostname)[:8]).
    const deviceID =
      resolveStringFlag(cmd, 'device-id', 'XYNCRA_DEVICE_ID', '') ||
      defaultDeviceID();

    // Resolve server URL.
    const serverURL = resolveStringFlag(
      cmd,
      'server',
      'XYNCRA_SERVER',
      'ws://localhost:8080/ws',
    );

    // Create user directory to compute dynamic defaults.
    const userDir = ensureUserDir(userID, deviceID);

    // Resolve db-path and log-dir with dynamic defaults.
    const resolvedDBPath = resolveStringFlag(
      cmd,
      'db-path',
      'XYNCRA_DB_PATH',
      dbPathDefault(userDir),
    );
    const resolvedLogDir = resolveStringFlag(
      cmd,
      'log-dir',
      'XYNCRA_LOG_DIR',
      logDirDefault(userDir),
    );

    return new CLIContext({
      userID,
      deviceID,
      serverURL,
      dbPath: resolvedDBPath,
      logDir: resolvedLogDir,
      userDir,
    });
  }
}

/**
 * Register all global persistent flags on a Commander command.
 *
 * Mirrors Go NewRootCommand() flag registration in internal/cli/app.go.
 */
export function registerGlobalFlags(program: Command): void {
  program
    .option('-u, --user-id <id>', 'User ID (env: XYNCRA_USER_ID)')
    .option(
      '--device-id <id>',
      'Device ID (default: SHA256(hostname)[:8], env: XYNCRA_DEVICE_ID)',
    )
    .option('-s, --server <url>', 'Server URL (env: XYNCRA_SERVER)')
    .option(
      '--db-path <path>',
      'Database path (default: $USER_DIR/xyncra.db) (env: XYNCRA_DB_PATH)',
    )
    .option(
      '--log-dir <dir>',
      'Log directory (default: $USER_DIR/logs/) (env: XYNCRA_LOG_DIR)',
    );
}
