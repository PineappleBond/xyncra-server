import { render, screen } from '@testing-library/react';
import React from 'react';
const mockReact = React;
import { ConversationList } from '../../components/FloatingAssistant/ConversationList';
import type { XyncraContextValue } from '../../context/XyncraProvider';
import { XyncraContext } from '../../context/XyncraProvider';
import { TypedEventEmitter } from '../../internal/EventEmitter';
import { FunctionRegistry } from '../../internal/FunctionRegistry';

jest.mock('../../hooks/useConversations', () => ({
  useConversations: () => ({
    conversations: [],
    loading: false,
    error: null,
    deleteConversation: jest.fn(),
    refresh: jest.fn(),
  }),
}));

jest.mock('antd', () => ({
  Empty: ({ description }: any) =>
    mockReact.createElement('div', { 'data-testid': 'empty' }, description),
}));

jest.mock(
  '@ant-design/x',
  () => ({
    Conversations: () => mockReact.createElement('div', null, 'conversations'),
  }),
  { virtual: true },
);

describe('ConversationList', () => {
  function renderList() {
    const onSelect = jest.fn();
    const contextValue: XyncraContextValue = {
      client: {} as any,
      connectionStatus: 'connected',
      deviceID: 'test-device',
      agentID: 'test-agent',
      functionRegistry: new FunctionRegistry(),
      eventEmitter: new TypedEventEmitter(),
      registerFunction: jest.fn(),
      unregisterFunction: jest.fn(),
    };
    const element = render(
      React.createElement(
        XyncraContext.Provider,
        { value: contextValue },
         React.createElement(ConversationList, {
          activeConversationID: null,
          onSelect,
        }),
      ),
    );
    return { ...element, onSelect };
  }

  it('should show empty state when no conversations', () => {
    renderList();
    expect(screen.getByText('暂无会话')).toBeTruthy();
  });
});
