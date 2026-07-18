/**
 * `send` command — send a message to a conversation.
 *
 * Tries IPC first, then falls back to standalone WebSocket (D-032).
 * Mirrors Go newSendCommand in internal/cli/send.go.
 * @module
 */

import type { Command } from 'commander';
import { CLIContext } from '../cli-context.js';
import { IPCClient } from '../ipc.js';
import { standaloneRPC } from '../rpc-helper.js';

/** Shape of the send_message result returned by the server. */
interface SendMessageResult {
  message?: {
    message_id: number;
    id: string;
    conversation_id: string;
    client_message_id: string;
  } | null;
  duplicate: boolean;
}

/**
 * Register the `send` command on the given Commander program.
 *
 * Usage: xyncra-client send -c <conversation-id> -m <content> [--reply-to <id>] [--client-msg-id <uuid>]
 */
export function registerSendCommand(program: Command): void {
  program
    .command('send')
    .description('Send a message to a conversation')
    .requiredOption('-c, --conversation-id <id>', 'Conversation ID')
    .requiredOption('-m, --content <text>', 'Message content (empty string allowed)')
    .option('--reply-to <id>', 'Message ID to reply to', '0')
    .option('--client-msg-id <uuid>', 'Client message ID for idempotency (auto-generated UUID if empty)')
    .action(
      async (
        options: { conversationId: string; content: string; replyTo: string; clientMsgId?: string },
        cmd: Command,
      ) => {
        const cliCtx = CLIContext.fromCommand(cmd);
        const replyTo = parseInt(options.replyTo, 10) || 0;

        const params: Record<string, unknown> = {
          conversation_id: options.conversationId,
          content: options.content,
          reply_to: replyTo,
        };
        if (options.clientMsgId) {
          params.client_message_id = options.clientMsgId;
        }

        // Try IPC first.
        let result: SendMessageResult | undefined;
        let ipcErr: Error | undefined;
        try {
          const client = new IPCClient(cliCtx.getSocketPath());
          const resp = await client.call('send_message', params);
          if (resp.error) {
            ipcErr = new Error(resp.error.message);
          } else {
            result = resp.result as SendMessageResult;
          }
        } catch (err) {
          ipcErr = err as Error;
        }

        if (result) {
          printSendResult(result);
          return;
        }

        // Fallback to standalone WebSocket.
        let wsErr: Error | undefined;
        try {
          const wsResult = await standaloneRPC(cliCtx, 'send_message', params);
          result = wsResult as SendMessageResult;
        } catch (err) {
          wsErr = err as Error;
        }

        if (result) {
          printSendResult(result);
          return;
        }

        // Both modes failed — unified error.
        console.error('Error: Cannot send message.');
        console.error(`  Cause 1: ${ipcErr?.message ?? ipcErr}`);
        console.error(`  Cause 2: ${wsErr?.message ?? wsErr}`);
        console.error("Hint: Start the daemon first: xyncra-client listen --user-id <user>");
        process.exit(1);
      },
    );
}

/** Print the result of a successful send operation. */
function printSendResult(result: SendMessageResult): void {
  console.log('Message sent.');
  if (result.message) {
    console.log(`  Message ID: ${result.message.message_id}`);
    console.log(`  UUID: ${result.message.id}`);
    console.log(`  Conversation: ${result.message.conversation_id}`);
    console.log(`  Client Msg ID: ${result.message.client_message_id}`);
  }
  console.log(`  Duplicate: ${result.duplicate}`);
}
