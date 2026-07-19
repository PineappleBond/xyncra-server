import { fireEvent, render, screen } from '@testing-library/react';
import React from 'react';
const mockReact = React;
import { ChatWindow } from '../../components/FloatingAssistant/ChatWindow';

jest.mock('../../components/FloatingAssistant/SidebarPanel', () => ({
  SidebarPanel: ({ onClose, open }: { onClose: () => void; open: boolean }) =>
    mockReact.createElement('div', { 'data-testid': 'sidebar-panel' }, [
      mockReact.createElement('span', { key: 'title' }, 'SidebarPanel'),
      mockReact.createElement('button', { type: 'button', key: 'close', onClick: onClose }, 'Close'),
    ]),
}));

describe('ChatWindow', () => {
  function renderChat() {
    const onClose = jest.fn();
    return { ...render(React.createElement(ChatWindow, { onClose })), onClose };
  }

  it('should render SidebarPanel as expanded', () => {
    renderChat();
    expect(screen.getByTestId('sidebar-panel')).toBeTruthy();
  });

  it('should call onClose when close button is clicked', () => {
    const { onClose } = renderChat();
    fireEvent.click(screen.getByText('Close'));
    expect(onClose).toHaveBeenCalled();
  });
});
