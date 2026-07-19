import { fireEvent, render, screen } from '@testing-library/react';
import React from 'react';
const mockReact = React;
import { HITLDialog } from '../../components/FloatingAssistant/HITLDialog';
import type { XyncraContextValue } from '../../context/XyncraProvider';
import { XyncraContext } from '../../context/XyncraProvider';
import { TypedEventEmitter } from '../../internal/EventEmitter';
import { FunctionRegistry } from '../../internal/FunctionRegistry';

const mockAnswer = jest.fn();
const mockDismiss = jest.fn();

jest.mock('../../hooks/useHITL', () => ({
  useHITL: () => ({
    pendingQuestion: {
      userId: 'agent1',
      conversationId: 'conv-1',
      question: 'Should I proceed?',
      questionId: 'q-abc',
    },
    answer: mockAnswer,
    dismiss: mockDismiss,
  }),
}));

jest.mock('antd', () => ({
  Modal: ({
    title,
    open,
    onOk,
    onCancel,
    children,
    okText,
    cancelText,
  }: any) => {
    if (!open) return null;
    return mockReact.createElement('div', { 'data-testid': 'modal' }, [
      mockReact.createElement('h3', { key: 'title' }, title),
      mockReact.createElement('div', { key: 'content' }, children),
      mockReact.createElement(
        'button',
        { type: 'button', key: 'ok', onClick: onOk },
        okText || 'OK',
      ),
      mockReact.createElement(
        'button',
        { type: 'button', key: 'cancel', onClick: onCancel },
        cancelText || 'Cancel',
      ),
    ]);
  },
  Form: Object.assign(
    ({ children }: any) => mockReact.createElement('form', null, children),
    {
      Item: ({ children, label }: any) =>
        mockReact.createElement('div', null, [
          label ? mockReact.createElement('label', { key: 'l' }, label) : null,
          mockReact.createElement('div', { key: 'c' }, children),
        ]),
      useForm: () => [
        {
          validateFields: jest
            .fn()
            .mockResolvedValue({ answer: 'test-answer' }),
          resetFields: jest.fn(),
        },
      ],
    },
  ),
  Input: {
    TextArea: ({ placeholder }: any) =>
      mockReact.createElement('textarea', {
        placeholder,
        'data-testid': 'answer-input',
      }),
  },
  Radio: {
    Group: ({ children }: any) => mockReact.createElement('div', null, children),
  },
}));

describe('HITLDialog', () => {
  beforeEach(() => {
    mockAnswer.mockClear();
    mockDismiss.mockClear();
  });

  function renderDialog() {
    const contextValue: XyncraContextValue = {
      client: {} as any,
      connectionStatus: 'connected',
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
        React.createElement(HITLDialog),
      ),
    );
  }

  it('should render the HITL dialog with question', () => {
    renderDialog();
    expect(screen.getByTestId('modal')).toBeTruthy();
    expect(screen.getByText('Should I proceed?')).toBeTruthy();
  });

  it('should have submit and cancel buttons', () => {
    renderDialog();
    expect(screen.getByText('提交')).toBeTruthy();
    expect(screen.getByText('取消')).toBeTruthy();
  });

  it('should call dismiss on cancel', () => {
    renderDialog();
    fireEvent.click(screen.getByText('取消'));
    expect(mockDismiss).toHaveBeenCalled();
  });

  it('should pass questionId (not userId) to answer', async () => {
    renderDialog();
    fireEvent.click(screen.getByText('提交'));
    // The answer handler resolves asynchronously; wait a tick.
    await new Promise((r) => setTimeout(r, 0));
    expect(mockAnswer).toHaveBeenCalledWith('q-abc', 'test-answer');
  });
});
