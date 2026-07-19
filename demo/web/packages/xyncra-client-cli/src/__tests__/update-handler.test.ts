/**
 * Tests for update-handler.ts — CLIUpdateHandler.
 */

import { CLIUpdateHandler } from '../update-handler';
import type {
  IUpdateHandler,
  ITypingHandler,
  IStreamingHandler,
  IAgentStatusHandler,
  IAgentTimeoutHandler,
  Message,
  Conversation,
} from '@xyncra/client-core';

describe('CLIUpdateHandler', () => {
  let stdoutSpy: jest.SpyInstance;
  let handler: CLIUpdateHandler;

  beforeEach(() => {
    stdoutSpy = jest.spyOn(process.stdout, 'write').mockImplementation(() => true);
    handler = new CLIUpdateHandler();
  });

  afterEach(() => {
    stdoutSpy.mockRestore();
  });

  test('implements IUpdateHandler interface', () => {
    expect(typeof handler.onMessage).toBe('function');
    expect(typeof handler.onDeleteMessage).toBe('function');
    expect(typeof handler.onMarkRead).toBe('function');
    expect(typeof handler.onConversation).toBe('function');
    expect(typeof handler.onGap).toBe('function');
    const iup: IUpdateHandler = handler;
    expect(iup).toBeDefined();
  });

  test('implements ITypingHandler', () => {
    expect(typeof handler.onTyping).toBe('function');
    const ith: ITypingHandler = handler;
    expect(ith).toBeDefined();
  });

  test('implements IStreamingHandler', () => {
    expect(typeof handler.onStreaming).toBe('function');
    const ish: IStreamingHandler = handler;
    expect(ish).toBeDefined();
  });

  test('implements IAgentStatusHandler', () => {
    expect(typeof handler.onAgentStatus).toBe('function');
    const iash: IAgentStatusHandler = handler;
    expect(iash).toBeDefined();
  });

  test('implements IAgentTimeoutHandler', () => {
    expect(typeof handler.onAgentTimeout).toBe('function');
    const iath: IAgentTimeoutHandler = handler;
    expect(iath).toBeDefined();
  });

  test('onMessage writes [new message] to stdout', async () => {
    const msg: Message = {
      id: 'msg-1',
      conversationId: 'conv-1',
      senderId: 'user1',
      content: 'hello world',
      clientMessageId: 'cm-1',
      createdAt: '2026-07-18T10:00:00Z',
    };
    await handler.onMessage(msg);
    expect(stdoutSpy).toHaveBeenCalledTimes(1);
    const line = stdoutSpy.mock.calls[0][0] as string;
    expect(line).toContain('[new message]');
    expect(line).toContain('seq=msg-1');
    expect(line).toContain('from=user1');
    expect(line).toContain('conv=conv-1');
    expect(line).toContain('hello world');
  });

  test('onDeleteMessage writes [delete message] to stdout', async () => {
    await handler.onDeleteMessage('msg-1', 'conv-1');
    expect(stdoutSpy).toHaveBeenCalledTimes(1);
    const line = stdoutSpy.mock.calls[0][0] as string;
    expect(line).toContain('[delete message]');
    expect(line).toContain('conv=conv-1');
    expect(line).toContain('msg=msg-1');
  });

  test('onMarkRead writes [mark read] to stdout', async () => {
    await handler.onMarkRead('conv-1', 'msg-1');
    const line = stdoutSpy.mock.calls[0][0] as string;
    expect(line).toContain('[mark read]');
    expect(line).toContain('conv=conv-1');
    expect(line).toContain('msg_id=msg-1');
  });

  test('onConversation writes [conversation] to stdout', async () => {
    const conv: Conversation = {
      id: 'conv-1',
      userId1: 'user1',
      userId2: 'user2',
      title: 'Test Chat',
      createdAt: '2026-07-18T10:00:00Z',
    };
    await handler.onConversation(conv);
    const line = stdoutSpy.mock.calls[0][0] as string;
    expect(line).toContain('[conversation]');
    expect(line).toContain('id=conv-1');
    expect(line).toContain('Test Chat');
  });

  test('onConversation includes the action in stdout', async () => {
    const conv: Conversation = {
      id: 'conv-2',
      userId1: 'user1',
      userId2: 'user2',
      title: 'Removed Chat',
      createdAt: '2026-07-18T10:00:00Z',
    };
    await handler.onConversation(conv, 'removed');
    const line = stdoutSpy.mock.calls[0][0] as string;
    expect(line).toContain('[conversation] removed');
    expect(line).toContain('id=conv-2');
  });

  test('onGap writes [gap] to stdout', async () => {
    await handler.onGap(42);
    const line = stdoutSpy.mock.calls[0][0] as string;
    expect(line).toContain('[gap]');
    expect(line).toContain('seq=42');
  });

  describe('onTyping', () => {
    test('shows "typing" for human users when typing starts', async () => {
      await handler.onTyping('user1', 'conv-1', true, false);
      const line = stdoutSpy.mock.calls[0][0] as string;
      expect(line).toContain('[typing]');
      expect(line).toContain('user=user1');
      expect(line).toContain('started typing');
    });

    test('shows "thinking" label for agent users when typing starts', async () => {
      await handler.onTyping('agent/claude', 'conv-1', true, true);
      const line = stdoutSpy.mock.calls[0][0] as string;
      expect(line).toContain('[thinking]');
      expect(line).toContain('user=agent/claude');
      // Action is "started typing" (matching Go reference); label is "thinking".
      expect(line).toContain('started typing');
    });

    test('shows "stopped typing" for human users when typing stops', async () => {
      await handler.onTyping('user1', 'conv-1', false, false);
      const line = stdoutSpy.mock.calls[0][0] as string;
      expect(line).toContain('[typing]');
      expect(line).toContain('stopped typing');
    });

    test('shows "stopped thinking" for agent users when typing stops', async () => {
      await handler.onTyping('agent/gpt-4', 'conv-1', false, true);
      const line = stdoutSpy.mock.calls[0][0] as string;
      expect(line).toContain('[thinking]');
      expect(line).toContain('stopped thinking');
    });
  });

  test('onStreaming writes [streaming] for human users', async () => {
    await handler.onStreaming('user1', 'conv-1', 'stream-1', 'hello', false, false);
    const line = stdoutSpy.mock.calls[0][0] as string;
    expect(line).toContain('[streaming]');
    expect(line).toContain('user=user1');
    expect(line).toContain('stream=stream-1');
    expect(line).toContain('status=streaming');
  });

  test('onStreaming writes [agent] for agent users', async () => {
    await handler.onStreaming('agent/claude', 'conv-1', 'stream-1', 'hello', true, true);
    const line = stdoutSpy.mock.calls[0][0] as string;
    expect(line).toContain('[agent]');
    expect(line).toContain('user=agent/claude');
    expect(line).toContain('status=done');
  });

  test('onAgentStatus writes [agent_status]', async () => {
    await handler.onAgentStatus('agent/claude', 'conv-1', 'running');
    const line = stdoutSpy.mock.calls[0][0] as string;
    expect(line).toContain('[agent_status]');
    expect(line).toContain('agent=agent/claude');
    expect(line).toContain('status=running');
  });

  test('onAgentTimeout writes [agent_timeout]', async () => {
    await handler.onAgentTimeout('agent/claude', 'conv-1', 'deadline exceeded');
    const line = stdoutSpy.mock.calls[0][0] as string;
    expect(line).toContain('[agent_timeout]');
    expect(line).toContain('agent=agent/claude');
    expect(line).toContain('deadline exceeded');
  });
});
