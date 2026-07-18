/**
 * Conversation commands — create, delete, restore, list, get.
 *
 * create/delete/restore: IPC first, standalone fallback (D-032).
 * list/get: IPC only (reads from daemon's local DB — D-035).
 *
 * Mirrors Go conversation commands in internal/cli/conversations.go.
 * @module
 */

import type { Command } from 'commander';
import { CLIContext } from '../cli-context.js';
import { IPCClient } from '../ipc.js';
import { standaloneRPC } from '../rpc-helper.js';

/** Shape of the create_conversation result. */
interface CreateConversationResult {
  conversation?: {
    id: string;
    user_id2: string;
    title: string;
  } | null;
  duplicate: boolean;
}

/** Shape of the delete_conversation result. */
interface DeleteConversationResult {
  deleted_message_count: number;
}

/** Shape of the restore_conversation result. */
interface RestoreConversationResult {
  restored_message_count: number;
}

/** Shape of a conversation for list/detail display. */
interface Conversation {
  id: string;
  type: string;
  user_id1: string;
  user_id2: string;
  title: string;
  created_at: string;
  last_message_at: string;
  last_read_message_id1: number;
  last_read_message_id2: number;
  last_processed_message_id: number;
}

/**
 * Register conversation commands on the given Commander program.
 */
