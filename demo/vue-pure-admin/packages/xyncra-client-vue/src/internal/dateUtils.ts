/**
 * Safely convert a date value (Date, string, null, or undefined) to an ISO string.
 * Returns undefined for invalid or missing values (prevents Invalid Date → "RangeError: Invalid time value").
 */
export function safeISODate(
  input: Date | string | null | undefined,
): string | undefined {
  if (input === null || input === undefined) return undefined;
  const d = input instanceof Date ? input : new Date(input);
  if (Number.isNaN(d.getTime())) return undefined;
  return d.toISOString();
}
