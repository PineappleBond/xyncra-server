import { fireEvent, render, screen } from '@testing-library/react';
import React from 'react';
const mockReact = React;
import { MessageArea } from '../../components/FloatingAssistant/MessageArea';
import type { XyncraContextValue } from '../../context/XyncraProvider';
import { XyncraContext } from '../../context/XyncraProvider';
import { TypedEventEmitter } from '../../internal/EventEmitter';
import { FunctionRegistry } from '../../internal/FunctionRegistry';

const mockSend = jest.fn();
jest.mock('../../hooks/useMessages', () => ({
  useMessages: () => ({
    messages: [],
    loading: false,
    error: null,
    send: mockSend,
    refresh: jest.fn(),
  }),
}));

jest.mock('../../hooks/useStreaming', () => ({
  useStreaming: () => ({
    streamingText: '',
    isStreaming: false,
    currentStreamID: null,
  }),
}));

jest.mock('../../hooks/useAgentStatus', () => ({
  useAgentStatus: () => ({
    status: null,
    isTyping: false,
  }),
}));

jest.mock('antd', () => ({
  Empty: ({ description }: any) =>
    mockReact.createElement('div', { 'data-testid': 'empty' }, description),
}));

jest.mock(
  '@ant-design/x',
  () => ({
    Bubble: {
      List: ({ items }: any) =>
        mockReact.createElement(
          'div',
          { 'data-testid': 'bubble-list' },
          `${items.length} items`,
        ),
    },
    Sender: ({ onSubmit, placeholder, disabled }: any) =>
      mockReact.createElement('div', { 'data-testid': 'sender' }, [
        mockReact.createElement('input', {
          key: 'input',
          'data-testid': 'sender-input',
          placeholder,
          disabled,
        }),
        mockReact.createElement(
          'button',
          {
            type: 'button',
            key: 'btn',
            'data-testid': 'sender-btn',
            onClick: () => onSubmit?.('test message'),
          },
          'Send',
        ),
      ]),
  }),
  { virtual: true },
);

jest.mock('@ant-design/x-markdown', () => ({
  XMarkdown: ({ content }: any) => mockReact.createElement('span', null, content),
}), { virtual: true });

jest.mock('../../components/FloatingAssistant/styles', () => ({
  FLOATING_ASSISTANT_STYLES: { messageArea: {}, messageList: {}, emptyState: {}, senderArea: {} },
}));

describe('MessageArea', () => {
  beforeEach(() => {
    mockSend.mockClear();
  });

  function renderArea(conversationID: string | null = 'conv-1') {
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
    return render(
      React.createElement(
        XyncraContext.Provider,
        { value: contextValue },
        React.createElement(MessageArea, { conversationID }),
      ),
    );
  }

  it('should show empty state when no conversation selected', () => {
    renderArea(null);
    expect(screen.getByText('选择一个会话开始对话')).toBeTruthy();
  });

  it('should show empty state when conversation has no messages', () => {
    renderArea('conv-1');
    expect(screen.getByText('发送消息开始对话')).toBeTruthy();
  });

  it('should render sender input', () => {
    renderArea('conv-1');
    expect(screen.getByTestId('sender-input')).toBeTruthy();
  });

  it('should call send when message is submitted', () => {
    renderArea('conv-1');
    fireEvent.click(screen.getByTestId('sender-btn'));
    expect(mockSend).toHaveBeenCalledWith('test message');
  });
});
