/**
 * `draft` command — manage message drafts (local only).
 *
 * All operations go through IPC to the daemon (which owns the DB).
 * Mirrors Go newDraftCommand in internal/cli/draft.go.
 * @module
 */

import type { Command } from 'commander';
import { CLIContext } from '../cli-context.js';
import { IPCClient } from '../ipc.js';

/**
 * Register the `draft` parent command with save/get/delete subcommands.
 */
export function registerDraftCommand(program: Command): void {
  const draft = program
    .command('draft')
    .description('Manage message drafts (local only)');

  // -----------------------------------------------------------------------
  // draft save
  // -----------------------------------------------------------------------
  draft
    .command('save')
    .description('Save or update a draft for a conversation')
    .requiredOption('-c, --conversation-id <id>', 'Conversation ID')
    .requiredOption('-m, --content <text>', 'Draft content')
    .action(async (options: { conversationId: string; content: string }, cmd: Command) => {
      const cliCtx = CLIContext.fromCommand(cmd);
      const client = new IPCClient(cliCtx.getSocketPath());

      try {
        const resp = await client.call('draft_save', {
          conversation_id: options.conversationId,
          content: options.content,
        });
        if (resp.error) {
          console.error(`Error: draft save: ${resp.error.message}`);
          process.exit(1);
        }
        console.log('Draft saved.');
      } catch {
        console.error('Error: daemon not running.');
        console.error("Hint: Start with 'xyncra-client listen --user-id <user>'");
        process.exit(2);
      }
    });

  // -----------------------------------------------------------------------
  // draft get
  // -----------------------------------------------------------------------
  draft
    .command('get')
    .description('Retrieve the draft for a conversation')
    .requiredOption('-c, --conversation-id <id>', 'Conversation ID')
    .action(async (options: { conversationId: string }, cmd: Command) => {
      const cliCtx = CLIContext.fromCommand(cmd);
      const client = new IPCClient(cliCtx.getSocketPath());

      try {
        const resp = await client.call('draft_get', {
          conversation_id: options.conversationId,
        });
        if (resp.error) {
          console.error(`Error: draft get: ${resp.error.message}`);
          process.exit(1);
        }
        const result = resp.result as { content?: string } | null;
        if (!result || !result.content) {
          console.log('No draft found for this conversation.');
          return;
        }
        console.log(`Draft for conversation ${options.conversationId}:`);
        console.log(result.content);
      } catch {
        console.error('Error: daemon not running.');
        console.error("Hint: Start with 'xyncra-client listen --user-id <user>'");
        process.exit(2);
      }
    });

  // -----------------------------------------------------------------------
  // draft delete
  // -----------------------------------------------------------------------
  draft
    .command('delete')
    .description('Delete the draft for a conversation')
    .requiredOption('-c, --conversation-id <id>', 'Conversation ID')
    .action(async (options: { conversationId: string }, cmd: Command) => {
      const cliCtx = CLIContext.fromCommand(cmd);
      const client = new IPCClient(cliCtx.getSocketPath());

      try {
        const resp = await client.call('draft_delete', {
          conversation_id: options.conversationId,
        });
        if (resp.error) {
          // Go distinguishes ErrNotFound ("No draft found") from other errors,
          // but both are non-fatal in spirit. Mirror Go's output.
          console.log('No draft found for this conversation.');
          return;
        }
        console.log('Draft deleted.');
      } catch {
        console.error('Error: daemon not running.');
        console.error("Hint: Start with 'xyncra-client listen --user-id <user>'");
        process.exit(2);
      }
    });
}
