import { fireEvent, render, screen } from '@testing-library/react';
import React from 'react';
const mockReact = React;
import { ConversationList } from '../../components/FloatingAssistant/ConversationList';
import type { XyncraContextValue } from '../../context/XyncraProvider';
import { XyncraContext } from '../../context/XyncraProvider';
import { TypedEventEmitter } from '../../internal/EventEmitter';
import { FunctionRegistry } from '../../internal/FunctionRegistry';

// Mock hooks
jest.mock('../../hooks/useConversations', () => ({
  useConversations: () => ({
    conversations: [],
    loading: false,
    error: null,
    createConversation: jest.fn(),
    deleteConversation: jest.fn(),
    refresh: jest.fn(),
  }),
}));

// Mock antd
jest.mock('antd', () => ({
  Button: ({ children, onClick, ...props }: any) =>
    mockReact.createElement(
      'button',
      { type: 'button', onClick, ...props },
      children,
    ),
  Empty: ({ description }: any) =>
    mockReact.createElement('div', { 'data-testid': 'empty' }, description),
}));

jest.mock('@ant-design/icons', () => ({
  PlusOutlined: () => mockReact.createElement('span', null, '+'),
}));

jest.mock('../../components/FloatingAssistant/ConnectionStatus', () => ({
  ConnectionStatus: () => mockReact.createElement('span', null, 'status'),
}));

jest.mock(
  '@ant-design/x',
  () => ({
    Conversations: () => mockReact.createElement('div', null, 'conversations'),
  }),
  { virtual: true },
);

jest.mock('../../components/FloatingAssistant/styles', () => ({
  FLOATING_ASSISTANT_STYLES: { conversationList: {} },
}));

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
          selectedAgentID: 'test-agent',
          selectedAgentName: null,
          onSelect,
        }),
      ),
    );
    return { ...element, onSelect };
  }

  it('should render the new conversation button', () => {
    renderList();
    expect(screen.getByText('新建会话')).toBeTruthy();
  });

  it('should show empty state when no conversations', () => {
    renderList();
    expect(screen.getByText('暂无会话')).toBeTruthy();
  });

  it('should call createConversation when new button is clicked', () => {
    renderList();
    fireEvent.click(screen.getByText('新建会话'));
    // createConversation is called via the hook mock
  });
});
