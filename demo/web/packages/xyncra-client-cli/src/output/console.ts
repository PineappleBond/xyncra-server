/**
 * Console output writer with tabwriter-equivalent alignment.
 *
 * Mirrors Go internal/cli/output/console.go exactly.
 *
 * @module
 */

import type { Writable } from 'node:stream';

/** Key-value pair for aligned display. */
export interface KeyValueEntry {
  key: string;
  value: string;
}

/**
 * ConsoleWriter writes formatted output to a Writable stream.
 * Mirrors Go ConsoleWriter.
 */
export class ConsoleWriter {
  private readonly w: Writable;

  constructor(w: Writable = process.stdout) {
    this.w = w;
  }

  /**
   * Write a table with headers and rows. Uses manual column-width alignment
   * (equivalent to Go text/tabwriter with minwidth=0, tabwidth=0, padding=2).
   */
  table(headers: string[], rows: string[][]): void {
    // Calculate column widths.
    const colWidths = headers.map((h) => h.length);
    for (const row of rows) {
      for (let i = 0; i < row.length; i++) {
        if (i < colWidths.length) {
          colWidths[i] = Math.max(colWidths[i], row[i].length);
        }
      }
    }

    const pad = (s: string, w: number): string => s + ' '.repeat(Math.max(0, w - s.length));

    // Print headers.
    const headerLine = headers.map((h, i) => pad(h, colWidths[i])).join('  ');
    this.w.write(headerLine + '\n');

    // Print separator.
    const sepLine = headers.map((h, i) => '-'.repeat(h.length) + (i < headers.length - 1 ? '  ' : '')).join('');
    // Simpler: just use dashes with same column widths
    const sepLine2 = colWidths.map((w, i) => '-'.repeat(w) + (i < colWidths.length - 1 ? '  ' : '')).join('');
    this.w.write(sepLine2 + '\n');

    // Print rows.
    for (const row of rows) {
      const line = row.map((cell, i) => {
        const w = i < colWidths.length ? colWidths[i] : cell.length;
        return pad(cell, w);
      }).join('  ');
      this.w.write(line + '\n');
    }
  }

  /** Write aligned key-value pairs. */
  keyValue(pairs: KeyValueEntry[]): void {
    if (pairs.length === 0) return;
    const maxKeyLen = Math.max(...pairs.map((p) => p.key.length));
    for (const p of pairs) {
      this.w.write(`${p.key.padEnd(maxKeyLen)}  ${p.value}\n`);
    }
  }

  /** Write v as indented JSON. */
  jsonPretty(v: unknown): void {
    this.w.write(JSON.stringify(v, null, 2) + '\n');
  }

  /** Write a section title followed by a newline. */
  section(title: string): void {
    this.w.write(title + '\n');
  }

  /** Write an informational message. */
  info(msg: string): void {
    this.w.write(msg + '\n');
  }

  /** Write an error message. */
  error(msg: string): void {
    this.w.write(`Error: ${msg}\n`);
  }
}
