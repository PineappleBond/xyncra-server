import { fireEvent, render, screen } from '@testing-library/react';
import React from 'react';
const mockReact = React;
import { FloatingAssistant } from '../../components/FloatingAssistant/FloatingAssistant';

jest.mock('../../components/FloatingAssistant/FloatingButton', () => ({
  FloatingButton: ({ onClick, visible }: { onClick: () => void; visible: boolean }) =>
    visible ? mockReact.createElement('button', { type: 'button', onClick, 'data-testid': 'floating-btn' }, 'Open') : null,
}));

jest.mock('../../components/FloatingAssistant/SidebarPanel', () => ({
  SidebarPanel: ({ onClose, open }: { onClose: () => void; open: boolean }) =>
    open ? mockReact.createElement('div', { 'data-testid': 'sidebar-panel' }, [
      mockReact.createElement('span', { key: 'title' }, 'Sidebar'),
      mockReact.createElement('button', { type: 'button', key: 'close', onClick: onClose }, 'Close'),
    ]) : null,
}));

jest.mock('../../components/FloatingAssistant/styles', () => ({
  FLOATING_ASSISTANT_STYLES: {
    container: {},
    sidebar: {},
    header: {},
    agentSelector: {},
    conversationPanel: {},
    messageArea: {},
    senderArea: {},
  },
}));

describe('FloatingAssistant', () => {
  it('should render the floating button initially', () => {
    render(React.createElement(FloatingAssistant));
    expect(screen.getByTestId('floating-btn')).toBeTruthy();
    expect(screen.queryByTestId('sidebar-panel')).toBeNull();
  });

  it('should open sidebar panel on button click', () => {
    render(React.createElement(FloatingAssistant));
    fireEvent.click(screen.getByTestId('floating-btn'));
    expect(screen.getByTestId('sidebar-panel')).toBeTruthy();
    expect(screen.queryByTestId('floating-btn')).toBeNull();
  });

  it('should close sidebar panel when onClose is called', () => {
    render(React.createElement(FloatingAssistant));
    fireEvent.click(screen.getByTestId('floating-btn'));
    expect(screen.getByTestId('sidebar-panel')).toBeTruthy();
    fireEvent.click(screen.getByText('Close'));
    expect(screen.getByTestId('floating-btn')).toBeTruthy();
    expect(screen.queryByTestId('sidebar-panel')).toBeNull();
  });

  it('should toggle between button and panel', () => {
    render(React.createElement(FloatingAssistant));
    expect(screen.getByTestId('floating-btn')).toBeTruthy();
    fireEvent.click(screen.getByTestId('floating-btn'));
    expect(screen.getByTestId('sidebar-panel')).toBeTruthy();
    fireEvent.click(screen.getByText('Close'));
    expect(screen.getByTestId('floating-btn')).toBeTruthy();
  });
});
