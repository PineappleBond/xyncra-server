/**
 * Ambient module declarations for packages without built-in types.
 */

declare module 'fs-ext' {
  import type { ErrnoException } from 'node:fs';

  /**
   * Apply or remove an advisory lock on an open file descriptor.
   *
   * @param fd       Open file descriptor.
   * @param operation Bitmask of LOCK_SH (1), LOCK_EX (2), LOCK_NB (4), LOCK_UN (8).
   * @param callback Called with `null` on success or an `ErrnoException` on failure.
   */
  export function flock(
    fd: number,
    operation: number,
    callback: (err: ErrnoException | null) => void,
  ): void;
}

/**
 * fake-indexeddb/auto polyfills the global indexedDB.
 * The package's exports map doesn't expose its type declarations correctly
 * under moduleResolution "bundler", so we declare the module here.
 */
declare module 'fake-indexeddb/auto';
