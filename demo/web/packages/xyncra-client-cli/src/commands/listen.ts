/**
 * `listen` command — starts the daemon process.
 *
 * Mirrors Go newListenCommand in internal/cli/listen.go.
 * @module
 */

import type { Command } from 'commander';
import { runListen } from '../daemon.js';

/**
 * Register the `listen` command on the given Commander program.
 *
 * Usage: xyncra-client listen
 * Action: delegates to runListen() from daemon.js.
 */
export function registerListenCommand(program: Command): void {
  program
    .command('listen')
    .description('Start listening for message updates')
    .option('--device-info <json>', 'JSON object with device metadata (e.g. \'{"name":"MacBook","os":"darwin"}\')')
    .action(async (_options: unknown, cmd: Command) => {
      await runListen(cmd);
    });
}
