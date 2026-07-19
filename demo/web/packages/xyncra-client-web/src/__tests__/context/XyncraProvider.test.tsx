import { act, render, screen } from '@testing-library/react';
import React, { useContext } from 'react';
import { XyncraContext, XyncraProvider } from '../../context/XyncraProvider';

jest.mock('@xyncra/client-core', () => {
  const mockClient = {
    start: jest.fn().mockReturnValue(new Promise(() => {})),
    stop: jest.fn(),
    call: jest.fn().mockResolvedValue(undefined),
    registerRequestHandler: jest.fn(),
    listConversations: jest.fn().mockResolvedValue({ conversations: [] }),
    getMessages: jest.fn().mockResolvedValue({ messages: [] }),
  };
  return {
    XyncraClient: jest.fn(() => mockClient),
    isAgentUser: (id: string) => id.startsWith('agent'),
  };
});

jest.mock('../../adapters/websocket', () => ({
  BrowserWebSocketFactory: jest.fn(() => ({ create: jest.fn() })),
}));

jest.mock('../../adapters/indexeddb', () => ({
  BrowserIndexedDBProvider: jest.fn(() => ({
    init: jest.fn().mockResolvedValue(undefined),
    getIDBFactory: jest.fn(),
  })),
}));

jest.mock('../../adapters/logger', () => ({
  ConsoleLogger: jest.fn(() => ({
    debug: jest.fn(),
    info: jest.fn(),
    warn: jest.fn(),
    error: jest.fn(),
  })),
}));

function ContextReader({ field }: { field: string }) {
  const ctx = useContext(XyncraContext);
  const value = (ctx as any)?.[field];
  const display =
    typeof value === 'function'
      ? 'function'
      : typeof value === 'string'
        ? value
        : String(value ?? 'none');
  return React.createElement('span', { 'data-testid': 'value' }, display);
}

describe('XyncraProvider', () => {
  it('should provide context to children', () => {
    render(
      React.createElement(
        XyncraProvider,
        { wsUrl: 'ws://test', deviceID: 'test-device' } as any,
        React.createElement(ContextReader, { field: 'connectionStatus' }),
      ),
    );
    expect(screen.getByTestId('value')).toBeTruthy();
  });

  it('should use provided deviceID', () => {
    render(
      React.createElement(
        XyncraProvider,
        { wsUrl: 'ws://test', deviceID: 'my-custom-device' } as any,
        React.createElement(ContextReader, { field: 'deviceID' }),
      ),
    );
    expect(screen.getByTestId('value').textContent).toBe('my-custom-device');
  });

  it('should auto-generate deviceID when not provided', () => {
    render(
      React.createElement(
        XyncraProvider,
        { wsUrl: 'ws://test' } as any,
        React.createElement(ContextReader, { field: 'deviceID' }),
      ),
    );
    const deviceId = screen.getByTestId('value').textContent;
    expect(deviceId).toBeTruthy();
    expect(deviceId).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i,
    );
  });

  it('should expose registerFunction and unregisterFunction', () => {
    render(
      React.createElement(
        XyncraProvider,
        { wsUrl: 'ws://test', deviceID: 'test-device' } as any,
        React.createElement(ContextReader, { field: 'registerFunction' }),
      ),
    );
    expect(screen.getByTestId('value').textContent).toBe('function');
  });

  it('should use agentID prop', () => {
    render(
      React.createElement(
        XyncraProvider,
        {
          wsUrl: 'ws://test',
          deviceID: 'test-device',
          agentID: 'my-agent',
        } as any,
        React.createElement(ContextReader, { field: 'agentID' }),
      ),
    );
    expect(screen.getByTestId('value').textContent).toBe('my-agent');
  });

  it('should show syncing on the 2s empty-database fallback (D-130)', () => {
    jest.useFakeTimers();
    try {
      render(
        React.createElement(
          XyncraProvider,
          { wsUrl: 'ws://test', deviceID: 'test-device' } as any,
          React.createElement(ContextReader, { field: 'connectionStatus' }),
        ),
      );
      // Before the 2s window, status is 'connecting'.
      expect(screen.getByTestId('value').textContent).toBe('connecting');
      act(() => {
        jest.advanceTimersByTime(2000);
      });
      // Per D-130, an empty database falls back to 'syncing', not a fake
      // 'connected'.
      expect(screen.getByTestId('value').textContent).toBe('syncing');
    } finally {
      jest.useRealTimers();
    }
  });
});