export function registerConversationCommands(program: Command): void {
  // -----------------------------------------------------------------------
  // create-conversation
  // -----------------------------------------------------------------------
  program
    .command('create-conversation')
    .description('Create a 1-on-1 conversation with another user')
    .requiredOption('--peer-id <id>', 'Peer user ID')
    .option('--title <title>', 'Conversation title', '')
    .action(
      async (options: { peerId: string; title: string }, cmd: Command) => {
        const cliCtx = CLIContext.fromCommand(cmd);
        const params = { user_id2: options.peerId, title: options.title };

        let result: CreateConversationResult | undefined;
        let ipcErr: Error | undefined;
        try {
          const client = new IPCClient(cliCtx.getSocketPath());
          const resp = await client.call('create_conversation', params);
          if (resp.error) {
            ipcErr = new Error(resp.error.message);
          } else {
            result = resp.result as CreateConversationResult;
          }
        } catch (err) {
          ipcErr = err as Error;
        }

        if (!result) {
          let wsErr: Error | undefined;
          try {
            // Standalone uses user_id (not user_id2).
            const wsResult = await standaloneRPC(cliCtx, 'create_conversation', {
              user_id: options.peerId,
              title: options.title,
            });
            result = wsResult as CreateConversationResult;
          } catch (err) {
            wsErr = err as Error;
          }

          if (!result) {
            console.error('Error: Cannot create conversation.');
            console.error(`  Cause 1: ${ipcErr?.message ?? ipcErr}`);
            console.error(`  Cause 2: ${wsErr?.message ?? wsErr}`);
            console.error("Hint: Start the daemon first: xyncra-client listen --user-id <user>");
            process.exit(1);
          }
        }

        printCreateConversationResult(result);
      },
    );

  // -----------------------------------------------------------------------
  // delete-conversation
  // -----------------------------------------------------------------------
  program
    .command('delete-conversation')
    .description('Soft-delete a conversation and all its messages')
    .requiredOption('-c, --conversation-id <id>', 'Conversation ID')
    .action(async (options: { conversationId: string }, cmd: Command) => {
      const cliCtx = CLIContext.fromCommand(cmd);
      const params = { conversation_id: options.conversationId };

      let result: DeleteConversationResult | undefined;
      let ipcErr: Error | undefined;
      try {
        const client = new IPCClient(cliCtx.getSocketPath());
        const resp = await client.call('delete_conversation', params);
        if (resp.error) {
          ipcErr = new Error(resp.error.message);
        } else {
          result = resp.result as DeleteConversationResult;
        }
      } catch (err) {
        ipcErr = err as Error;
      }

      if (!result) {
        let wsErr: Error | undefined;
        try {
          const wsResult = await standaloneRPC(cliCtx, 'delete_conversation', params);
          result = wsResult as DeleteConversationResult;
        } catch (err) {
          wsErr = err as Error;
        }

        if (!result) {
          console.error('Error: Cannot delete conversation.');
          console.error(`  Cause 1: ${ipcErr?.message ?? ipcErr}`);
          console.error(`  Cause 2: ${wsErr?.message ?? wsErr}`);
          console.error("Hint: Start the daemon first: xyncra-client listen --user-id <user>");
          process.exit(1);
        }
      }

      console.log(`Conversation deleted. ${result.deleted_message_count} message(s) removed.`);
    });

  // -----------------------------------------------------------------------
  // restore-conversation
  // -----------------------------------------------------------------------
  program
    .command('restore-conversation')
    .description('Restore a previously soft-deleted conversation')
    .requiredOption('-c, --conversation-id <id>', 'Conversation ID')
    .action(async (options: { conversationId: string }, cmd: Command) => {
      const cliCtx = CLIContext.fromCommand(cmd);
      const params = { conversation_id: options.conversationId };

      let result: RestoreConversationResult | undefined;
      let ipcErr: Error | undefined;
      try {
        const client = new IPCClient(cliCtx.getSocketPath());
        const resp = await client.call('restore_conversation', params);
        if (resp.error) {
          ipcErr = new Error(resp.error.message);
        } else {
          result = resp.result as RestoreConversationResult;
        }
      } catch (err) {
        ipcErr = err as Error;
      }

      if (!result) {
        let wsErr: Error | undefined;
        try {
          const wsResult = await standaloneRPC(cliCtx, 'restore_conversation', params);
          result = wsResult as RestoreConversationResult;
        } catch (err) {
          wsErr = err as Error;
        }

        if (!result) {
          console.error('Error: Cannot restore conversation.');
          console.error(`  Cause 1: ${ipcErr?.message ?? ipcErr}`);
          console.error(`  Cause 2: ${wsErr?.message ?? wsErr}`);
          console.error("Hint: Start the daemon first: xyncra-client listen --user-id <user>");
          process.exit(1);
        }
      }

      console.log(`Conversation restored. ${result.restored_message_count} message(s) recovered.`);
    });

  // -----------------------------------------------------------------------
  // list-conversations (IPC-only — reads from daemon's local DB, D-035)
  // -----------------------------------------------------------------------
  program
    .command('list-conversations')
    .description('List conversations from local database')
    .option('--offset <n>', 'Pagination offset', '0')
    .option('--limit <n>', 'Maximum number of conversations to show', '20')
    .action(async (options: { offset: string; limit: string }, cmd: Command) => {
      const cliCtx = CLIContext.fromCommand(cmd);
      const offset = parseInt(options.offset, 10);
      const limit = parseInt(options.limit, 10);

      const client = new IPCClient(cliCtx.getSocketPath());
      try {
        const resp = await client.call('list_conversations', { offset, limit });
        if (resp.error) {
          console.error(`Error: list-conversations: ${resp.error.message}`);
          process.exit(1);
        }
        const result = resp.result as { conversations: Conversation[]; has_more: boolean };
        printConversationList(result.conversations, cliCtx.userID, result.has_more);
      } catch {
        console.error('Error: daemon not running.');
        console.error("Hint: Start with 'xyncra-client listen --user-id <user>'");
        process.exit(2);
      }
    });

  // -----------------------------------------------------------------------
  // get-conversation (IPC-only — reads from daemon's local DB, D-035)
  // -----------------------------------------------------------------------
  program
    .command('get-conversation')
    .description('Show conversation details from local database')
    .requiredOption('-c, --conversation-id <id>', 'Conversation ID')
    .action(async (options: { conversationId: string }, cmd: Command) => {
      const cliCtx = CLIContext.fromCommand(cmd);

      const client = new IPCClient(cliCtx.getSocketPath());
      try {
        const resp = await client.call('get_conversation', {
          conversation_id: options.conversationId,
        });
        if (resp.error) {
          console.error(`Error: get-conversation: ${resp.error.message}`);
          process.exit(1);
        }
        const result = resp.result as { conversation: Conversation; unread_count: number };
        printConversationDetail(result.conversation, cliCtx.userID, result.unread_count);
      } catch {
        console.error('Error: daemon not running.');
        console.error("Hint: Start with 'xyncra-client listen --user-id <user>'");
        process.exit(2);
      }
    });
}

