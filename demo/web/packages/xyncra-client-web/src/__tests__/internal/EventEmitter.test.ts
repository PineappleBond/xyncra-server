import { TypedEventEmitter } from '../../internal/EventEmitter';

describe('TypedEventEmitter', () => {
  let emitter: TypedEventEmitter<{
    test: string;
    data: { value: number };
    empty: undefined;
  }>;

  beforeEach(() => {
    emitter = new TypedEventEmitter();
  });

  describe('on', () => {
    it('should register and receive events', () => {
      const callback = jest.fn();
      emitter.on('test', callback);
      emitter.emit('test', 'hello');
      expect(callback).toHaveBeenCalledWith('hello');
    });

    it('should support multiple listeners for the same event', () => {
      const cb1 = jest.fn();
      const cb2 = jest.fn();
      emitter.on('test', cb1);
      emitter.on('test', cb2);
      emitter.emit('test', 'hello');
      expect(cb1).toHaveBeenCalledWith('hello');
      expect(cb2).toHaveBeenCalledWith('hello');
    });

    it('should return an unsubscribe function', () => {
      const callback = jest.fn();
      const unsub = emitter.on('test', callback);
      unsub();
      emitter.emit('test', 'hello');
      expect(callback).not.toHaveBeenCalled();
    });

    it('should clean up empty listener sets after unsubscribe', () => {
      const callback = jest.fn();
      const unsub = emitter.on('test', callback);
      expect(emitter.listenerCount('test')).toBe(1);
      unsub();
      expect(emitter.listenerCount('test')).toBe(0);
    });
  });

  describe('once', () => {
    it('should fire listener only once', () => {
      const callback = jest.fn();
      emitter.once('test', callback);
      emitter.emit('test', 'first');
      emitter.emit('test', 'second');
      expect(callback).toHaveBeenCalledTimes(1);
      expect(callback).toHaveBeenCalledWith('first');
    });

    it('should return an unsubscribe function that prevents firing', () => {
      const callback = jest.fn();
      const unsub = emitter.once('test', callback);
      unsub();
      emitter.emit('test', 'hello');
      expect(callback).not.toHaveBeenCalled();
    });
  });

  describe('emit', () => {
    it('should not throw when no listeners are registered', () => {
      expect(() => emitter.emit('test', 'hello')).not.toThrow();
    });

    it('should swallow listener errors', () => {
      const badCallback = jest.fn(() => {
        throw new Error('listener error');
      });
      const goodCallback = jest.fn();
      emitter.on('test', badCallback);
      emitter.on('test', goodCallback);
      emitter.emit('test', 'hello');
      expect(badCallback).toHaveBeenCalled();
      expect(goodCallback).toHaveBeenCalled();
    });

    it('should pass object payloads correctly', () => {
      const callback = jest.fn();
      emitter.on('data', callback);
      emitter.emit('data', { value: 42 });
      expect(callback).toHaveBeenCalledWith({ value: 42 });
    });
  });

  describe('off', () => {
    it('should remove all listeners for a specific event', () => {
      const cb1 = jest.fn();
      const cb2 = jest.fn();
      emitter.on('test', cb1);
      emitter.on('data', cb2);
      emitter.off('test');
      emitter.emit('test', 'hello');
      emitter.emit('data', { value: 1 });
      expect(cb1).not.toHaveBeenCalled();
      expect(cb2).toHaveBeenCalled();
    });

    it('should remove all listeners when called without arguments', () => {
      const cb1 = jest.fn();
      const cb2 = jest.fn();
      emitter.on('test', cb1);
      emitter.on('data', cb2);
      emitter.off();
      emitter.emit('test', 'hello');
      emitter.emit('data', { value: 1 });
      expect(cb1).not.toHaveBeenCalled();
      expect(cb2).not.toHaveBeenCalled();
    });
  });

  describe('removeAllListeners', () => {
    it('should remove all listeners for all events', () => {
      const cb1 = jest.fn();
      const cb2 = jest.fn();
      emitter.on('test', cb1);
      emitter.on('data', cb2);
      emitter.removeAllListeners();
      emitter.emit('test', 'hello');
      emitter.emit('data', { value: 1 });
      expect(cb1).not.toHaveBeenCalled();
      expect(cb2).not.toHaveBeenCalled();
    });
  });

  describe('listenerCount', () => {
    it('should return 0 for events with no listeners', () => {
      expect(emitter.listenerCount('test')).toBe(0);
    });

    it('should return the correct count', () => {
      emitter.on('test', jest.fn());
      emitter.on('test', jest.fn());
      expect(emitter.listenerCount('test')).toBe(2);
    });

    it('should decrease after unsubscribe', () => {
      const unsub = emitter.on('test', jest.fn());
      emitter.on('test', jest.fn());
      expect(emitter.listenerCount('test')).toBe(2);
      unsub();
      expect(emitter.listenerCount('test')).toBe(1);
    });
  });
});
