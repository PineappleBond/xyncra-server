import type { Conversation, Message } from '@xyncra/client-core';
import type { UpdateHandlerEventMap } from '../../internal/EventEmitter';
import { TypedEventEmitter } from '../../internal/EventEmitter';
import { ReactUpdateHandler } from '../../internal/ReactUpdateHandler';

describe('ReactUpdateHandler', () => {
  let handler: ReactUpdateHandler;
  let emitter: TypedEventEmitter<UpdateHandlerEventMap>;

  beforeEach(() => {
    emitter = new TypedEventEmitter<UpdateHandlerEventMap>();
    handler = new ReactUpdateHandler(emitter);
  });

  describe('onMessage', () => {
    it('should emit message:added event', async () => {
      const callback = jest.fn();
      emitter.on('message:added', callback);

      const msg: Message = {
        id: 'msg-1',
        conversationId: 'conv-1',
        senderId: 'user1',
        content: 'Hello',
        clientMessageId: 'client-1',
        createdAt: '2026-01-01T00:00:00Z',
      };

      await handler.onMessage(msg);

      expect(callback).toHaveBeenCalledTimes(1);
      expect(callback).toHaveBeenCalledWith({
        message: expect.objectContaining({
          id: 'msg-1',
          conversationId: 'conv-1',
          senderId: 'user1',
          content: 'Hello',
          clientMessageId: 'client-1',
        }),
      });
    });

    it('should preserve optional fields', async () => {
      const callback = jest.fn();
      emitter.on('message:added', callback);

      const msg: Message = {
        id: 'msg-1',
        conversationId: 'conv-1',
        senderId: 'user1',
        content: 'Reply',
        clientMessageId: 'client-1',
        replyToId: 'msg-0',
        createdAt: '2026-01-01T00:00:00Z',
        updatedAt: '2026-01-01T01:00:00Z',
      };

      await handler.onMessage(msg);
      const emitted = callback.mock.calls[0][0].message;
      expect(emitted.replyToId).toBe('msg-0');
      expect(emitted.updatedAt).toBe('2026-01-01T01:00:00Z');
    });
  });

  describe('onDeleteMessage', () => {
    it('should emit message:removed event', async () => {
      const callback = jest.fn();
      emitter.on('message:removed', callback);

      await handler.onDeleteMessage('msg-1', 'conv-1');

      expect(callback).toHaveBeenCalledWith({
        messageId: 'msg-1',
        conversationId: 'conv-1',
      });
    });
  });

  describe('onConversation', () => {
    const conv: Conversation = {
      id: 'conv-1',
      userId1: 'user1',
      userId2: 'user2',
      title: 'Test',
      createdAt: '2026-01-01T00:00:00Z',
    };

    it('should emit conversation:added event for created action', async () => {
      const callback = jest.fn();
      emitter.on('conversation:added', callback);

      await handler.onConversation(conv, 'created');

      expect(callback).toHaveBeenCalledTimes(1);
      expect(callback).toHaveBeenCalledWith({
        conversation: expect.objectContaining({
          id: 'conv-1',
          userId1: 'user1',
          userId2: 'user2',
          title: 'Test',
        }),
      });
    });

    it('should emit conversation:updated event for updated action (default)', async () => {
      const callback = jest.fn();
      emitter.on('conversation:updated', callback);

      await handler.onConversation(conv);
      await handler.onConversation(conv, 'updated');

      expect(callback).toHaveBeenCalledTimes(2);
      expect(callback).toHaveBeenCalledWith({
        conversation: expect.objectContaining({ id: 'conv-1' }),
      });
    });

    it('should emit conversation:removed event for removed action', async () => {
      const callback = jest.fn();
      emitter.on('conversation:removed', callback);

      await handler.onConversation(conv, 'removed');

      expect(callback).toHaveBeenCalledTimes(1);
      expect(callback).toHaveBeenCalledWith({ conversationId: 'conv-1' });
    });
  });

  describe('onMarkRead', () => {
    it('should emit read:updated event', async () => {
      const callback = jest.fn();
      emitter.on('read:updated', callback);

      await handler.onMarkRead('conv-1', 'msg-5');

      expect(callback).toHaveBeenCalledWith({
        conversationId: 'conv-1',
        lastReadMessageId: 'msg-5',
      });
    });
  });

  describe('onGap', () => {
    it('should not throw', async () => {
      await expect(handler.onGap(42)).resolves.not.toThrow();
    });
  });

  describe('onTyping', () => {
    it('should emit agent:thinking event', async () => {
      const callback = jest.fn();
      emitter.on('agent:thinking', callback);

      await handler.onTyping('agent1', 'conv-1', true);

      expect(callback).toHaveBeenCalledWith({
        userId: 'agent1',
        conversationId: 'conv-1',
        isTyping: true,
      });
    });

    it('should handle typing stopped', async () => {
      const callback = jest.fn();
      emitter.on('agent:thinking', callback);

      await handler.onTyping('agent1', 'conv-1', false);

      expect(callback).toHaveBeenCalledWith({
        userId: 'agent1',
        conversationId: 'conv-1',
        isTyping: false,
      });
    });
  });

  describe('onStreaming', () => {
    it('should emit stream:text when not done', async () => {
      const callback = jest.fn();
      emitter.on('stream:text', callback);

      await handler.onStreaming('agent1', 'conv-1', 'stream-1', 'Hello', false);

      expect(callback).toHaveBeenCalledWith({
        userId: 'agent1',
        conversationId: 'conv-1',
        streamId: 'stream-1',
        text: 'Hello',
      });
    });

    it('should emit stream:done when done', async () => {
      const callback = jest.fn();
      emitter.on('stream:done', callback);

      await handler.onStreaming('agent1', 'conv-1', 'stream-1', '', true);

      expect(callback).toHaveBeenCalledWith({
        userId: 'agent1',
        conversationId: 'conv-1',
        streamId: 'stream-1',
      });
    });
  });

  describe('onAgentStatus', () => {
    it('should emit agent:status event', async () => {
      const callback = jest.fn();
      emitter.on('agent:status', callback);

      await handler.onAgentStatus('agent1', 'conv-1', 'thinking');

      expect(callback).toHaveBeenCalledWith({
        userId: 'agent1',
        conversationId: 'conv-1',
        status: 'thinking',
      });
    });
  });

  describe('onAgentTimeout', () => {
    it('should emit hitl:question event', async () => {
      const callback = jest.fn();
      emitter.on('hitl:question', callback);

      await handler.onAgentTimeout('agent1', 'conv-1', 'Need user input');

      expect(callback).toHaveBeenCalledWith({
        userId: 'agent1',
        conversationId: 'conv-1',
        reason: 'Need user input',
        questionId: undefined,
        checkpointId: undefined,
        interruptId: undefined,
      });
    });
  });

  describe('constructor', () => {
    it('should create its own emitter if none provided', () => {
      const standalone = new ReactUpdateHandler();
      expect(standalone.emitter).toBeInstanceOf(TypedEventEmitter);
    });
  });
});
