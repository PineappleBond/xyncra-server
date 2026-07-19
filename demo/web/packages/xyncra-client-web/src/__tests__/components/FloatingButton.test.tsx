import { fireEvent, render, screen } from '@testing-library/react';
import React from 'react';
const mockReact = React;
import { FloatingButton } from '../../components/FloatingAssistant/FloatingButton';

jest.mock('@ant-design/icons', () => ({
  RobotOutlined: () => mockReact.createElement('span', null, 'robot'),
}));

describe('FloatingButton', () => {
  it('should render a button element', () => {
    render(React.createElement(FloatingButton, { onClick: jest.fn(), visible: true }));
    const btn = screen.getByRole('button');
    expect(btn).toBeTruthy();
    expect(btn.tagName).toBe('BUTTON');
  });

  it('should call onClick when clicked', () => {
    const onClick = jest.fn();
    render(React.createElement(FloatingButton, { onClick, visible: true }));
    fireEvent.click(screen.getByRole('button'));
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it('should have aria-label for accessibility', () => {
    render(React.createElement(FloatingButton, { onClick: jest.fn(), visible: true }));
    const btn = screen.getByRole('button');
    expect(btn.getAttribute('aria-label')).toBe('打开 AI 助手');
  });

  it('should not render when visible is false', () => {
    render(React.createElement(FloatingButton, { onClick: jest.fn(), visible: false }));
    expect(screen.queryByRole('button')).toBeNull();
  });
});
