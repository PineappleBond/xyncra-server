/**
 * @packageDocumentation
 * ConversationList — middle column of the FloatingAssistant.
 *
 * Displays the list of conversations using @ant-design/x Conversations
 * component, with a "new conversation" button and delete functionality.
 *
 * @module
 */

import { PlusOutlined } from '@ant-design/icons';
import { Conversations } from '@ant-design/x';
import type { MenuProps } from 'antd';
import { Button, Empty } from 'antd';
import { useCallback } from 'react';
import { useConversations } from '../../hooks/useConversations';
import { ConnectionStatus } from './ConnectionStatus';
import { FLOATING_ASSISTANT_STYLES } from './styles';

export interface ConversationListProps {
  /** The currently selected conversation ID, or null if none. */
  activeConversationID: string | null;
  /** The currently selected agent ID, or null if none. */
  selectedAgentID: string | null;
  /** Called when the user selects a conversation. */
  onSelect: (id: string) => void;
}

/**
 * Renders the conversation list panel with connection status,
 * a creation button, and a scrollable Conversations component.
 */
export function ConversationList({
  activeConversationID,
  selectedAgentID,
  onSelect,
}: ConversationListProps): React.JSX.Element {
  const { conversations, createConversation, deleteConversation } =
    useConversations();

  const items = conversations.map((conv) => ({
    key: conv.id,
    label: conv.title || '新会话',
  }));

  /**
   * Menu factory: returns a per-item menu with a "delete" action.
   * The menu onClick receives the antd MenuInfo; we stopPropagation
   * to prevent the click from bubbling to the Conversations item.
   */
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

  const handleCreate = useCallback(() => {
    if (!selectedAgentID) {
      // This should not happen if UI is correct, but guard anyway
      return;
    }
    // Create a conversation with the selected agent as the other user.
    void createConversation(selectedAgentID, '新会话');
  }, [createConversation, selectedAgentID]);

  return (
    <div style={FLOATING_ASSISTANT_STYLES.conversationList}>
      <ConnectionStatus />
      <div style={{ padding: 12 }}>
        <Button
          type="primary"
          icon={<PlusOutlined />}
          block
          onClick={handleCreate}
          disabled={!selectedAgentID}
          title={selectedAgentID ? undefined : '请先选择一个 Agent'}
        >
          新建会话
        </Button>
      </div>
      <div style={{ flex: 1, overflow: 'auto' }}>
        {conversations.length === 0 ? (
          <Empty description="暂无会话" />
        ) : (
          <Conversations
            items={items}
            activeKey={activeConversationID ?? undefined}
            onActiveChange={onSelect}
            menu={menu}
          />
        )}
      </div>
    </div>
  );
}
