import { safeISODate } from '../../internal/dateUtils';

describe('safeISODate', () => {
  it('should return undefined for null', () => {
    expect(safeISODate(null)).toBeUndefined();
  });

  it('should return undefined for undefined', () => {
    expect(safeISODate(undefined)).toBeUndefined();
  });

  it('should return undefined for Invalid Date', () => {
    expect(safeISODate(new Date('invalid'))).toBeUndefined();
  });

  it('should return undefined for empty string', () => {
    expect(safeISODate('')).toBeUndefined();
  });

  it('should return ISO string for a valid Date object', () => {
    const d = new Date('2026-01-01T00:00:00Z');
    expect(safeISODate(d)).toBe(d.toISOString());
  });

  it('should return ISO string for a valid ISO string', () => {
    const iso = '2026-02-03T04:05:06.000Z';
    expect(safeISODate(iso)).toBe(new Date(iso).toISOString());
  });
});
