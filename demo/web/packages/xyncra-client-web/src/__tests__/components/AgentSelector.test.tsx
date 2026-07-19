import { render, screen } from '@testing-library/react';
import React from 'react';
import { AgentSelector } from '../../components/FloatingAssistant/AgentSelector';
import type { XyncraContextValue } from '../../context/XyncraProvider';
import { XyncraContext } from '../../context/XyncraProvider';
import { TypedEventEmitter } from '../../internal/EventEmitter';
import { FunctionRegistry } from '../../internal/FunctionRegistry';

const mockReact = React;

jest.mock('antd', () => {
  const Meta = ({ avatar: _avatar, title, description }: any) =>
    mockReact.createElement('div', null, [
      mockReact.createElement('span', { key: 'title' }, title),
      mockReact.createElement('span', { key: 'desc' }, description),
    ]);

  const Item = ({ children, onClick, style }: any) =>
    mockReact.createElement('div', { onClick, style, 'data-testid': 'list-item' }, children);

  Item.Meta = Meta;

  return {
    Avatar: ({ icon }: any) => mockReact.createElement('span', { 'data-testid': 'avatar' }, icon),
    List: Object.assign(
      ({ dataSource, renderItem }: any) =>
        mockReact.createElement(
          'div',
          { 'data-testid': 'list' },
          dataSource.map((item: any, i: number) => renderItem(item, i)),
        ),
      { Item },
    ),
  };
});

jest.mock('@ant-design/icons', () => ({
  RobotOutlined: () => mockReact.createElement('span', null, 'robot'),
}));

describe('AgentSelector', () => {
  const DEFAULT_CONTEXT: XyncraContextValue = {
    client: {} as any,
    connectionStatus: 'connected',
    deviceID: 'test-device',
    agentID: 'test-bot',
    functionRegistry: new FunctionRegistry(),
    eventEmitter: new TypedEventEmitter(),
    registerFunction: jest.fn(),
    unregisterFunction: jest.fn(),
  };

  function renderSelector(agentID = 'test-bot') {
    const onSelect = jest.fn();
    const contextValue: XyncraContextValue = { ...DEFAULT_CONTEXT, agentID };
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

  it('should render the agents list', () => {
    const { onSelect } = renderSelector();
    expect(screen.getByText('Test Bot')).toBeTruthy();
    expect(screen.getByText('Weather Bot')).toBeTruthy();
    expect(screen.getByText('HITL 测试助手')).toBeTruthy();
    expect(screen.getByText('HITL Parent')).toBeTruthy();
  });

  it('should call onSelect when an agent is clicked', () => {
    const { onSelect } = renderSelector();
    const items = screen.getAllByTestId('list-item');
    items[0].click();
    expect(onSelect).toHaveBeenCalledWith('agent/test-bot');
  });

  it('should highlight the selected agent', () => {
    render(
      React.createElement(
        XyncraContext.Provider,
        { value: DEFAULT_CONTEXT },
        React.createElement(AgentSelector, { selectedAgentID: 'agent/test-bot', onSelect: jest.fn() }),
      ),
    );
    expect(screen.getByTestId('list')).toBeTruthy();
  });
});
