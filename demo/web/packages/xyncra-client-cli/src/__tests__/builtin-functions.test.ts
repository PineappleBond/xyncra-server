/**
 * Tests for builtin-functions.ts — built-in diagnostic function metadata.
 */

import { builtinFunctionInfos } from '../builtin-functions';

describe('builtinFunctionInfos', () => {
  const infos = builtinFunctionInfos();

  test('returns exactly 3 functions', () => {
    expect(infos).toHaveLength(3);
  });

  test('all names are unique', () => {
    const names = infos.map((i) => i.name);
    expect(new Set(names).size).toBe(names.length);
  });

  test('all have non-empty name and description', () => {
    for (const info of infos) {
      expect(info.name).toBeTruthy();
      expect(info.description).toBeTruthy();
    }
  });

  test('all have "diagnostic" tag', () => {
    for (const info of infos) {
      expect(info.tags).toContain('diagnostic');
    }
  });

  describe('ping', () => {
    const info = infos.find((i) => i.name === 'ping')!;

    test('exists', () => {
      expect(info).toBeDefined();
    });

    test('has correct name', () => {
      expect(info.name).toBe('ping');
    });

    test('has a description', () => {
      expect(info.description).toMatch(/echo/i);
    });

    test('has parameters schema', () => {
      expect(info.parameters).toBeDefined();
      expect(info.parameters!.type).toBe('object');
      const props = info.parameters!.properties as Record<string, unknown>;
      expect(props).toBeDefined();
      expect(props.message).toBeDefined();
    });

    test('has returns', () => {
      expect(info.returns).toBeDefined();
      expect(info.returns!.type).toBe('object');
    });

    test('has diagnostic tag', () => {
      expect(info.tags).toContain('diagnostic');
    });
  });

  describe('get_device_info', () => {
    const info = infos.find((i) => i.name === 'get_device_info')!;

    test('exists', () => {
      expect(info).toBeDefined();
    });

    test('has correct name', () => {
      expect(info.name).toBe('get_device_info');
    });

    test('has a description', () => {
      expect(info.description).toMatch(/device/i);
    });

    test('has parameters (empty object)', () => {
      expect(info.parameters).toBeDefined();
      expect(info.parameters!.type).toBe('object');
    });

    test('has returns', () => {
      expect(info.returns).toBeDefined();
      expect(info.returns!.type).toBe('object');
    });
  });

  describe('get_time', () => {
    const info = infos.find((i) => i.name === 'get_time')!;

    test('exists', () => {
      expect(info).toBeDefined();
    });

    test('has correct name', () => {
      expect(info.name).toBe('get_time');
    });

    test('has a description', () => {
      expect(info.description).toMatch(/time/i);
    });

    test('has parameters (empty object)', () => {
      expect(info.parameters).toBeDefined();
      expect(info.parameters!.type).toBe('object');
    });

    test('has returns', () => {
      expect(info.returns).toBeDefined();
      expect(info.returns!.type).toBe('object');
    });
  });
});
