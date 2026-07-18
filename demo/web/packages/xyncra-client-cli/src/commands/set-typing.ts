/**
 * `set-typing` command — send a typing indicator to a conversation.
 *
 * IPC-only (D-036): typing is fire-and-forget (D-050); if the daemon is not
 * running there is nothing to broadcast, so standalone fallback is pointless.
 *
 * Mirrors Go newSetTypingCommand in internal/cli/set_typing.go.
 * @module
 */

import type { Command } from 'commander';
import { CLIContext } from '../cli-context.js';
import { IPCClient } from '../ipc.js';

/**
 * Register the `set-typing` command on the given Commander program.
 *
 * Usage: xyncra-client set-typing -c <conversation-id> [--stop]
 */
export function registerSetTypingCommand(program: Command): void {
  program
    .command('set-typing')
    .description('Send a typing indicator to a conversation (D-050)')
    .requiredOption('-c, --conversation-id <id>', 'Conversation ID')
    .option('--stop', 'Stop typing (default: start typing)', false)
    .action(async (options: { conversationId: string; stop: boolean }, cmd: Command) => {
      const cliCtx = CLIContext.fromCommand(cmd);
      const client = new IPCClient(cliCtx.getSocketPath());

      try {
        const resp = await client.call('set_typing', {
          conversation_id: options.conversationId,
          is_typing: !options.stop,
        });
        if (resp.error) {
          console.error(`Error: set-typing: ${resp.error.message}`);
          process.exit(1);
        }

        if (options.stop) {
          console.log(`Typing indicator cleared for conversation ${options.conversationId}`);
        } else {
          console.log(`Typing indicator sent to conversation ${options.conversationId}`);
        }
      } catch {
        // IPC connection failed — daemon is not running.
        console.error('Error: daemon not running.');
        console.error("Hint: Start with 'xyncra-client listen --user-id <user>'");
        process.exit(2);
      }
    });
}
