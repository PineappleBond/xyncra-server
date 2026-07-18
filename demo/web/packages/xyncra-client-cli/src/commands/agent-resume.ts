/**
 * `agent-resume` command — resume a paused agent after HITL interruption.
 *
 * IPC-only (D-036, D-114): the daemon forwards the request to the server via
 * WebSocket; if the daemon is not running there is nothing to forward.
 *
 * Mirrors Go newAgentResumeCommand in internal/cli/agent_resume.go.
 * @module
 */

import type { Command } from 'commander';
import { CLIContext } from '../cli-context.js';
import { IPCClient } from '../ipc.js';

/**
 * Register the `agent-resume` command on the given Commander program.
 *
 * Usage: xyncra-client agent-resume --conversation-id <id> --checkpoint-id <id> --answer <text> --agent-id <id> [--interrupt-id <id>]
 */
export function registerAgentResumeCommand(program: Command): void {
  program
    .command('agent-resume')
    .description('Resume a paused agent after HITL interruption (D-085, IPC-only)')
    .addHelpText(
      'after',
      `
Resume an agent that is waiting for human input after a HITL
(Human-In-The-Loop) interruption. The agent must have been paused by an
ask_user tool call.

This command is IPC-only (D-036, D-114) — it requires the listen daemon
to be running. Start the daemon with 'xyncra-client listen' first.

Typical workflow:
  1. Run 'xyncra-client listen' to receive HITL notifications
  2. Note the checkpoint_id and interrupt_id from the HITL notification
  3. Run 'xyncra-client agent-resume' with the answer`,
    )
    .requiredOption('--conversation-id <id>', 'Conversation ID')
    .requiredOption('--checkpoint-id <id>', 'Checkpoint ID from HITL notification')
    .option('--interrupt-id <id>', 'Interrupt ID from HITL notification (optional)')
    .requiredOption('--answer <text>', "Answer to the agent's question")
    .requiredOption('--agent-id <id>', 'Agent ID to resume (e.g. agent/xxx)')
    .action(
      async (
        options: {
          conversationId: string;
          checkpointId: string;
          interruptId?: string;
          answer: string;
          agentId: string;
        },
        cmd: Command,
      ) => {
        const cliCtx = CLIContext.fromCommand(cmd);
        const client = new IPCClient(cliCtx.getSocketPath());

        try {
          const resp = await client.call('agent_resume', {
            conversation_id: options.conversationId,
            checkpoint_id: options.checkpointId,
            interrupt_id: options.interruptId ?? '',
            answer: options.answer,
            agent_id: options.agentId,
          });
          if (resp.error) {
            console.error(`Error: agent-resume: ${resp.error.message}`);
            process.exit(1);
          }

          console.log('Agent resume queued successfully');
          console.log(`  conversation: ${options.conversationId}`);
          console.log(`  checkpoint:   ${options.checkpointId}`);
        } catch {
          // IPC connection failed — daemon is not running.
          console.error('Error: daemon not running.');
          console.error("Hint: Start with 'xyncra-client listen --user-id <user>'");
          process.exit(2);
        }
      },
    );
}
