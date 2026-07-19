/**
 * Test utilities for @xyncra/client-web tests.
 *
 * Provides mock context providers, factory functions, and helpers.
 */
import React from 'react';
import type {
  ConnectionStatus,
  XyncraContextValue,
} from '../context/XyncraProvider';
import { XyncraContext } from '../context/XyncraProvider';
import type {
  ConversationEvent,
  MessageEvent,
  UpdateHandlerEventMap,
} from '../internal/EventEmitter';
import { TypedEventEmitter } from '../internal/EventEmitter';
import { FunctionRegistry } from '../internal/FunctionRegistry';

// ---------------------------------------------------------------------------
// Mock types
// ---------------------------------------------------------------------------

export interface MockXyncraContext {
  client: {
    listConversations: jest.Mock;
    getMessages: jest.Mock;
    sendMessage: jest.Mock;
    createConversation: jest.Mock;
    deleteConversation: jest.Mock;
    call: jest.Mock;
    start: jest.Mock;
    stop: jest.Mock;
    registerRequestHandler: jest.Mock;
  };
  eventEmitter: TypedEventEmitter<UpdateHandlerEventMap>;
  functionRegistry: FunctionRegistry;
  connectionStatus: ConnectionStatus;
  deviceID: string;
  agentID: string;
  registerFunction: jest.Mock;
  unregisterFunction: jest.Mock;
}

// ---------------------------------------------------------------------------
// Factory: create mock XyncraContext
// ---------------------------------------------------------------------------

export function createMockContext(
  overrides: Partial<MockXyncraContext> = {},
): MockXyncraContext {
  const eventEmitter =
    overrides.eventEmitter ?? new TypedEventEmitter<UpdateHandlerEventMap>();
  const functionRegistry = overrides.functionRegistry ?? new FunctionRegistry();

  return {
    client: {
      listConversations: jest.fn().mockResolvedValue({ conversations: [] }),
      getMessages: jest.fn().mockResolvedValue({ messages: [] }),
      sendMessage: jest.fn().mockResolvedValue({}),
      createConversation: jest.fn().mockResolvedValue({
        conversation: createMockDBConversation(),
      }),
      deleteConversation: jest.fn().mockResolvedValue(undefined),
      call: jest.fn().mockResolvedValue(undefined),
      start: jest.fn().mockResolvedValue(undefined),
      stop: jest.fn(),
      registerRequestHandler: jest.fn(),
    },
    eventEmitter,
    functionRegistry,
    connectionStatus: 'connected',
    deviceID: 'test-device-id',
    agentID: 'test-agent',
    registerFunction: jest.fn(),
    unregisterFunction: jest.fn(),
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Wrapper: render with mock XyncraContext
// ---------------------------------------------------------------------------

export function renderWithXyncraContext<_T>(
  ui: React.ReactElement,
  contextOverrides: Partial<MockXyncraContext> = {},
): { element: React.ReactElement; mockContext: MockXyncraContext } {
  const mockContext = createMockContext(contextOverrides);
  const element = React.createElement(
    XyncraContext.Provider,
    { value: mockContext as unknown as XyncraContextValue },
    ui,
  );
  return { element, mockContext };
}

// ---------------------------------------------------------------------------
// Factory: ConversationEvent
// ---------------------------------------------------------------------------

let convCounter = 0;

export function createMockConversationEvent(
  overrides: Partial<ConversationEvent> = {},
): ConversationEvent {
  convCounter++;
  return {
    id: `conv-${convCounter}`,
    userId1: 'user1',
    userId2: 'user2',
    title: `Test Conversation ${convCounter}`,
    createdAt: new Date().toISOString(),
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Factory: MessageEvent
// ---------------------------------------------------------------------------

let msgCounter = 0;

export function createMockMessageEvent(
  overrides: Partial<MessageEvent> = {},
): MessageEvent {
  msgCounter++;
  return {
    id: `msg-${msgCounter}`,
    conversationId: overrides.conversationId ?? 'conv-1',
    senderId: 'user1',
    content: `Test message ${msgCounter}`,
    clientMessageId: `client-msg-${msgCounter}`,
    createdAt: new Date().toISOString(),
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Factory: DBConversation (snake_case, for useConversations tests)
// ---------------------------------------------------------------------------

export function createMockDBConversation(
  overrides: Record<string, unknown> = {},
) {
  convCounter++;
  const now = new Date();
  return {
    id: `conv-${convCounter}`,
    user_id1: 'user1',
    user_id2: 'user2',
    type: '1-on-1',
    title: `Test Conversation ${convCounter}`,
    pinned: false,
    muted: false,
    avatar_url: '',
    description: '',
    last_processed_message_id: 0,
    created_at: now,
    updated_at: now,
    last_message_at: now,
    last_read_message_id1: 0,
    last_read_message_id2: 0,
    agent_status: 'idle',
    agent_id: '',
    checkpoint_id: '',
    agent_last_activity: now,
    deleted_at: null,
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Factory: DBMessage (snake_case, for useMessages tests)
// ---------------------------------------------------------------------------

export function createMockDBMessage(overrides: Record<string, unknown> = {}) {
  msgCounter++;
  const now = new Date();
  return {
    id: `msg-${msgCounter}`,
    client_message_id: `client-msg-${msgCounter}`,
    conversation_id: 'conv-1',
    message_id: msgCounter,
    sender_id: 'user1',
    content: `Test message ${msgCounter}`,
    type: 'text',
    reply_to: 0,
    status: 'sent',
    created_at: now,
    deleted_at: null,
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Reset counters between tests
// ---------------------------------------------------------------------------

export function resetTestCounters(): void {
  convCounter = 0;
  msgCounter = 0;
}
