import { fireEvent, render, screen } from '@testing-library/react';
import React from 'react';
import { FloatingButton } from '../../components/FloatingAssistant/FloatingButton';

jest.mock('@ant-design/icons', () => ({
  MessageOutlined: () => React.createElement('span', null, 'icon'),
}));

describe('FloatingButton', () => {
  it('should render a button element', () => {
    render(React.createElement(FloatingButton, { onClick: jest.fn() }));
    const btn = screen.getByRole('button');
    expect(btn).toBeTruthy();
    expect(btn.tagName).toBe('DIV');
  });

  it('should call onClick when clicked', () => {
    const onClick = jest.fn();
    render(React.createElement(FloatingButton, { onClick }));
    fireEvent.click(screen.getByRole('button'));
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it('should call onClick on Enter key', () => {
    const onClick = jest.fn();
    render(React.createElement(FloatingButton, { onClick }));
    fireEvent.keyDown(screen.getByRole('button'), { key: 'Enter' });
    expect(onClick).toHaveBeenCalled();
  });

  it('should call onClick on Space key', () => {
    const onClick = jest.fn();
    render(React.createElement(FloatingButton, { onClick }));
    fireEvent.keyDown(screen.getByRole('button'), { key: ' ' });
    expect(onClick).toHaveBeenCalled();
  });

  it('should have tabIndex for keyboard access', () => {
    render(React.createElement(FloatingButton, { onClick: jest.fn() }));
    const btn = screen.getByRole('button');
    expect(btn.getAttribute('tabindex')).toBe('0');
  });
});
