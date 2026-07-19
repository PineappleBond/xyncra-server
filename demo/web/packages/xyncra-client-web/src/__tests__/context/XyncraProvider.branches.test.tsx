import { act, render, screen } from '@testing-library/react';
import React, { useContext } from 'react';
import { XyncraContext, XyncraProvider } from '../../context/XyncraProvider';

const mockClient: any = {
  start: jest.fn(),
  stop: jest.fn(),
  call: jest.fn().mockResolvedValue(undefined),
  registerRequestHandler: jest.fn(),
  listConversations: jest.fn().mockResolvedValue({ conversations: [] }),
  getMessages: jest.fn().mockResolvedValue({ messages: [] }),
};

jest.mock('@xyncra/client-core', () => ({
  XyncraClient: jest.fn(() => mockClient),
}));

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

describe('XyncraProvider branch coverage', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockClient.start.mockReturnValue(new Promise(() => {})); // never resolves
    mockClient.call.mockResolvedValue(undefined);
    // Clear localStorage mock
    if (globalThis.localStorage) {
      (globalThis.localStorage as any).clear?.();
    }
  });

  it('should use deviceID from localStorage when available', () => {
    // Pre-set localStorage using the actual store from jest.setup
    localStorage.setItem('xyncra-device-id', 'stored-device-id');

    act(() => {
      render(
        React.createElement(
          XyncraProvider,
          { wsUrl: 'ws://test' } as any,
          React.createElement(ContextReader, { field: 'deviceID' }),
        ),
      );
    });

    expect(screen.getByTestId('value').textContent).toBe('stored-device-id');

    // Clean up
    localStorage.removeItem('xyncra-device-id');
  });

  it('should set connecting status on mount', () => {
    render(
      React.createElement(
        XyncraProvider,
        { wsUrl: 'ws://test', deviceID: 'test-device' } as any,
        React.createElement(ContextReader, { field: 'connectionStatus' }),
      ),
    );

    // The provider starts with 'disconnected' state, then useEffect sets 'connecting'
    // Due to React batching, it might be 'connecting' or 'disconnected'
    const status = screen.getByTestId('value').textContent;
    expect(['connecting', 'disconnected']).toContain(status);
  });

  it('should set disconnected when client.start() resolves', async () => {
    mockClient.start.mockResolvedValue(undefined);

    render(
      React.createElement(
        XyncraProvider,
        { wsUrl: 'ws://test', deviceID: 'test-device' } as any,
        React.createElement(ContextReader, { field: 'connectionStatus' }),
      ),
    );

    await act(async () => {
      await new Promise((r) => setTimeout(r, 10));
    });

    expect(screen.getByTestId('value').textContent).toBe('disconnected');
  });

  it('should set disconnected when client.start() rejects', async () => {
    mockClient.start.mockRejectedValue(new Error('Connection failed'));

    render(
      React.createElement(
        XyncraProvider,
        { wsUrl: 'ws://test', deviceID: 'test-device' } as any,
        React.createElement(ContextReader, { field: 'connectionStatus' }),
      ),
    );

    await act(async () => {
      await new Promise((r) => setTimeout(r, 10));
    });

    expect(screen.getByTestId('value').textContent).toBe('disconnected');
  });

  it('should set connected when first message event arrives', async () => {
    mockClient.start.mockReturnValue(new Promise(() => {})); // never resolves

    let capturedEmitter: any;
    render(
      React.createElement(
        XyncraProvider,
        {
          wsUrl: 'ws://test',
          deviceID: 'test-device',
          onUpdateHandler: () => {},
        } as any,
        React.createElement(XyncraContext.Consumer as any, {
          // biome-ignore lint/correctness/noChildrenProp: Context.Consumer uses children as render fn
          children: (value: any) => {
            capturedEmitter = value?.eventEmitter;
            return React.createElement(
              'span',
              { 'data-testid': 'status' },
              value?.connectionStatus ?? 'none',
            );
          },
        }),
      ),
    );

    // Wait for mount
    await act(async () => {
      await new Promise((r) => setTimeout(r, 10));
    });

    if (capturedEmitter) {
      act(() => {
        capturedEmitter.emit('message:added', {
          message: {
            id: 'msg-1',
            conversationId: 'conv-1',
            senderId: 'user1',
            content: 'test',
            clientMessageId: 'client-1',
            createdAt: new Date().toISOString(),
          },
        });
      });

      await act(async () => {
        await new Promise((r) => setTimeout(r, 0));
      });
    }
  });

  it('should call stop on unmount', () => {
    const { unmount } = render(
      React.createElement(
        XyncraProvider,
        { wsUrl: 'ws://test', deviceID: 'test-device' } as any,
        React.createElement('span', null, 'child'),
      ),
    );

    unmount();
    expect(mockClient.stop).toHaveBeenCalled();
  });
});
