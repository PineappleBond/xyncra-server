import { render, screen } from '@testing-library/react';
import React from 'react';
import { FunctionCallDisplay } from '../../components/FloatingAssistant/FunctionCallDisplay';

jest.mock('antd', () => ({
  Tag: ({ children, ...props }: any) =>
    React.createElement('span', props, children),
  Typography: {
    Text: ({ children, ...props }: any) =>
      React.createElement('span', props, children),
  },
}));

jest.mock('@ant-design/icons', () => ({
  CodeOutlined: () => React.createElement('span', null, 'code-icon'),
}));

describe('FunctionCallDisplay', () => {
  it('should return null when no props provided', () => {
    const { container } = render(React.createElement(FunctionCallDisplay, {}));
    expect(container.firstChild).toBeNull();
  });

  it('should render function call', () => {
    render(
      React.createElement(FunctionCallDisplay, {
        functionCall: {
          name: 'get_weather',
          arguments: { location: 'Tokyo' },
        },
      }),
    );
    expect(screen.getByText(/get_weather/)).toBeTruthy();
    expect(screen.getByText(/Tokyo/)).toBeTruthy();
  });

  it('should render function result', () => {
    render(
      React.createElement(FunctionCallDisplay, {
        functionResult: {
          name: 'get_weather',
          result: { temp: 22 },
        },
      }),
    );
    expect(screen.getByText(/执行结果/)).toBeTruthy();
    expect(screen.getByText(/22/)).toBeTruthy();
  });

  it('should render error result', () => {
    render(
      React.createElement(FunctionCallDisplay, {
        functionResult: {
          name: 'get_weather',
          result: null,
          error: 'Network error',
        },
      }),
    );
    expect(screen.getByText(/执行失败/)).toBeTruthy();
    expect(screen.getByText(/Network error/)).toBeTruthy();
  });

  it('should render both call and result', () => {
    render(
      React.createElement(FunctionCallDisplay, {
        functionCall: {
          name: 'get_time',
          arguments: { tz: 'UTC' },
        },
        functionResult: {
          name: 'get_time',
          result: '12:00',
        },
      }),
    );
    expect(screen.getByText(/get_time/)).toBeTruthy();
    expect(screen.getByText(/执行结果/)).toBeTruthy();
  });
});
