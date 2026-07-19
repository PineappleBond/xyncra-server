import { fireEvent, render, screen } from '@testing-library/react';
import React from 'react';
const mockReact = React;
import { FloatingAssistant } from '../../components/FloatingAssistant/FloatingAssistant';

jest.mock('../../components/FloatingAssistant/FloatingButton', () => ({
  FloatingButton: ({ onClick }: { onClick: () => void }) =>
    mockReact.createElement(
      'button',
      { type: 'button', onClick, 'data-testid': 'floating-btn' },
      'Open',
    ),
}));

jest.mock('../../components/FloatingAssistant/ChatWindow', () => ({
  ChatWindow: ({ onClose }: { onClose: () => void }) =>
    mockReact.createElement('div', { 'data-testid': 'chat-window' }, [
      mockReact.createElement('span', { key: 'title' }, 'Chat Window'),
      mockReact.createElement(
        'button',
        { type: 'button', key: 'close', onClick: onClose },
        'Close',
      ),
    ]),
}));

jest.mock('../../components/FloatingAssistant/styles', () => ({
  FLOATING_ASSISTANT_STYLES: {
    container: {},
    chatWindow: {},
    floatingButton: {},
  },
}));

describe('FloatingAssistant', () => {
  it('should render the floating button initially', () => {
    render(React.createElement(FloatingAssistant));
    expect(screen.getByTestId('floating-btn')).toBeTruthy();
    expect(screen.queryByTestId('chat-window')).toBeNull();
  });

  it('should open chat window on button click', () => {
    render(React.createElement(FloatingAssistant));
    fireEvent.click(screen.getByTestId('floating-btn'));
    expect(screen.getByTestId('chat-window')).toBeTruthy();
    expect(screen.queryByTestId('floating-btn')).toBeNull();
  });

  it('should close chat window when onClose is called', () => {
    render(React.createElement(FloatingAssistant));
    fireEvent.click(screen.getByTestId('floating-btn'));
    expect(screen.getByTestId('chat-window')).toBeTruthy();
    fireEvent.click(screen.getByText('Close'));
    expect(screen.getByTestId('floating-btn')).toBeTruthy();
    expect(screen.queryByTestId('chat-window')).toBeNull();
  });

  it('should toggle between button and window', () => {
    render(React.createElement(FloatingAssistant));
    expect(screen.getByTestId('floating-btn')).toBeTruthy();
    fireEvent.click(screen.getByTestId('floating-btn'));
    expect(screen.getByTestId('chat-window')).toBeTruthy();
    fireEvent.click(screen.getByText('Close'));
    expect(screen.getByTestId('floating-btn')).toBeTruthy();
  });
});
