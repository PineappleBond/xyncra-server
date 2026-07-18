/**
 * Root CLI command — wires up global flags and subcommands.
 *
 * Mirrors Go NewRootCommand in internal/cli/app.go.
 * @module
 */

import { Command } from 'commander';
import { registerGlobalFlags } from '../cli-context.js';

import { registerListenCommand } from './listen.js';
import { registerSendCommand } from './send.js';
import { registerConversationCommands } from './conversations.js';
import { registerMessageCommands } from './messages.js';
import { registerDraftCommand } from './draft.js';
import { registerSyncCommand } from './sync.js';
import { registerSetTypingCommand } from './set-typing.js';
import { registerStreamTextCommand } from './stream-text.js';
import { registerLogsCommand } from './logs.js';
import { registerKillCommand } from './kill.js';
import { registerAgentResumeCommand } from './agent-resume.js';
import { registerReloadAgentsCommand } from './reload-agents.js';

/**
 * Create the root Commander program with global flags and all subcommands.
 *
 * Global flags (D-034): --user-id, --device-id, --server, --db-path, --log-dir.
 * Each subcommand is registered via its own register* function from the
 * commands/ directory.
 */
export function createRootCommand(): Command {
  const program = new Command();

  program
    .name('xyncra-client')
    .description('Node.js CLI runtime for Xyncra — daemon, IPC, and CLI commands')
    .version('0.1.0');

  registerGlobalFlags(program);

  // Subcommands — defined in their respective files.
  registerListenCommand(program);
  registerSendCommand(program);
  registerConversationCommands(program);
  registerMessageCommands(program);
  registerDraftCommand(program);
  registerSyncCommand(program);
  registerSetTypingCommand(program);
  registerStreamTextCommand(program);
  registerLogsCommand(program);
  registerKillCommand(program);
  registerAgentResumeCommand(program);
  registerReloadAgentsCommand(program);

  return program;
}
