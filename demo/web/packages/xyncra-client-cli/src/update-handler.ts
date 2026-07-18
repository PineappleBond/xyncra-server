/**
 * CLI update handler implementing IUpdateHandler and optional handler interfaces.
 *
 * Prints received updates to stdout in the same format as the Go CLI.
 *
 * Mirrors Go cliUpdateHandler in internal/cli/listen.go.
 *
 * @module
 */

import type {
  Conversation,
  IAgentStatusHandler,
  IAgentTimeoutHandler,
  IStreamingHandler,
  ITypingHandler,
  IUpdateHandler,
  Message,
} from '@xyncra/client-core';
import { isAgentUser } from '@xyncra/client-core';

/**
 * CLIUpdateHandler prints received data updates to stdout.
 * Implements IUpdateHandler + all optional handler interfaces.
 */
export class CLIUpdateHandler
  implements
    IUpdateHandler,
    ITypingHandler,
    IStreamingHandler,
    IAgentStatusHandler,
    IAgentTimeoutHandler
{
  async onMessage(message: Message): Promise<void> {
    process.stdout.write(
      `[new message] seq=${message.id} from=${message.senderId} conv=${message.conversationId} "${message.content}"\n`,
    );
  }

  async onDeleteMessage(messageId: string, conversationId: string): Promise<void> {
    process.stdout.write(`[delete message] conv=${conversationId} msg=${messageId}\n`);
  }

  async onMarkRead(conversationId: string, messageId: string): Promise<void> {
    process.stdout.write(`[mark read] conv=${conversationId} msg_id=${messageId}\n`);
  }

  async onConversation(conversation: Conversation): Promise<void> {
    process.stdout.write(
      `[conversation] id=${conversation.id} title="${conversation.title ?? ''}"\n`,
    );
  }

  async onGap(seq: number): Promise<void> {
    process.stdout.write(`[gap] seq=${seq}\n`);
  }

  // -- ITypingHandler --
  async onTyping(
    userId: string,
    conversationId: string,
    isTyping: boolean,
  ): Promise<void> {
    let action = 'started typing';
    let label = 'typing';
    if (!isTyping) action = 'stopped typing';
    if (isAgentUser(userId)) {
      label = 'thinking';
      if (!isTyping) action = 'stopped thinking';
    }
    process.stdout.write(
      `[${label}] user=${userId} conv=${conversationId} ${action}\n`,
    );
  }

  // -- IStreamingHandler --
  async onStreaming(
    userId: string,
    conversationId: string,
    streamId: string,
    text: string,
    isDone: boolean,
  ): Promise<void> {
    const status = isDone ? 'done' : 'streaming';
    const prefix = isAgentUser(userId) ? 'agent' : 'streaming';
    process.stdout.write(
      `[${prefix}] user=${userId} conv=${conversationId} stream=${streamId} status=${status} text="${text}"\n`,
    );
  }

  // -- IAgentStatusHandler --
  async onAgentStatus(
    userId: string,
    conversationId: string,
    status: string,
  ): Promise<void> {
    process.stdout.write(
      `[agent_status] agent=${userId} conv=${conversationId} status=${status}\n`,
    );
  }

  // -- IAgentTimeoutHandler --
  async onAgentTimeout(
    userId: string,
    conversationId: string,
    reason: string,
  ): Promise<void> {
    process.stdout.write(
      `[agent_timeout] agent=${userId} conv=${conversationId} reason="${reason}"\n`,
    );
  }
}
