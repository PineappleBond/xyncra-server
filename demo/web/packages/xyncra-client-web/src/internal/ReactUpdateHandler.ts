/**
 * @packageDocumentation
 * ReactUpdateHandler — bridges IUpdateHandler callbacks to typed events.
 *
 * Design decision D-1: ReactUpdateHandler does not hold React state. Instead
 * it emits events through a TypedEventEmitter. Each React hook subscribes
 * in useEffect. This decouples the long-lived IUpdateHandler from React's
 * render cycle and avoids stale closure issues.
 *
 * Implements IUpdateHandler + all optional handler interfaces from
 * @xyncra/client-core.
 *
 * @module
 */

import type {
  Conversation,
  ConversationAction,
  IAgentStatusHandler,
  IAgentTimeoutHandler,
  IStreamingHandler,
  ITypingHandler,
  IUpdateHandler,
  Message,
} from '@xyncra/client-core';
import {
  type ConversationEvent,
  type MessageEvent,
  TypedEventEmitter,
  type UpdateHandlerEventMap,
} from './EventEmitter';

/**
 * ReactUpdateHandler receives data updates from the core sync pipeline
 * and re-emits them as typed events via a TypedEventEmitter.
 *
 * React hooks subscribe to the emitter in useEffect to drive UI updates.
 */
export class ReactUpdateHandler
  implements
    IUpdateHandler,
    ITypingHandler,
    IStreamingHandler,
    IAgentStatusHandler,
    IAgentTimeoutHandler
{
  /** The typed event emitter that React hooks subscribe to. */
  public readonly emitter: TypedEventEmitter<UpdateHandlerEventMap>;

  constructor(emitter?: TypedEventEmitter<UpdateHandlerEventMap>) {
    this.emitter = emitter ?? new TypedEventEmitter<UpdateHandlerEventMap>();
  }

  // -- IUpdateHandler --

  async onMessage(message: Message): Promise<void> {
    this.emitter.emit('message:added', {
      message: toMessageEvent(message),
    });
  }

  async onDeleteMessage(
    messageId: string,
    conversationId: string,
  ): Promise<void> {
    this.emitter.emit('message:removed', { messageId, conversationId });
  }

  async onMarkRead(conversationId: string, messageId: string): Promise<void> {
    this.emitter.emit('read:updated', {
      conversationId,
      lastReadMessageId: messageId,
    });
  }

  async onConversation(
    conversation: Conversation,
    action: ConversationAction = 'updated',
  ): Promise<void> {
    switch (action) {
      case 'created':
        this.emitter.emit('conversation:added', {
          conversation: toConversationEvent(conversation),
        });
        break;
      case 'removed':
        this.emitter.emit('conversation:removed', { conversationId: conversation.id });
        break;
      case 'updated':
      default:
        this.emitter.emit('conversation:updated', {
          conversation: toConversationEvent(conversation),
        });
        break;
    }
  }

  async onGap(_seq: number): Promise<void> {
    // Gap events are handled by the core sync pipeline. The UI does not
    // need to react to them directly.
  }

  // -- ITypingHandler --

  async onTyping(
    userId: string,
    conversationId: string,
    isTyping: boolean,
    isAgent: boolean,
  ): Promise<void> {
    this.emitter.emit('agent:thinking', {
      userId,
      conversationId,
      isTyping,
      isAgent,
    });
  }

  // -- IStreamingHandler --

  async onStreaming(
    userId: string,
    conversationId: string,
    streamId: string,
    text: string,
    isDone: boolean,
    isAgent: boolean,
  ): Promise<void> {
    if (isDone) {
      this.emitter.emit('stream:done', { userId, conversationId, streamId });
    } else {
      this.emitter.emit('stream:text', {
        userId,
        conversationId,
        streamId,
        text,
      });
    }
  }

  // -- IAgentStatusHandler --

  async onAgentStatus(
    userId: string,
    conversationId: string,
    status: string,
  ): Promise<void> {
    this.emitter.emit('agent:status', { userId, conversationId, status });
  }

  // -- IAgentTimeoutHandler --

  async onAgentTimeout(
    userId: string,
    conversationId: string,
    reason: string,
  ): Promise<void> {
    this.emitter.emit('hitl:question', { userId, conversationId, reason });
  }
}

// ---------------------------------------------------------------------------
// Conversion helpers — core camelCase models → event payloads
// ---------------------------------------------------------------------------

/**
 * Convert a core Conversation (camelCase) to a ConversationEvent.
 */
function toConversationEvent(conv: Conversation): ConversationEvent {
  return {
    id: conv.id,
    userId1: conv.userId1,
    userId2: conv.userId2,
    title: conv.title,
    lastMessageId: conv.lastMessageId,
    lastMessageAt: conv.lastMessageAt,
    lastReadMessageId1: conv.lastReadMessageId1,
    lastReadMessageId2: conv.lastReadMessageId2,
    createdAt: conv.createdAt,
    updatedAt: conv.updatedAt,
    deletedAt: conv.deletedAt,
  };
}

/**
 * Convert a core Message (camelCase) to a MessageEvent.
 */
function toMessageEvent(msg: Message): MessageEvent {
  return {
    id: msg.id,
    conversationId: msg.conversationId,
    senderId: msg.senderId,
    content: msg.content,
    clientMessageId: msg.clientMessageId,
    replyToId: msg.replyToId,
    createdAt: msg.createdAt,
    updatedAt: msg.updatedAt,
    deletedAt: msg.deletedAt,
  };
}

