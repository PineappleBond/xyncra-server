/**
 * @packageDocumentation
 * MessageArea — right column of the FloatingAssistant.
 *
 * Displays messages using @ant-design/x Bubble.List with role-based
 * placement (user messages on the right, AI on the left), a streaming
 * text indicator, the Think component for agent thinking state, and
 * a Sender input at the bottom.
 *
 * @module
 */

import { Bubble, Sender, Think } from '@ant-design/x';
import type { BubbleListProps } from '@ant-design/x/es/bubble/interface';
import { Empty } from 'antd';
import { useCallback, useMemo } from 'react';
import { useAgentStatus } from '../../hooks/useAgentStatus';
import { useMessages } from '../../hooks/useMessages';
import { useStreaming } from '../../hooks/useStreaming';
import { FLOATING_ASSISTANT_STYLES } from './styles';

export interface MessageAreaProps {
  /** The conversation ID to display messages for. Null shows an empty state. */
  conversationID: string | null;
}

/**
 * Role configuration for Bubble.List.
 *
 * `user` role is placed at the end (right side).
 * `ai` role is placed at the start (left side).
 */
const ROLE_CONFIG: BubbleListProps['role'] = {
  user: {
    placement: 'end',
  },
  ai: {
    placement: 'start',
  },
};

/**
 * Renders the message area: a scrollable bubble list, a typing/thinking
 * indicator, and a sender input at the bottom.
 */
export function MessageArea({
  conversationID,
}: MessageAreaProps): React.JSX.Element {
  const { messages, send } = useMessages({ conversationId: conversationID });
  const { streamingText, isStreaming } = useStreaming();
  const { isTyping } = useAgentStatus();

  const handleSubmit = useCallback(
    (content: string) => {
      if (!content.trim()) return;
      void send(content);
    },
    [send],
  );

  /**
   * Build Bubble.List items from the messages array.
   * Messages from 'user' sender get role='user', everything else is 'ai'.
   */
  const bubbleItems = useMemo(() => {
    const items: Array<{
      key: string;
      content: string;
      role: string;
      loading?: boolean;
    }> = messages.map((msg) => ({
      key: msg.id,
      content: msg.content,
      role: msg.senderId === 'user' ? 'user' : 'ai',
    }));

    // Append a streaming bubble if a stream is active.
    if (isStreaming && streamingText) {
      items.push({
        key: 'streaming',
        content: streamingText,
        role: 'ai',
        loading: true,
      });
    }

    return items;
  }, [messages, isStreaming, streamingText]);

  if (!conversationID) {
    return (
      <div
        style={{
          ...FLOATING_ASSISTANT_STYLES.messageArea,
          alignItems: 'center',
          justifyContent: 'center',
        }}
      >
        <Empty description="选择一个会话开始对话" />
      </div>
    );
  }

  return (
    <div style={FLOATING_ASSISTANT_STYLES.messageArea}>
      <div style={{ flex: 1, overflow: 'auto', padding: 16 }}>
        {messages.length === 0 && !isStreaming ? (
          <Empty description="发送消息开始对话" />
        ) : (
          <>
            <Bubble.List items={bubbleItems} role={ROLE_CONFIG} />
            {isTyping && <Think loading title="AI 正在思考..." />}
          </>
        )}
      </div>
      <div style={{ padding: 12, borderTop: '1px solid #f0f0f0' }}>
        <Sender
          placeholder="输入消息..."
          onSubmit={handleSubmit}
          disabled={isStreaming}
        />
      </div>
    </div>
  );
}
