import { ConsoleLogger } from '../../adapters/logger';

describe('ConsoleLogger', () => {
  let logger: ConsoleLogger;

  beforeEach(() => {
    jest.spyOn(console, 'debug').mockImplementation();
    jest.spyOn(console, 'info').mockImplementation();
    jest.spyOn(console, 'warn').mockImplementation();
    jest.spyOn(console, 'error').mockImplementation();
    logger = new ConsoleLogger();
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('should use default prefix', () => {
    logger.debug('test message');
    expect(console.debug).toHaveBeenCalledWith('[xyncra] test message');
  });

  it('should use custom prefix', () => {
    const custom = new ConsoleLogger('[custom]');
    custom.info('hello');
    expect(console.info).toHaveBeenCalledWith('[custom] hello');
  });

  it('should delegate debug to console.debug', () => {
    logger.debug('debug msg', { extra: 1 });
    expect(console.debug).toHaveBeenCalledWith('[xyncra] debug msg', {
      extra: 1,
    });
  });

  it('should delegate info to console.info', () => {
    logger.info('info msg');
    expect(console.info).toHaveBeenCalledWith('[xyncra] info msg');
  });

  it('should delegate warn to console.warn', () => {
    logger.warn('warn msg');
    expect(console.warn).toHaveBeenCalledWith('[xyncra] warn msg');
  });

  it('should delegate error to console.error', () => {
    logger.error('error msg', new Error('test'));
    expect(console.error).toHaveBeenCalledWith(
      '[xyncra] error msg',
      expect.any(Error),
    );
  });
});
