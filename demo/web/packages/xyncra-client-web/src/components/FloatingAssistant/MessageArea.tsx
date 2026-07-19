import { Bubble, Sender } from '@ant-design/x';
import type { BubbleListProps } from '@ant-design/x/es/bubble/interface';
import { Empty } from 'antd';
import { useCallback, useMemo } from 'react';
import { useAgentStatus } from '../../hooks/useAgentStatus';
import { useMessages } from '../../hooks/useMessages';
import { useStreaming } from '../../hooks/useStreaming';
import { MarkdownRenderer } from './MarkdownRenderer';
import { FLOATING_ASSISTANT_STYLES as S } from './styles';

export interface MessageAreaProps {
  conversationID: string | null;
}

const ROLE_CONFIG: BubbleListProps['role'] = {
  user: {
    placement: 'end',
  },
  ai: {
    placement: 'start',
  },
};

export function MessageArea({
  conversationID,
}: MessageAreaProps): React.JSX.Element {
  const { messages, send } = useMessages({ conversationId: conversationID });
  const { isStreaming } = useStreaming();
  const { isTyping } = useAgentStatus();

  const handleSubmit = useCallback(
    (content: string) => {
      if (!content.trim()) return;
      void send(content);
    },
    [send],
  );

  const bubbleItems = useMemo(() => {
    const items: Array<{
      key: string;
      content: React.ReactNode;
      role: string;
      loading?: boolean;
    }> = messages.map((msg) => ({
      key: msg.id,
      content:
        msg.senderId === 'user' ? (
          msg.content
        ) : (
          <MarkdownRenderer content={msg.content} />
        ),
      role: msg.senderId === 'user' ? 'user' : 'ai',
    }));

    if (isTyping || isStreaming) {
      items.push({
        key: '__thinking__',
        content: 'AI 正在思考...',
        role: 'ai',
        loading: true,
      });
    }

    return items;
  }, [messages, isTyping, isStreaming]);

  if (!conversationID) {
    return (
      <div style={S.emptyState}>
        <Empty description="选择一个会话开始对话" />
      </div>
    );
  }

  return (
    <div style={S.messageArea}>
      <div style={S.messageList}>
        {messages.length === 0 && !isStreaming && !isTyping ? (
          <div style={S.emptyState}>
            <Empty description="发送消息开始对话" />
          </div>
        ) : (
          <Bubble.List
            items={bubbleItems}
            role={ROLE_CONFIG}
            autoScroll
          />
        )}
      </div>
      <div style={S.senderArea}>
        <Sender
          placeholder="输入消息..."
          onSubmit={handleSubmit}
          disabled={isStreaming}
        />
      </div>
    </div>
  );
}
