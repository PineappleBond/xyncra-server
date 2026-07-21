/**
 * @packageDocumentation
 * Console logger adapter — implements ILogger using the browser console.
 *
 * @module
 */

import type { ILogger } from '@xyncra/client-core';

/**
 * Logger interface for the web package.
 * Matches the shape of ILogger from @xyncra/client-core.
 */
export interface Logger {
  debug(message: string, ...args: unknown[]): void;
  info(message: string, ...args: unknown[]): void;
  warn(message: string, ...args: unknown[]): void;
  error(message: string, ...args: unknown[]): void;
}

/**
 * ConsoleLogger implements both Logger and ILogger by delegating to the
 * browser's native `console` object.
 *
 * Each log level maps to the corresponding console method:
 * - debug → console.debug
 * - info  → console.info
 * - warn  → console.warn
 * - error → console.error
 */
export class ConsoleLogger implements Logger, ILogger {
  private prefix: string;

  /**
   * @param prefix - Optional prefix prepended to all log messages.
   *                 Defaults to '[xyncra]'.
   */
  constructor(prefix = '[xyncra]') {
    this.prefix = prefix;
  }

  debug(message: string, ...args: unknown[]): void {
    console.debug(`${this.prefix} ${message}`, ...args);
  }

  info(message: string, ...args: unknown[]): void {
    console.info(`${this.prefix} ${message}`, ...args);
  }

  warn(message: string, ...args: unknown[]): void {
    console.warn(`${this.prefix} ${message}`, ...args);
  }

  error(message: string, ...args: unknown[]): void {
    console.error(`${this.prefix} ${message}`, ...args);
  }
}
