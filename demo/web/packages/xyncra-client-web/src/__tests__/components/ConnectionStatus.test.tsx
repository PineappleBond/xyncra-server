import { render, screen } from '@testing-library/react';
import React from 'react';
const mockReact = React;
import { ConnectionStatus } from '../../components/FloatingAssistant/ConnectionStatus';
import type {
  ConnectionStatus as ConnectionStatusType,
  XyncraContextValue,
} from '../../context/XyncraProvider';
import { XyncraContext } from '../../context/XyncraProvider';
import { TypedEventEmitter } from '../../internal/EventEmitter';
import { FunctionRegistry } from '../../internal/FunctionRegistry';

jest.mock('antd', () => ({
  Badge: ({ status, text }: any) =>
    mockReact.createElement(
      'span',
      { 'data-testid': 'badge', 'data-status': status },
      text,
    ),
}));

function renderWithStatus(status: ConnectionStatusType) {
  const contextValue: XyncraContextValue = {
    client: {} as any,
    connectionStatus: status,
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
      React.createElement(ConnectionStatus),
    ),
  );
}

describe('ConnectionStatus', () => {
  it('should render connecting status', () => {
    renderWithStatus('connecting');
    expect(screen.getByText('连接中...')).toBeTruthy();
  });

  it('should render connected status', () => {
    renderWithStatus('connected');
    expect(screen.getByText('已连接')).toBeTruthy();
  });

  it('should render disconnected status', () => {
    renderWithStatus('disconnected');
    expect(screen.getByText('未连接')).toBeTruthy();
  });

  it('should render syncing status', () => {
    renderWithStatus('syncing');
    expect(screen.getByText('同步中...')).toBeTruthy();
  });
});
