/**
 * Message commands — delete, mark-as-read, get, search.
 *
 * delete/mark-as-read: IPC first, standalone fallback (D-032).
 * get-messages/search-messages: IPC only (reads from daemon's local DB, D-035).
 *
 * Mirrors Go message commands in internal/cli/messages.go.
 * @module
 */

import type { Command } from 'commander';
import { CLIContext } from '../cli-context.js';
import { IPCClient } from '../ipc.js';
import { standaloneRPC } from '../rpc-helper.js';

/** Shape of a message for list display. */
interface Message {
  message_id: number;
  sender_id: string;
  content: string;
  created_at: string;
}

/**
 * Register message commands on the given Commander program.
 */
export function registerMessageCommands(program: Command): void {
  // -----------------------------------------------------------------------
  // delete-message (IPC first, standalone fallback)
  // -----------------------------------------------------------------------
  program
    .command('delete-message')
    .description('Soft-delete a message (sender only — D-014)')
    .requiredOption('--message-id <uuid>', 'Message UUID to delete')
    .action(async (options: { messageId: string }, cmd: Command) => {
      const cliCtx = CLIContext.fromCommand(cmd);
      const params = { message_id: options.messageId };

      let success = false;
      let ipcErr: Error | undefined;
      try {
        const client = new IPCClient(cliCtx.getSocketPath());
        const resp = await client.call('delete_message', params);
        if (resp.error) {
          ipcErr = new Error(resp.error.message);
        } else {
          success = true;
        }
      } catch (err) {
        ipcErr = err as Error;
      }

      if (success) {
        console.log('Message deleted.');
        return;
      }

      let wsErr: Error | undefined;
      try {
        await standaloneRPC(cliCtx, 'delete_message', params);
        success = true;
      } catch (err) {
        wsErr = err as Error;
      }

      if (success) {
        console.log('Message deleted.');
        return;
      }

      console.error('Error: Cannot delete message.');
      console.error(`  Cause 1: ${ipcErr?.message ?? ipcErr}`);
      console.error(`  Cause 2: ${wsErr?.message ?? wsErr}`);
      console.error("Hint: Start the daemon first: xyncra-client listen --user-id <user>");
      process.exit(1);
    });

  // -----------------------------------------------------------------------
  // mark-as-read (IPC first, standalone fallback)
  // -----------------------------------------------------------------------
  program
    .command('mark-as-read')
    .description('Mark messages as read in a conversation (D-012)')
    .requiredOption('-c, --conversation-id <id>', 'Conversation ID')
    .option('--message-id <n>', 'Message sequence number (0 = mark all as read)', '0')
    .action(async (options: { conversationId: string; messageId: string }, cmd: Command) => {
      const cliCtx = CLIContext.fromCommand(cmd);
      const msgID = parseInt(options.messageId, 10) || 0;
      const params = { conversation_id: options.conversationId, message_id: msgID };

      let confirmedID: number | undefined;
      let ipcErr: Error | undefined;
      try {
        const client = new IPCClient(cliCtx.getSocketPath());
        const resp = await client.call('mark_as_read', params);
        if (resp.error) {
          ipcErr = new Error(resp.error.message);
        } else {
          const result = resp.result as { last_read_message_id: number };
          confirmedID = result.last_read_message_id;
        }
      } catch (err) {
        ipcErr = err as Error;
      }

      if (confirmedID === undefined) {
        let wsErr: Error | undefined;
        try {
          const wsResult = await standaloneRPC(cliCtx, 'mark_as_read', params);
          const result = wsResult as { last_read_message_id: number };
          confirmedID = result.last_read_message_id;
        } catch (err) {
          wsErr = err as Error;
        }

        if (confirmedID === undefined) {
          console.error('Error: Cannot mark as read.');
          console.error(`  Cause 1: ${ipcErr?.message ?? ipcErr}`);
          console.error(`  Cause 2: ${wsErr?.message ?? wsErr}`);
          console.error("Hint: Start the daemon first: xyncra-client listen --user-id <user>");
          process.exit(1);
        }
      }

      console.log(`Marked as read up to message #${confirmedID}.`);
    });

  // -----------------------------------------------------------------------
  // get-messages (IPC-only — reads from daemon's local DB, D-035)
  // -----------------------------------------------------------------------
  program
    .command('get-messages')
    .description('List messages from local database')
    .requiredOption('-c, --conversation-id <id>', 'Conversation ID')
    .option('--after-message-id <n>', 'Show messages after this sequence number', '0')
    .option('--limit <n>', 'Maximum number of messages to show', '50')
    .action(
      async (
        options: { conversationId: string; afterMessageId: string; limit: string },
        cmd: Command,
      ) => {
        const cliCtx = CLIContext.fromCommand(cmd);
        const afterMsgID = parseInt(options.afterMessageId, 10);
        const limit = parseInt(options.limit, 10);

        const client = new IPCClient(cliCtx.getSocketPath());
        try {
          const resp = await client.call('get_messages', {
            conversation_id: options.conversationId,
            after_message_id: afterMsgID,
            limit,
          });
          if (resp.error) {
            console.error(`Error: get-messages: ${resp.error.message}`);
            process.exit(1);
          }
          const result = resp.result as { messages: Message[]; has_more: boolean };
          printMessageList(result.messages, result.has_more);
        } catch {
          console.error('Error: daemon not running.');
          console.error("Hint: Start with 'xyncra-client listen --user-id <user>'");
          process.exit(2);
        }
      },
    );

  // -----------------------------------------------------------------------
  // search-messages (IPC-only — reads from daemon's local DB, D-035)
  // -----------------------------------------------------------------------
  program
    .command('search-messages')
    .description('Search messages in local database')
    .requiredOption('-c, --conversation-id <id>', 'Conversation ID')
    .requiredOption('-q, --query <text>', 'Search query')
    .option('--after-message-id <n>', 'Pagination cursor', '0')
    .option('--limit <n>', 'Maximum number of messages to show', '50')
    .action(
      async (
        options: {
          conversationId: string;
          query: string;
          afterMessageId: string;
          limit: string;
        },
        cmd: Command,
      ) => {
        const cliCtx = CLIContext.fromCommand(cmd);
        const afterMsgID = parseInt(options.afterMessageId, 10);
        const limit = parseInt(options.limit, 10);

        const client = new IPCClient(cliCtx.getSocketPath());
        try {
          const resp = await client.call('search_messages', {
            conversation_id: options.conversationId,
            query: options.query,
            after_message_id: afterMsgID,
            limit,
          });
          if (resp.error) {
            console.error(`Error: search-messages: ${resp.error.message}`);
            process.exit(1);
          }
          const result = resp.result as { messages: Message[]; has_more: boolean };
          printMessageList(result.messages, result.has_more);
        } catch {
          console.error('Error: daemon not running.');
          console.error("Hint: Start with 'xyncra-client listen --user-id <user>'");
          process.exit(2);
        }
      },
    );
}

/**
 * Print a list of messages in the Go format:
 * [#MessageID] SenderID (HH:MM): Content
 */
function printMessageList(msgs: Message[], hasMore: boolean): void {
  if (!msgs || msgs.length === 0) {
    console.log('No messages found.');
    return;
  }
  for (const msg of msgs) {
    const t = formatHHMM(msg.created_at);
    console.log(`[#${msg.message_id}] ${msg.sender_id} (${t}): ${msg.content}`);
  }
  if (hasMore) {
    console.log('(Use --after-message-id to see more)');
  }
}

/** Format an ISO timestamp to "HH:MM" (matches Go "15:04"). */
function formatHHMM(iso: string): string {
  try {
    const d = new Date(iso);
    const pad = (n: number): string => String(n).padStart(2, '0');
    return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
  } catch {
    return iso;
  }
}
