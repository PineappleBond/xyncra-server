import { render, screen } from '@testing-library/react';
import React from 'react';
import { ChatWindow } from '../../components/FloatingAssistant/ChatWindow';
import type { XyncraContextValue } from '../../context/XyncraProvider';
import { XyncraContext } from '../../context/XyncraProvider';
import { TypedEventEmitter } from '../../internal/EventEmitter';
import { FunctionRegistry } from '../../internal/FunctionRegistry';

// Mock sub-components
jest.mock('../../components/FloatingAssistant/AgentSelector', () => ({
  AgentSelector: () =>
    React.createElement(
      'div',
      { 'data-testid': 'agent-selector' },
      'AgentSelector',
    ),
}));

jest.mock('../../components/FloatingAssistant/ConversationList', () => ({
  ConversationList: () =>
    React.createElement(
      'div',
      { 'data-testid': 'conv-list' },
      'ConversationList',
    ),
}));

jest.mock('../../components/FloatingAssistant/MessageArea', () => ({
  MessageArea: () =>
    React.createElement(
      'div',
      { 'data-testid': 'message-area' },
      'MessageArea',
    ),
}));

jest.mock('../../components/FloatingAssistant/HITLDialog', () => ({
  HITLDialog: () =>
    React.createElement('div', { 'data-testid': 'hitl-dialog' }, 'HITLDialog'),
}));

jest.mock('../../components/FloatingAssistant/styles', () => ({
  FLOATING_ASSISTANT_STYLES: { chatWindow: {} },
}));

jest.mock('@ant-design/x', () => ({
  XProvider: ({ children }: any) => React.createElement('div', null, children),
}));

describe('ChatWindow', () => {
  function renderChat() {
    const onClose = jest.fn();
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
        React.createElement(ChatWindow, { onClose }),
      ),
    );
    return { ...element, onClose };
  }

  it('should render all sub-components', () => {
    renderChat();
    expect(screen.getByTestId('agent-selector')).toBeTruthy();
    expect(screen.getByTestId('conv-list')).toBeTruthy();
    expect(screen.getByTestId('message-area')).toBeTruthy();
    expect(screen.getByTestId('hitl-dialog')).toBeTruthy();
  });
});
