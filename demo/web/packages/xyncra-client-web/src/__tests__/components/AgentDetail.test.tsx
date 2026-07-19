import { render, screen } from '@testing-library/react';
import React from 'react';
import { AgentDetail } from '../../components/FloatingAssistant/AgentDetail';
import type { XyncraContextValue } from '../../context/XyncraProvider';
import { XyncraContext } from '../../context/XyncraProvider';
import { TypedEventEmitter } from '../../internal/EventEmitter';
import { FunctionRegistry } from '../../internal/FunctionRegistry';

jest.mock('antd', () => {
  const Descriptions = ({ children }: any) =>
    React.createElement('div', { 'data-testid': 'descriptions' }, children);
  (Descriptions as any).Item = ({ children, label }: any) =>
    React.createElement('div', null, [
      React.createElement('span', { key: 'label' }, label),
      React.createElement('span', { key: 'value' }, children),
    ]);

  return {
    Descriptions,
    Typography: {
      Title: ({ children, level }: any) =>
        React.createElement(`h${level || 5}`, null, children),
    },
  };
});

describe('AgentDetail', () => {
  function renderWithMock(deviceID = 'test-device-123') {
    const contextValue: XyncraContextValue = {
      client: {} as any,
      connectionStatus: 'connected',
      deviceID,
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
        React.createElement(AgentDetail, { agentID: 'my-agent' }),
      ),
    );
  }

  it('should render agent details', () => {
    renderWithMock();
    expect(screen.getByText('Agent 详情')).toBeTruthy();
    expect(screen.getByText('my-agent')).toBeTruthy();
    expect(screen.getByText('test-device-123')).toBeTruthy();
  });

  it('should show online status', () => {
    renderWithMock();
    expect(screen.getByText('在线')).toBeTruthy();
  });
});
