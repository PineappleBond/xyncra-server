/**
 * CLI-aware logger implementing ILogger from @xyncra/client-core.
 *
 * Writes structured log lines to stderr. Debug output is suppressed
 * unless XYNCRA_DEBUG is set to "1" or "true".
 *
 * Mirrors Go cliLogger in internal/cli/listen.go.
 *
 * @module
 */

import type { ILogger } from '@xyncra/client-core';

/**
 * Format the current time as "YYYY-MM-DD HH:mm:ss".
 * Mirrors Go logTimestamp().
 */
function logTimestamp(): string {
  const now = new Date();
  const pad = (n: number, w = 2): string => String(n).padStart(w, '0');
  return (
    `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())} ` +
    `${pad(now.getHours())}:${pad(now.getMinutes())}:${pad(now.getSeconds())}`
  );
}

/**
 * Format variadic key-value arguments as " key=value ..." string.
 * Mirrors Go formatLogArgs().
 */
function formatLogArgs(args: unknown[]): string {
  if (args.length === 0) return '';
  const parts: string[] = [];
  for (let i = 0; i < args.length; i += 2) {
    const key = String(args[i]);
    const val = i + 1 < args.length ? String(args[i + 1]) : 'MISSING';
    parts.push(`${key}=${val}`);
  }
  return ' ' + parts.join(' ');
}

/**
 * CLILogger implements ILogger by writing to stderr.
 * Debug output is gated by XYNCRA_DEBUG environment variable.
 */
export class CLILogger implements ILogger {
  private readonly debugEnabled: boolean;

  constructor() {
    const v = process.env.XYNCRA_DEBUG;
    this.debugEnabled = v === '1' || v === 'true';
  }

  debug(message: string, ...args: unknown[]): void {
    if (!this.debugEnabled) return;
    process.stderr.write(
      `[${logTimestamp()}] [DEBUG] ${message}${formatLogArgs(args)}\n`,
    );
  }

  info(message: string, ...args: unknown[]): void {
    process.stderr.write(
      `[${logTimestamp()}] [INFO] ${message}${formatLogArgs(args)}\n`,
    );
  }

  warn(message: string, ...args: unknown[]): void {
    process.stderr.write(
      `[${logTimestamp()}] [WARN] ${message}${formatLogArgs(args)}\n`,
    );
  }

  error(message: string, ...args: unknown[]): void {
    process.stderr.write(
      `[${logTimestamp()}] [ERROR] ${message}${formatLogArgs(args)}\n`,
    );
  }
}
