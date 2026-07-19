import { render, screen } from '@testing-library/react';
import React from 'react';
import { AgentSelector } from '../../components/FloatingAssistant/AgentSelector';
import type { XyncraContextValue } from '../../context/XyncraProvider';
import { XyncraContext } from '../../context/XyncraProvider';
import { TypedEventEmitter } from '../../internal/EventEmitter';
import { FunctionRegistry } from '../../internal/FunctionRegistry';

// jest.mock factory functions cannot reference out-of-scope variables, so we
// alias React to a `mock`-prefixed name that jest permits inside factories.
const mockReact = React;

jest.mock('antd', () => ({
  Avatar: ({ icon }: any) =>
    mockReact.createElement('span', { 'data-testid': 'avatar' }, icon),
  List: Object.assign(
    ({ dataSource, renderItem }: any) =>
      mockReact.createElement(
        'div',
        { 'data-testid': 'list' },
        dataSource.map((item: any, _i: number) => renderItem(item, _i)),
      ),
    {
      Item: Object.assign(
        ({ children, onClick, style }: any) =>
          mockReact.createElement(
            'div',
            { onClick, style, 'data-testid': 'list-item' },
            children,
          ),
        {
          Meta: ({ avatar: _avatar, title, description }: any) =>
            mockReact.createElement('div', null, [
              mockReact.createElement('span', { key: 'a' }, title),
              mockReact.createElement('span', { key: 'd' }, description),
            ]),
        },
      ),
    },
  ),
  Typography: {
    Text: ({ children, strong }: any) =>
      mockReact.createElement(strong ? 'strong' : 'span', null, children),
  },
}));

jest.mock('../../components/FloatingAssistant/ConnectionStatus', () => ({
  ConnectionStatus: () => mockReact.createElement('span', null, 'status'),
}));

jest.mock('../../components/FloatingAssistant/styles', () => ({
  FLOATING_ASSISTANT_STYLES: { agentSelector: {} },
}));

jest.mock('@ant-design/icons', () => ({
  RobotOutlined: () => mockReact.createElement('span', null, 'robot'),
}));

describe('AgentSelector', () => {
  // The component ignores context.agentID and renders DEFAULT_AGENTS instead,
  // so the injected agentID here is unused by AgentSelector (kept only to
  // satisfy the XyncraContextValue type).
  function renderSelector(agentID = 'test-bot') {
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

  it('should show the default agents', () => {
    renderSelector();
    expect(screen.getByText('Test Bot')).toBeTruthy();
    expect(screen.getByText('Weather Bot')).toBeTruthy();
    expect(screen.getByText('HITL 测试助手')).toBeTruthy();
    expect(screen.getByText('HITL Parent')).toBeTruthy();
  });

  it('should call onSelect when agent is clicked', () => {
    const { onSelect } = renderSelector();
    const items = screen.getAllByTestId('list-item');
    items[0].click();
    expect(onSelect).toHaveBeenCalledWith('agent/test-bot');
  });
});
