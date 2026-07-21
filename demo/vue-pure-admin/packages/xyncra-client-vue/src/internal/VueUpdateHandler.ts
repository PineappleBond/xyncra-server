import type {
  Conversation,
  ConversationAction,
  IAgentStatusHandler,
  IAgentTimeoutHandler,
  IFunctionCallHandler,
  IStreamingHandler,
  ITypingHandler,
  IUpdateHandler,
  Message,
} from '@xyncra/client-core'
import {
  type ConversationEvent,
  type MessageEvent,
  TypedEventEmitter,
  type UpdateHandlerEventMap,
} from './EventEmitter'

export class VueUpdateHandler
  implements
    IUpdateHandler,
    ITypingHandler,
    IStreamingHandler,
    IAgentStatusHandler,
    IAgentTimeoutHandler,
    IFunctionCallHandler
{
  public readonly emitter: TypedEventEmitter<UpdateHandlerEventMap>

  constructor(emitter?: TypedEventEmitter<UpdateHandlerEventMap>) {
    this.emitter = emitter ?? new TypedEventEmitter<UpdateHandlerEventMap>()
  }

  async onMessage(message: Message): Promise<void> {
    this.emitter.emit('message:added', {
      message: toMessageEvent(message),
    })
  }

  async onDeleteMessage(messageId: string, conversationId: string): Promise<void> {
    this.emitter.emit('message:removed', { messageId, conversationId })
  }

  async onMarkRead(conversationId: string, messageId: string): Promise<void> {
    this.emitter.emit('read:updated', {
      conversationId,
      lastReadMessageId: messageId,
    })
  }

  async onConversation(conversation: Conversation, action: ConversationAction = 'updated'): Promise<void> {
    switch (action) {
      case 'created':
        this.emitter.emit('conversation:added', {
          conversation: toConversationEvent(conversation),
        })
        break
      case 'removed':
        this.emitter.emit('conversation:removed', { conversationId: conversation.id })
        break
      case 'updated':
      default:
        this.emitter.emit('conversation:updated', {
          conversation: toConversationEvent(conversation),
        })
        if (conversation.agentStatus === 'asking_user' && conversation.questions && conversation.questions.length > 0) {
          const question = conversation.questions[0]
          this.emitter.emit('hitl:question', {
            userId: conversation.userId2,
            conversationId: conversation.id,
            reason: question.question_text,
            questionId: question.id,
            checkpointId: question.checkpoint_id,
            interruptId: question.interrupt_id,
          })
        }
        break
    }
  }

  async onGap(_seq: number): Promise<void> {}

  async onTyping(userId: string, conversationId: string, isTyping: boolean, isAgent: boolean): Promise<void> {
    this.emitter.emit('agent:thinking', { userId, conversationId, isTyping, isAgent })
  }

  async onStreaming(userId: string, conversationId: string, streamId: string, text: string, isDone: boolean, isAgent: boolean): Promise<void> {
    if (isDone) {
      this.emitter.emit('stream:done', { userId, conversationId, streamId, text })
    } else {
      this.emitter.emit('stream:text', { userId, conversationId, streamId, text })
    }
  }

  async onAgentStatus(userId: string, conversationId: string, status: string): Promise<void> {
    this.emitter.emit('agent:status', { userId, conversationId, status })
  }

  async onAgentTimeout(userId: string, conversationId: string, reason: string): Promise<void> {
    this.emitter.emit('hitl:question', { userId, conversationId, reason })
  }

  async onFunctionCall(
    userId: string,
    conversationId: string,
    name: string,
    args: string,
    result: string,
    error: string,
    durationMs: number,
    isDone: boolean,
  ): Promise<void> {
    this.emitter.emit('function:called', {
      userId,
      conversationId,
      name,
      args,
      result,
      error,
      durationMs,
      isDone,
    })
  }
}

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
    // Preserve agent status fields
    agentStatus: conv.agentStatus,
    agentId: conv.agentId,
  }
}

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
  }
}
