/**
 * `reload-agents` command — reload agent configurations from disk.
 *
 * IPC-only (D-036, D-076): the daemon manages agent config state; if the
 * daemon is not running there is nothing to reload.
 *
 * Mirrors Go newReloadAgentsCommand in internal/cli/reload_agents.go.
 * @module
 */

import type { Command } from 'commander';
import { CLIContext } from '../cli-context.js';
import { IPCClient } from '../ipc.js';

/**
 * Register the `reload-agents` command on the given Commander program.
 *
 * Usage: xyncra-client reload-agents
 */
export function registerReloadAgentsCommand(program: Command): void {
  program
    .command('reload-agents')
    .description('Reload agent configurations from disk (IPC-only, D-076)')
    .addHelpText(
      'after',
      `
Reload agent configurations from disk. The daemon picks up
any changes to agent definition files without requiring a restart.

This command is IPC-only (D-036, D-076) — it requires the listen daemon
to be running. Start the daemon with 'xyncra-client listen' first.`,
    )
    .action(async (_options: unknown, cmd: Command) => {
      const cliCtx = CLIContext.fromCommand(cmd);
      const client = new IPCClient(cliCtx.getSocketPath());

      try {
        const resp = await client.call('reload_agents');
        if (resp.error) {
          console.error(`Error: reload-agents: ${resp.error.message}`);
          process.exit(1);
        }

        const result = resp.result as { count: number } | undefined;
        const count = result?.count ?? 0;
        console.log(`Successfully reloaded ${count} agent configuration(s)`);
      } catch {
        // IPC connection failed — daemon is not running.
        console.error('Error: daemon not running.');
        console.error(`Hint: Start with 'xyncra-client listen --user-id ${cliCtx.userID}'`);
        process.exit(2);
      }
    });
}
