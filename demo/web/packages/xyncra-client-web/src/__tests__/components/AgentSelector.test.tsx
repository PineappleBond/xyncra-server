import { render, screen } from '@testing-library/react';
import React from 'react';
import { AgentSelector } from '../../components/FloatingAssistant/AgentSelector';
import type { XyncraContextValue } from '../../context/XyncraProvider';
import { XyncraContext } from '../../context/XyncraProvider';
import { TypedEventEmitter } from '../../internal/EventEmitter';
import { FunctionRegistry } from '../../internal/FunctionRegistry';

jest.mock('antd', () => ({
  Avatar: ({ icon }: any) =>
    React.createElement('span', { 'data-testid': 'avatar' }, icon),
  List: Object.assign(
    ({ dataSource, renderItem }: any) =>
      React.createElement(
        'div',
        { 'data-testid': 'list' },
        dataSource.map((item: any, _i: number) => renderItem(item, _i)),
      ),
    {
      Item: Object.assign(
        ({ children, onClick, style }: any) =>
          React.createElement(
            'div',
            { onClick, style, 'data-testid': 'list-item' },
            children,
          ),
        {
          Meta: ({ avatar: _avatar, title, description }: any) =>
            React.createElement('div', null, [
              React.createElement('span', { key: 'a' }, title),
              React.createElement('span', { key: 'd' }, description),
            ]),
        },
      ),
    },
  ),
  Typography: {
    Text: ({ children, strong }: any) =>
      React.createElement(strong ? 'strong' : 'span', null, children),
  },
}));

jest.mock('../../components/FloatingAssistant/ConnectionStatus', () => ({
  ConnectionStatus: () => React.createElement('span', null, 'status'),
}));

jest.mock('../../components/FloatingAssistant/styles', () => ({
  FLOATING_ASSISTANT_STYLES: { agentSelector: {} },
}));

jest.mock('@ant-design/icons', () => ({
  RobotOutlined: () => React.createElement('span', null, 'robot'),
}));

describe('AgentSelector', () => {
  function renderSelector(agentID = 'test-agent') {
    const onSelect = jest.fn();
    const contextValue: XyncraContextValue = {
      client: {} as any,
      connectionStatus: 'connected',
      deviceID: 'test-device',
      agentID,
      functionRegistry: new FunctionRegistry(),
      eventEmitter: new TypedEventEmitter(),
      registerFunction: jest.fn(),
      unregisterFunction: jest.fn(),
    };
    const element = render(
      React.createElement(
        XyncraContext.Provider,
        { value: contextValue },
        React.createElement(AgentSelector, {
          selectedAgentID: null,
          onSelect,
        }),
      ),
    );
    return { ...element, onSelect };
  }

  it('should render the agents header', () => {
    renderSelector();
    expect(screen.getByText('Agents')).toBeTruthy();
  });

  it('should show the AI assistant agent', () => {
    renderSelector();
    expect(screen.getByText('AI 助手')).toBeTruthy();
  });

  it('should call onSelect when agent is clicked', () => {
    const { onSelect } = renderSelector();
    const items = screen.getAllByTestId('list-item');
    items[0].click();
    expect(onSelect).toHaveBeenCalledWith('test-agent');
  });
});
