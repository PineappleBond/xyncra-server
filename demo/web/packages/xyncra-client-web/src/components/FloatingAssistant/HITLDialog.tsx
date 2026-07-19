/**
 * @packageDocumentation
 * HITLDialog — Human-in-the-Loop confirmation dialog.
 *
 * When the agent times out or raises a HITL interrupt, this modal
 * appears with the question text and an input area for the user to
 * respond. Supports both free-text answers and option-based selection.
 *
 * @module
 */

import { Form, Input, Modal, Radio } from 'antd';
import { useCallback } from 'react';
import { useHITL } from '../../hooks/useHITL';

/**
 * Renders the HITL dialog when a pending question exists.
 * Returns null when there is no pending question.
 */
export function HITLDialog(): React.JSX.Element | null {
  const { pendingQuestion, answer, dismiss } = useHITL();
  const [form] = Form.useForm<{ answer: string }>();

  const handleOk = useCallback(async () => {
    if (!pendingQuestion) return;
    const values = await form.validateFields();
    await answer(pendingQuestion.questionId ?? pendingQuestion.userId, values.answer);
    form.resetFields();
  }, [pendingQuestion, form, answer]);

  if (!pendingQuestion) return null;

  return (
    <Modal
      title="需要您的确认"
      open={!!pendingQuestion}
      onOk={() => {
        void handleOk();
      }}
      onCancel={() => {
        dismiss();
        form.resetFields();
      }}
      okText="提交"
      cancelText="取消"
    >
      <Form form={form} layout="vertical">
        <Form.Item label="问题">
          <div
            style={{
              padding: 12,
              backgroundColor: '#fafafa',
              borderRadius: 6,
            }}
          >
            {pendingQuestion.question}
          </div>
        </Form.Item>
        <Form.Item
          name="answer"
          label="您的回答"
          rules={[{ required: true, message: '请输入您的回答' }]}
        >
          <Input.TextArea rows={4} placeholder="请输入..." />
        </Form.Item>
      </Form>
    </Modal>
  );
}
