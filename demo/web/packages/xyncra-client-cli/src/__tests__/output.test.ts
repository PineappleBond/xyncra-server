/**
 * Tests for output/console.ts and output/csv.ts — ConsoleWriter and openExportOutput.
 */

import { mkdtempSync, rmSync, readFileSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Writable } from 'node:stream';

import { ConsoleWriter } from '../output/console';
import { openExportOutput } from '../output/csv';

/** In-memory writable stream for capturing output. */
class StringWritable extends Writable {
  chunks: string[] = [];

  _write(chunk: Buffer | string, _enc: string, cb: () => void) {
    this.chunks.push(chunk.toString('utf8'));
    cb();
  }

  toString(): string {
    return this.chunks.join('');
  }
}

describe('ConsoleWriter', () => {
  let sink: StringWritable;
  let writer: ConsoleWriter;

  beforeEach(() => {
    sink = new StringWritable();
    writer = new ConsoleWriter(sink);
  });

  describe('table()', () => {
    test('correct column alignment', () => {
      writer.table(['ID', 'Name'], [
        ['1', 'Alice'],
        ['22', 'Bob'],
      ]);
      const output = sink.toString();
      const lines = output.split('\n').filter(Boolean);
      expect(lines.length).toBe(4); // header, separator, 2 data rows

      // Column widths: ID=2, Name=5 (because "Alice" is 5 chars).
      // All columns (including last) are padded to column width.
      // Header: pad("ID",2) + "  " + pad("Name",5) = "ID  Name "
      expect(lines[0]).toBe('ID  Name ');
      // Separator: dashes matching column widths with 2-space gap.
      expect(lines[1]).toBe('--  -----');

      // Data rows padded the same way.
      // "1 " + "  " + "Alice" = "1   Alice"
      expect(lines[2]).toBe('1   Alice');
      // "22" + "  " + "Bob  " = "22  Bob  "
      expect(lines[3]).toBe('22  Bob  ');
    });

    test('single column table', () => {
      writer.table(['Col'], [['a'], ['bb']]);
      const lines = sink.toString().split('\n').filter(Boolean);
      expect(lines.length).toBe(4);
    });

    test('column widths adapt to widest cell', () => {
      writer.table(['K', 'V'], [
        ['short', 'value'],
        ['a-very-long-key', 'x'],
      ]);
      const lines = sink.toString().split('\n').filter(Boolean);
      // K column width: max(1, 5, 15) = 15
      // V column width: max(1, 5, 1) = 5
      const header = lines[0];
      // pad("K",15) + "  " + pad("V",5) = 15 + 2 + 5 = 22
      expect(header.length).toBe(15 + 2 + 5);
    });
  });

  describe('keyValue()', () => {
    test('aligned key-value pairs', () => {
      writer.keyValue([
        { key: 'Name', value: 'Alice' },
        { key: 'Age', value: '30' },
        { key: 'Long Key', value: 'test' },
      ]);
      const lines = sink.toString().split('\n').filter(Boolean);
      expect(lines.length).toBe(3);

      // Max key length is 8 ("Long Key").
      // "Name      Alice" (Name + 4 spaces + Alice)
      expect(lines[0]).toBe('Name      Alice');
      expect(lines[1]).toBe('Age       30');
      expect(lines[2]).toBe('Long Key  test');
    });

    test('empty pairs produces no output', () => {
      writer.keyValue([]);
      expect(sink.toString()).toBe('');
    });
  });

  describe('jsonPretty()', () => {
    test('indented JSON', () => {
      writer.jsonPretty({ a: 1, b: [1, 2] });
      const output = sink.toString();
      // Should match JSON.stringify(..., null, 2) + newline.
      const expected = JSON.stringify({ a: 1, b: [1, 2] }, null, 2) + '\n';
      expect(output).toBe(expected);
    });

    test('handles null and primitives', () => {
      writer.jsonPretty(null);
      expect(sink.toString().trim()).toBe('null');
    });
  });

  describe('section()', () => {
    test('writes title with newline', () => {
      writer.section('My Section');
      expect(sink.toString()).toBe('My Section\n');
    });
  });

  describe('info()', () => {
    test('writes message with newline', () => {
      writer.info('hello');
      expect(sink.toString()).toBe('hello\n');
    });
  });

  describe('error()', () => {
    test('writes Error: prefix', () => {
      writer.error('oops');
      expect(sink.toString()).toBe('Error: oops\n');
    });
  });
});

describe('openExportOutput', () => {
  let tmpDir: string;

  beforeEach(() => {
    tmpDir = mkdtempSync(join(tmpdir(), 'xyncra-out-'));
  });

  afterEach(() => {
    rmSync(tmpDir, { recursive: true, force: true });
  });

  test('returns stdout for empty path', () => {
    const out = openExportOutput('');
    expect(out.writer).toBe(process.stdout);
    expect(() => out.close()).not.toThrow();
  });

  test('returns stdout for "-" path', () => {
    const out = openExportOutput('-');
    expect(out.writer).toBe(process.stdout);
    expect(() => out.close()).not.toThrow();
  });

  test('creates a file for non-empty path', async () => {
    const fp = join(tmpDir, 'out.txt');
    const out = openExportOutput(fp);
    out.writer.write('hello\n');
    out.close();

    // Wait for close to flush.
    await new Promise((r) => setTimeout(r, 100));

    expect(existsSync(fp)).toBe(true);
    expect(readFileSync(fp, 'utf8')).toBe('hello\n');
  });
});
