import { CloseOutlined, MessageOutlined, PlusOutlined, RobotOutlined } from '@ant-design/icons';
import { XProvider } from '@ant-design/x';
import { Drawer } from 'antd';
import { useCallback, useEffect, useRef, useState } from 'react';
import { AgentSelector } from './AgentSelector';
import { ConnectionStatus } from './ConnectionStatus';
import { ConversationList } from './ConversationList';
import { HITLDialog } from './HITLDialog';
import { MessageArea } from './MessageArea';
import { FLOATING_ASSISTANT_STYLES as S } from './styles';
import { useConversations } from '../../hooks/useConversations';

export interface SidebarPanelProps {
  open: boolean;
  onClose: () => void;
}

export function SidebarPanel({ open, onClose }: SidebarPanelProps): React.JSX.Element | null {
  const [selectedAgentID, setSelectedAgentID] = useState<string | null>(null);
  const [selectedConversationID, setSelectedConversationID] = useState<string | null>(null);
  const [visible, setVisible] = useState(open);
  const [slideIn, setSlideIn] = useState(false);
  const [showAgentPanel, setShowAgentPanel] = useState(false);
  const [showConvPanel, setShowConvPanel] = useState(false);
  const sidebarRef = useRef<HTMLDivElement>(null);

  const { createConversationWithAgent } = useConversations();

  useEffect(() => {
    if (open) {
      setVisible(true);
      requestAnimationFrame(() => {
        requestAnimationFrame(() => {
          setSlideIn(true);
        });
      });
    } else {
      setSlideIn(false);
    }
  }, [open]);

  // Close sidebar on Escape key
  useEffect(() => {
    if (!visible) return;

    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        onClose();
      }
    }

    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, [visible, onClose]);

  const handleTransitionEnd = useCallback(() => {
    if (!slideIn) {
      setVisible(false);
    }
  }, [slideIn]);

  const handleAgentSelect = useCallback(async (agentID: string) => {
    setSelectedAgentID(agentID);
    setSelectedConversationID(null);
    setShowAgentPanel(false);
    const conv = await createConversationWithAgent(agentID);
    setSelectedConversationID(conv.id);
  }, [createConversationWithAgent]);

  const handleConversationSelect = useCallback((id: string) => {
    setSelectedConversationID(id);
    setShowConvPanel(false);
  }, []);

  const handleCreateConversation = useCallback(() => {
    if (selectedAgentID) {
      void (async () => {
        const conv = await createConversationWithAgent(selectedAgentID);
        setSelectedConversationID(conv.id);
      })();
    } else {
      setShowAgentPanel(true);
    }
  }, [selectedAgentID, createConversationWithAgent]);

  if (!visible) {
    return null;
  }

  const DRAWER_STYLES = {
    body: {
      padding: 0,
    },
  };

  return (
    <div style={S.container}>
      <div
        ref={sidebarRef}
        style={{
          ...S.sidebar,
          transform: slideIn ? 'translateX(0)' : 'translateX(100%)',
          transition: 'transform 250ms cubic-bezier(0.4, 0, 0.2, 1)',
        }}
        onTransitionEnd={handleTransitionEnd}
      >
        <XProvider>
          <div style={{
            ...S.header,
            padding: '8px 12px',
          }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <ConnectionStatus />
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
              <RobotOutlined
                style={{ fontSize: 16, cursor: 'pointer', padding: 6, borderRadius: 6 }}
                onClick={() => setShowAgentPanel(true)}
                title="选择 Agent"
                aria-label="选择 Agent"
              />
              <MessageOutlined
                style={{ fontSize: 16, cursor: 'pointer', padding: 6, borderRadius: 6 }}
                onClick={() => setShowConvPanel(true)}
                title="会话列表"
                aria-label="会话列表"
              />
              <PlusOutlined
                style={{ fontSize: 16, cursor: 'pointer', padding: 6, borderRadius: 6 }}
                onClick={handleCreateConversation}
                title="新建会话"
                aria-label="新建会话"
              />
              <CloseOutlined
                style={{ fontSize: 14, cursor: 'pointer', padding: 6, borderRadius: 6, color: 'var(--color-text-secondary, #999)' }}
                onClick={onClose}
                aria-label="关闭"
              />
            </div>
          </div>

          <div style={S.messageArea}>
            <MessageArea conversationID={selectedConversationID} />
          </div>
        </XProvider>
      </div>

      <Drawer
        title="选择 Agent"
        placement="left"
        open={showAgentPanel}
        onClose={() => setShowAgentPanel(false)}
        getContainer={false}
        style={{ position: 'absolute' }}
        width={280}
        styles={DRAWER_STYLES}
      >
        <AgentSelector
          selectedAgentID={selectedAgentID}
          onSelect={handleAgentSelect}
        />
      </Drawer>

      <Drawer
        title="会话列表"
        placement="left"
        open={showConvPanel}
        onClose={() => setShowConvPanel(false)}
        getContainer={false}
        style={{ position: 'absolute' }}
        width={280}
        styles={DRAWER_STYLES}
      >
        <ConversationList
          activeConversationID={selectedConversationID}
          onSelect={handleConversationSelect}
        />
      </Drawer>

      <HITLDialog conversationId={selectedConversationID ?? undefined} />
    </div>
  );
}
