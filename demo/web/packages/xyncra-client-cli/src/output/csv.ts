/**
 * CSV/JSON export output helper.
 *
 * Mirrors Go internal/cli/output/csv.go.
 *
 * @module
 */

import { createWriteStream, type WriteStream } from 'node:fs';
import type { Writable } from 'node:stream';

/**
 * A writable output destination that can be closed.
 * When path is empty or "-", writes to stdout (close is no-op).
 */
export interface ExportOutput {
  writer: Writable;
  close(): void;
}

/**
 * Open an output destination for export data.
 * If path is empty or "-", returns stdout. Otherwise creates/truncates the file.
 */
export function openExportOutput(path: string): ExportOutput {
  if (!path || path === '-') {
    return {
      writer: process.stdout,
      close() {
        // no-op for stdout
      },
    };
  }
  const stream: WriteStream = createWriteStream(path);
  return {
    writer: stream,
    close() {
      stream.end();
    },
  };
}
