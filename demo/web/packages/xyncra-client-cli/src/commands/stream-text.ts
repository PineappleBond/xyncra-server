/**
 * `stream-text` command — send streaming text to a conversation.
 *
 * IPC-only (D-036): streaming is fire-and-forget (D-051); if the daemon is not
 * running there is nothing to broadcast, so standalone fallback is pointless.
 *
 * Mirrors Go newStreamTextCommand in internal/cli/stream_text.go.
 * @module
 */

import type { Command } from 'commander';
import { CLIContext } from '../cli-context.js';
import { IPCClient } from '../ipc.js';

/**
 * Register the `stream-text` command on the given Commander program.
 *
 * Usage: xyncra-client stream-text -c <conversation-id> --stream-id <id> --text <text> [--done]
 */
export function registerStreamTextCommand(program: Command): void {
  program
    .command('stream-text')
    .description('Send streaming text to a conversation (D-051)')
    .requiredOption('-c, --conversation-id <id>', 'Conversation ID')
    .requiredOption('--stream-id <id>', 'Stream ID (client-generated UUID)')
    .requiredOption('--text <text>', 'Cumulative text content')
    .option('--done', 'Mark stream as done (is_done=true)', false)
    .action(
      async (
        options: { conversationId: string; streamId: string; text: string; done: boolean },
        cmd: Command,
      ) => {
        const cliCtx = CLIContext.fromCommand(cmd);
        const client = new IPCClient(cliCtx.getSocketPath());

        try {
          const resp = await client.call('stream_text', {
            conversation_id: options.conversationId,
            stream_id: options.streamId,
            text: options.text,
            is_done: options.done,
          });
          if (resp.error) {
            console.error(`Error: stream-text: ${resp.error.message}`);
            process.exit(1);
          }

          if (options.done) {
            console.log(`Streaming done sent to conversation ${options.conversationId}`);
          } else {
            console.log(`Streaming text sent to conversation ${options.conversationId}`);
          }
        } catch {
          // IPC connection failed — daemon is not running.
          console.error('Error: daemon not running.');
          console.error("Hint: Start with 'xyncra-client listen --user-id <user>'");
          process.exit(2);
        }
      },
    );
}