// ---------------------------------------------------------------------------
// Output helpers — match Go format exactly.
// ---------------------------------------------------------------------------

function printCreateConversationResult(result: CreateConversationResult): void {
  if (result.duplicate) {
    console.log('Conversation already exists (find-or-create).');
  } else {
    console.log('Conversation created.');
  }
  if (result.conversation) {
    console.log(`  Conversation ID: ${result.conversation.id}`);
    console.log(`  Peer: ${result.conversation.user_id2}`);
    if (result.conversation.title) {
      console.log(`  Title: ${result.conversation.title}`);
    }
  }
}

function printConversationList(
  convs: Conversation[],
  currentUserID: string,
  hasMore: boolean,
): void {
  if (!convs || convs.length === 0) {
    console.log("No conversations found. Run 'xyncra-client listen' first to sync data.");
    return;
  }

  // tabwriter-style output: ID, PEER, TITLE, LAST MESSAGE.
  const rows: string[][] = [];
  rows.push(['ID', 'PEER', 'TITLE', 'LAST MESSAGE']);
  rows.push(['--', '----', '-----', '------------']);
  for (const conv of convs) {
    const peer = conv.user_id2 === currentUserID ? conv.user_id1 : conv.user_id2;
    const title = conv.title || '-';
    const lastMsg = conv.last_message_at ? formatTime(conv.last_message_at) : '-';
    rows.push([conv.id, peer, title, lastMsg]);
  }
  printTable(rows);

  if (hasMore) {
    console.log('... more conversations available (use --offset to paginate)');
  }
}

function printConversationDetail(
  conv: Conversation,
  currentUserID: string,
  unreadCount: number,
): void {
  const peer = conv.user_id2 === currentUserID ? conv.user_id1 : conv.user_id2;
  console.log('Conversation Details');
  console.log(`  ID:           ${conv.id}`);
  console.log(`  Type:         ${conv.type}`);
  console.log(`  User 1:       ${conv.user_id1}`);
  console.log(`  User 2:       ${conv.user_id2}`);
  console.log(`  Peer:         ${peer}`);
  if (conv.title) {
    console.log(`  Title:        ${conv.title}`);
  }
  console.log(`  Created:      ${formatTime(conv.created_at)}`);
  const lastMsg = conv.last_message_at ? formatTime(conv.last_message_at) : '-';
  console.log(`  Last Message: ${lastMsg}`);
  console.log(`  Unread:       ${unreadCount}`);
}

/** Format an ISO 8601 timestamp to "YYYY-MM-DD HH:mm:ss" (matches Go "2006-01-02 15:04:05"). */
function formatTime(iso: string): string {
  try {
    const d = new Date(iso);
    const pad = (n: number, w = 2): string => String(n).padStart(w, '0');
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
  } catch {
    return iso;
  }
}

/** Print a table with auto-sized columns (tabwriter emulation). */
function printTable(rows: string[][]): void {
  if (rows.length === 0) return;
  const colCount = rows[0].length;
  const widths = new Array(colCount).fill(0);
  for (const row of rows) {
    for (let i = 0; i < colCount; i++) {
      widths[i] = Math.max(widths[i], (row[i] ?? '').length);
    }
  }
  for (const row of rows) {
    const cells: string[] = [];
    for (let i = 0; i < colCount; i++) {
      const cell = row[i] ?? '';
      cells.push(cell.padEnd(widths[i]));
    }
    console.log(cells.join('  '));
  }
}
