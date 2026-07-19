import { Conversations } from '@ant-design/x';
import type { MenuProps } from 'antd';
import { Empty } from 'antd';
import { useCallback } from 'react';
import { useConversations } from '../../hooks/useConversations';

export interface ConversationListProps {
  activeConversationID: string | null;
  onSelect: (id: string) => void;
}

export function ConversationList({
  activeConversationID,
  onSelect,
}: ConversationListProps): React.JSX.Element {
  const { conversations, deleteConversation } =
    useConversations();

  const items = conversations.map((conv) => ({
    key: conv.id,
    label: conv.title || '新会话',
  }));

  const menu = useCallback(
    (conversation: { key: string }): MenuProps => ({
      items: [
        {
          key: 'delete',
          label: '删除',
          danger: true,
        },
      ],
      onClick: (info) => {
        info.domEvent.stopPropagation();
        if (info.key === 'delete') {
          void deleteConversation(conversation.key);
        }
      },
    }),
    [deleteConversation],
  );

  return conversations.length === 0 ? (
    <Empty description="暂无会话" image={Empty.PRESENTED_IMAGE_SIMPLE} />
  ) : (
    <Conversations
      items={items}
      activeKey={activeConversationID ?? undefined}
      onActiveChange={onSelect}
      menu={menu}
      style={{ width: '100%' }}
    />
  );
}
