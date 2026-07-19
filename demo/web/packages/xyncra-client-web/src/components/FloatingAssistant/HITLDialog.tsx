/**
 * @packageDocumentation
 * HITLDialog — Human-in-the-Loop confirmation dialog.
 *
 * When the agent raises HITL questions, this modal appears with all
 * questions displayed in tabs. All questions must be answered before
 * submission is allowed.
 *
 * @module
 */

import { Form, Input, Modal, Tabs, message } from 'antd';
import { useCallback, useEffect, useState } from 'react';
import { useHITL } from '../../hooks/useHITL';

/**
 * Renders the HITL dialog when pending questions exist.
 * Returns null when there are no pending questions.
 */
export function HITLDialog({ conversationId }: { conversationId?: string }): React.JSX.Element | null {
  const { pendingQuestions, answerAll, dismiss, isSubmitting } = useHITL(conversationId);
  const [form] = Form.useForm();
  const [activeTab, setActiveTab] = useState<string>('0');
  const [answers, setAnswers] = useState<Record<string, string>>({});

  // Reset form when questions count changes
  useEffect(() => {
    form.resetFields();
    setActiveTab('0');
  }, [pendingQuestions.length]);

  const handleOk = useCallback(async () => {
    if (pendingQuestions.length === 0) return;

    try {
      const values = await form.validateFields();
      const answers = new Map<string, string>();

      // Collect all answers
      pendingQuestions.forEach((q, index) => {
        const answerKey = `answer_${index}`;
        const answerText = values[answerKey];
        if (answerText && q.questionId) {
          answers.set(q.questionId, answerText);
        }
      });

      await answerAll(answers);
      form.resetFields();
      message.success('所有问题已回答');
    } catch (error) {
      if (error && typeof error === 'object' && 'errorFields' in error) {
        // Form validation error - find which tab has the error
        const errorFields = (error as { errorFields: Array<{ name: string[] }> }).errorFields;
        if (errorFields.length > 0) {
          const fieldName = errorFields[0].name[0];
          const tabIndex = fieldName.replace('answer_', '');
          setActiveTab(tabIndex);
        }
        message.error('请填写所有问题的回答');
      } else {
        message.error('提交失败：' + (error instanceof Error ? error.message : String(error)));
      }
    }
  }, [pendingQuestions, form, answerAll]);

  if (pendingQuestions.length === 0) return null;

  // Check if all questions are answered from reactive state
  const allAnswered = pendingQuestions.every((_, index) => {
    const answerKey = `answer_${index}`;
    return answers[answerKey] && answers[answerKey].trim() !== '';
  });

  const tabItems = pendingQuestions.map((q, index) => ({
    key: String(index),
    label: `问题 ${index + 1}`,
    children: (
      <div>
        <Form.Item label="问题">
          <div
            style={{
              padding: 12,
              backgroundColor: '#fafafa',
              borderRadius: 6,
            }}
          >
            {q.question}
          </div>
        </Form.Item>
        <Form.Item
          name={`answer_${index}`}
          label="您的回答"
          rules={[{ required: true, message: '请输入您的回答' }]}
        >
          <Input.TextArea rows={4} placeholder="请输入..." />
        </Form.Item>
      </div>
    ),
  }));

  return (
    <Modal
      title={`需要您的确认（${pendingQuestions.length} 个问题）`}
      open={pendingQuestions.length > 0}
      onOk={() => {
        void handleOk();
      }}
      onCancel={() => {
        dismiss();
        form.resetFields();
      }}
      okText={isSubmitting ? '提交中...' : '提交全部'}
      cancelText="取消"
      confirmLoading={isSubmitting}
      okButtonProps={{ disabled: !allAnswered || isSubmitting }}
      width={600}
    >
      <Form form={form} layout="vertical" onValuesChange={(_, all) => setAnswers(all)}>
        <Tabs
          activeKey={activeTab}
          onChange={setActiveTab}
          items={tabItems}
          type="card"
        />
      </Form>
    </Modal>
  );
}
