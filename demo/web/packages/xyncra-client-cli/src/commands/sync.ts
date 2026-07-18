/**
 * `sync-updates` command — trigger a full sync via the daemon.
 *
 * IPC-only (D-036): there is no standalone WebSocket fallback because
 * opening a second WebSocket would compete with the daemon's syncManager
 * for SQLite writes and localMaxSeq state.
 *
 * Mirrors Go newSyncUpdatesCommand in internal/cli/sync.go.
 * @module
 */

import type { Command } from 'commander';
import { CLIContext } from '../cli-context.js';
import { IPCClient } from '../ipc.js';

/**
 * Register the `sync-updates` command on the given Commander program.
 *
 * Usage: xyncra-client sync-updates
 */
export function registerSyncCommand(program: Command): void {
  program
    .command('sync-updates')
    .description('Trigger a full sync of updates from the server via the daemon')
    .action(async (_options: unknown, cmd: Command) => {
      const cliCtx = CLIContext.fromCommand(cmd);
      const client = new IPCClient(cliCtx.getSocketPath());

      try {
        const resp = await client.call('sync_updates');
        if (resp.error) {
          console.error(`Error: sync-updates: ${resp.error.message}`);
          process.exit(1);
        }
        console.log('Sync complete.');
      } catch {
        // IPC connection failed — daemon is not running.
        console.error('Error: daemon not running.');
        console.error("Hint: Start with 'xyncra-client listen --user-id <user>'");
        process.exit(2);
      }
    });
}
