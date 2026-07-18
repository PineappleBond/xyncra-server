/**
 * `logs` command — view and manage RPC and notification logs.
 *
 * All subcommands forward to the daemon via IPC since the daemon owns the DB.
 * Mirrors Go newLogsCommand in internal/cli/logs.go.
 * @module
 */

import type { Command } from 'commander';
import { CLIContext } from '../cli-context.js';
import { IPCClient } from '../ipc.js';

/**
 * Register the `logs` parent command with tail/search/stats/export/cleanup
 * subcommands.
 */
export function registerLogsCommand(program: Command): void {
  const logs = program
    .command('logs')
    .description('View and manage RPC and notification logs');

  // -----------------------------------------------------------------------
  // logs tail
  // -----------------------------------------------------------------------
  logs
    .command('tail')
    .description('Show recent log entries')
    .option('--type <type>', 'Log type: rpc or notifications', 'rpc')
    .option('--limit <n>', 'Maximum number of entries to show', '50')
    .option('--since <duration>', 'Show entries since (e.g. 1h, 30m, 7d)', '1h')
    .action(
      async (options: { type: string; limit: string; since: string }, cmd: Command) => {
        const cliCtx = CLIContext.fromCommand(cmd);
        const client = new IPCClient(cliCtx.getSocketPath());

        try {
          const resp = await client.call('logs_tail', {
            type: options.type,
            limit: parseInt(options.limit, 10),
            since: options.since,
          });
          if (resp.error) {
            console.error(`Error: logs tail: ${resp.error.message}`);
            process.exit(1);
          }
          const result = resp.result as {
            headers: string[];
            rows: string[][];
          };
          printTable(result.headers, result.rows);
        } catch {
          console.error('Error: daemon not running.');
          console.error("Hint: Start with 'xyncra-client listen --user-id <user>'");
          process.exit(2);
        }
      },
    );

  // -----------------------------------------------------------------------
  // logs search
  // -----------------------------------------------------------------------
  logs
    .command('search')
    .description('Search log entries with filters')
    .option('--type <type>', 'Log type: rpc or notifications', 'rpc')
    .option('--method <method>', 'Filter by RPC method')
    .option('--error', 'Show only error entries', false)
    .option('--from <time>', 'Start time (duration or RFC3339)')
    .option('--to <time>', 'End time (duration or RFC3339)')
    .option('--conversation-id <id>', 'Filter by conversation ID (RPC only)')
    .option('--request-id <id>', 'Get specific entry by request ID (RPC only)')
    .option('--limit <n>', 'Maximum number of entries to return', '100')
    .action(
      async (
        options: {
          type: string;
          method?: string;
          error: boolean;
          from?: string;
          to?: string;
          conversationId?: string;
          requestId?: string;
          limit: string;
        },
        cmd: Command,
      ) => {
        const cliCtx = CLIContext.fromCommand(cmd);
        const client = new IPCClient(cliCtx.getSocketPath());

        const params: Record<string, unknown> = {
          type: options.type,
          limit: parseInt(options.limit, 10),
        };
        if (options.method) params.method = options.method;
        if (options.error) params.error = true;
        if (options.from) params.from = options.from;
        if (options.to) params.to = options.to;
        if (options.conversationId) params.conversation_id = options.conversationId;
        if (options.requestId) params.request_id = options.requestId;

        try {
          const resp = await client.call('logs_search', params);
          if (resp.error) {
            console.error(`Error: logs search: ${resp.error.message}`);
            process.exit(1);
          }
          const result = resp.result as {
            headers: string[];
            rows: string[][];
          };
          printTable(result.headers, result.rows);
        } catch {
          console.error('Error: daemon not running.');
          console.error("Hint: Start with 'xyncra-client listen --user-id <user>'");
          process.exit(2);
        }
      },
    );

  // -----------------------------------------------------------------------
  // logs stats
  // -----------------------------------------------------------------------
  logs
    .command('stats')
    .description('Show RPC log statistics')
    .option('--since <duration>', 'Statistics time window (e.g. 1h, 24h, 7d)', '24h')
    .option('--interval <interval>', 'Group by interval: 1m, 5m, 15m, 1h, 1d')
    .action(
      async (options: { since: string; interval?: string }, cmd: Command) => {
        const cliCtx = CLIContext.fromCommand(cmd);
        const client = new IPCClient(cliCtx.getSocketPath());

        const params: Record<string, unknown> = { since: options.since };
        if (options.interval) params.interval = options.interval;

        try {
          const resp = await client.call('logs_stats', params);
          if (resp.error) {
            console.error(`Error: logs stats: ${resp.error.message}`);
            process.exit(1);
          }
          const result = resp.result as {
            headers: string[];
            rows: string[][];
          };
          printTable(result.headers, result.rows);
        } catch {
          console.error('Error: daemon not running.');
          console.error("Hint: Start with 'xyncra-client listen --user-id <user>'");
          process.exit(2);
        }
      },
    );

  // -----------------------------------------------------------------------
  // logs export
  // -----------------------------------------------------------------------
  logs
    .command('export')
    .description('Export logs to CSV or JSON')
    .option('--type <type>', 'Log type: rpc or notifications', 'rpc')
    .option('--format <fmt>', 'Export format: csv or json', 'csv')
    .option('-o, --output <path>', 'Output file path (default: stdout)')
    .option('--method <method>', 'Filter by RPC method (RPC only)')
    .option('--from <time>', 'Start time (duration or RFC3339)')
    .option('--to <time>', 'End time (duration or RFC3339)')
    .option('--limit <n>', 'Maximum number of entries to export (max 10000)', '1000')
    .action(
      async (
        options: {
          type: string;
          format: string;
          output?: string;
          method?: string;
          from?: string;
          to?: string;
          limit: string;
        },
        cmd: Command,
      ) => {
        const cliCtx = CLIContext.fromCommand(cmd);
        const client = new IPCClient(cliCtx.getSocketPath());

        const params: Record<string, unknown> = {
          type: options.type,
          format: options.format,
          limit: parseInt(options.limit, 10),
        };
        if (options.output) params.output = options.output;
        if (options.method) params.method = options.method;
        if (options.from) params.from = options.from;
        if (options.to) params.to = options.to;

        try {
          const resp = await client.call('logs_export', params);
          if (resp.error) {
            console.error(`Error: logs export: ${resp.error.message}`);
            process.exit(1);
          }
          const result = resp.result as { data?: string; message?: string };
          if (result.data) {
            process.stdout.write(result.data);
          }
          if (options.output && options.output !== '-') {
            console.error(`Exported to ${options.output}`);
          }
        } catch {
          console.error('Error: daemon not running.');
          console.error("Hint: Start with 'xyncra-client listen --user-id <user>'");
          process.exit(2);
        }
      },
    );

  // -----------------------------------------------------------------------
  // logs cleanup
  // -----------------------------------------------------------------------
  logs
    .command('cleanup')
    .description('Delete old log entries')
    .option('--retain <duration>', 'Retention duration (e.g. 168h, 7d)', '168h')
    .option('--dry-run', 'Show what would be deleted without deleting', false)
    .option('--type <type>', 'Log type to clean: rpc, notifications, or all', 'all')
    .action(
      async (
        options: { retain: string; dryRun: boolean; type: string },
        cmd: Command,
      ) => {
        const cliCtx = CLIContext.fromCommand(cmd);
        const client = new IPCClient(cliCtx.getSocketPath());

        try {
          const resp = await client.call('logs_cleanup', {
            retain: options.retain,
            dry_run: options.dryRun,
            type: options.type,
          });
          if (resp.error) {
            console.error(`Error: logs cleanup: ${resp.error.message}`);
            process.exit(1);
          }
          const result = resp.result as { message: string };
          console.log(result.message);
        } catch {
          console.error('Error: daemon not running.');
          console.error("Hint: Start with 'xyncra-client listen --user-id <user>'");
          process.exit(2);
        }
      },
    );
}

/** Print a table with headers and rows (auto-sized columns). */
function printTable(headers: string[], rows: string[][]): void {
  const colCount = headers.length;
  const widths = headers.map((h) => h.length);
  for (const row of rows) {
    for (let i = 0; i < colCount; i++) {
      widths[i] = Math.max(widths[i], (row[i] ?? '').length);
    }
  }
  const pad = (s: string, w: number): string => s.padEnd(w);
  console.log(headers.map((h, i) => pad(h, widths[i])).join('  '));
  for (const row of rows) {
    console.log(row.map((cell, i) => pad(cell ?? '', widths[i])).join('  '));
  }
}
