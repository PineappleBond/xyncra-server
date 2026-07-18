/**
 * Tests for logger.ts — CLILogger implementing ILogger.
 */

import { CLILogger } from '../logger';
import type { ILogger } from '@xyncra/client-core';

describe('CLILogger', () => {
  let stderrSpy: jest.SpyInstance;
  const origDebug = process.env.XYNCRA_DEBUG;

  beforeEach(() => {
    stderrSpy = jest.spyOn(process.stderr, 'write').mockImplementation(() => true);
  });

  afterEach(() => {
    stderrSpy.mockRestore();
    if (origDebug === undefined) {
      delete process.env.XYNCRA_DEBUG;
    } else {
      process.env.XYNCRA_DEBUG = origDebug;
    }
  });

  test('implements ILogger interface', () => {
    const logger = new CLILogger();
    expect(typeof logger.debug).toBe('function');
    expect(typeof logger.info).toBe('function');
    expect(typeof logger.warn).toBe('function');
    expect(typeof logger.error).toBe('function');

    // Structural check: satisfies ILogger.
    const ilogger: ILogger = logger;
    expect(ilogger).toBeDefined();
  });

  test('debug output suppressed when XYNCRA_DEBUG not set', () => {
    delete process.env.XYNCRA_DEBUG;
    const logger = new CLILogger();
    logger.debug('hello');
    expect(stderrSpy).not.toHaveBeenCalled();
  });

  test('debug output suppressed when XYNCRA_DEBUG is "0"', () => {
    process.env.XYNCRA_DEBUG = '0';
    const logger = new CLILogger();
    logger.debug('hello');
    expect(stderrSpy).not.toHaveBeenCalled();
  });

  test('debug output enabled when XYNCRA_DEBUG=1', () => {
    process.env.XYNCRA_DEBUG = '1';
    const logger = new CLILogger();
    logger.debug('hello');
    expect(stderrSpy).toHaveBeenCalledTimes(1);
    const written = stderrSpy.mock.calls[0][0] as string;
    expect(written).toContain('[DEBUG]');
    expect(written).toContain('hello');
  });

  test('debug output enabled when XYNCRA_DEBUG=true', () => {
    process.env.XYNCRA_DEBUG = 'true';
    const logger = new CLILogger();
    logger.debug('hello');
    expect(stderrSpy).toHaveBeenCalledTimes(1);
    const written = stderrSpy.mock.calls[0][0] as string;
    expect(written).toContain('[DEBUG]');
  });

  test('info/warn/error always output regardless of XYNCRA_DEBUG', () => {
    delete process.env.XYNCRA_DEBUG;
    const logger = new CLILogger();

    logger.info('info-msg');
    logger.warn('warn-msg');
    logger.error('error-msg');

    expect(stderrSpy).toHaveBeenCalledTimes(3);

    const infoLine = stderrSpy.mock.calls[0][0] as string;
    expect(infoLine).toContain('[INFO]');
    expect(infoLine).toContain('info-msg');

    const warnLine = stderrSpy.mock.calls[1][0] as string;
    expect(warnLine).toContain('[WARN]');
    expect(warnLine).toContain('warn-msg');

    const errorLine = stderrSpy.mock.calls[2][0] as string;
    expect(errorLine).toContain('[ERROR]');
    expect(errorLine).toContain('error-msg');
  });

  test('formats key-value args as key=value pairs', () => {
    process.env.XYNCRA_DEBUG = '1';
    const logger = new CLILogger();
    logger.info('event', 'user', 'alice', 'action', 'login');
    expect(stderrSpy).toHaveBeenCalledTimes(1);
    const written = stderrSpy.mock.calls[0][0] as string;
    expect(written).toContain('user=alice');
    expect(written).toContain('action=login');
  });

  test('log line starts with timestamp in brackets', () => {
    process.env.XYNCRA_DEBUG = '1';
    const logger = new CLILogger();
    logger.info('test');
    const written = stderrSpy.mock.calls[0][0] as string;
    // Format: [YYYY-MM-DD HH:mm:ss] [LEVEL] ...
    expect(written).toMatch(/^\[\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\]/);
  });
});
