import { Bubble, Sender } from '@ant-design/x';
import type { BubbleListProps, BubbleListRef } from '@ant-design/x/es/bubble/interface';
import { Button, Empty } from 'antd';
import { useCallback, useEffect, useMemo, useRef } from 'react';
import { useAgentStatus } from '../../hooks/useAgentStatus';
import { useMessages } from '../../hooks/useMessages';
import { useStreaming } from '../../hooks/useStreaming';
import { formatRelativeTime } from '../../utils/formatRelativeTime';
import { MarkdownRenderer } from './MarkdownRenderer';
import { FLOATING_ASSISTANT_STYLES as S } from './styles';

export interface MessageAreaProps {
  conversationID: string | null;
}

const ROLE_CONFIG: BubbleListProps['role'] = {
  user: {
    placement: 'end',
    footerPlacement: 'outer-end',
  },
  ai: {
    placement: 'start',
    footerPlacement: 'outer-start',
  },
};

export function MessageArea({
  conversationID,
}: MessageAreaProps): React.JSX.Element {
  const { messages, send, fetchMore, loadingMore, hasMore } = useMessages({
    conversationId: conversationID,
  });
  const { isStreaming } = useStreaming();
  const { isTyping } = useAgentStatus();
  const bubbleListRef = useRef<BubbleListRef>(null);

  // Auto-load-more when user scrolls to the top of the message list.
  useEffect(() => {
    const scrollEl = bubbleListRef.current?.scrollBoxNativeElement;
    if (!scrollEl) return;

    const handleScroll = () => {
      if (scrollEl.scrollTop < 50 && hasMore && !loadingMore) {
        // Save current scroll height to restore position after prepend.
        const prevHeight = scrollEl.scrollHeight;
        void fetchMore().then(() => {
          // Restore scroll position so the viewport doesn't jump.
          requestAnimationFrame(() => {
            const newHeight = scrollEl.scrollHeight;
            scrollEl.scrollTop += newHeight - prevHeight;
          });
        });
      }
    };

    scrollEl.addEventListener('scroll', handleScroll, { passive: true });
    return () => scrollEl.removeEventListener('scroll', handleScroll);
  }, [fetchMore, hasMore, loadingMore]);

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
      footer?: React.ReactNode;
    }> = messages.map((msg) => ({
      key: msg.id,
      content:
        msg.senderId === 'user' ? (
          msg.content
        ) : (
          <MarkdownRenderer content={msg.content} />
        ),
      role: msg.senderId === 'user' ? 'user' : 'ai',
      footer: (
        <span style={msg.senderId === 'user' ? S.timestampRight : S.timestampLeft}>
          {formatRelativeTime(msg.createdAt)}
        </span>
      ),
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
          <>
            {hasMore && messages.length > 0 && (
              <div style={S.loadMoreTrigger}>
                <Button
                  type="link"
                  size="small"
                  loading={loadingMore}
                  onClick={() => {
                    const scrollEl = bubbleListRef.current?.scrollBoxNativeElement;
                    const prevHeight = scrollEl?.scrollHeight ?? 0;
                    void fetchMore().then(() => {
                      requestAnimationFrame(() => {
                        if (scrollEl) {
                          scrollEl.scrollTop += scrollEl.scrollHeight - prevHeight;
                        }
                      });
                    });
                  }}
                >
                  加载更多消息
                </Button>
              </div>
            )}
            <Bubble.List
              ref={bubbleListRef}
              items={bubbleItems}
              role={ROLE_CONFIG}
              autoScroll
            />
          </>
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
