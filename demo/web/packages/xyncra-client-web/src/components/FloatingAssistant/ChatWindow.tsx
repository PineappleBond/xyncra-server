/**
 * @packageDocumentation
 * ChatWindow — the expanded-state container for the FloatingAssistant.
 *
 * Three-column layout: AgentSelector | ConversationList | MessageArea.
 * Wrapped with @ant-design/x XProvider for theme context.
 * Also renders the HITLDialog overlay.
 *
 * Design decisions: D-9 (fixed div, not Modal/Drawer), D-10 (XProvider).
 *
 * @module
 */

import { XProvider } from '@ant-design/x';
import { useCallback, useState } from 'react';
import { AgentSelector } from './AgentSelector';
import { ConversationList } from './ConversationList';
import { HITLDialog } from './HITLDialog';
import { MessageArea } from './MessageArea';
import { FLOATING_ASSISTANT_STYLES } from './styles';

export interface ChatWindowProps {
  /** Called when the user closes the chat window. */
  onClose: () => void;
}

/**
 * The expanded chat window with three columns:
 * - AgentSelector (200px)
 * - ConversationList (240px)
 * - MessageArea (flex: 1)
 *
 * Wrapped with XProvider for @ant-design/x theme context.
 */
export function ChatWindow({ onClose }: ChatWindowProps): React.JSX.Element {
  const [selectedAgentID, setSelectedAgentID] = useState<string | null>(null);
  const [selectedConversationID, setSelectedConversationID] = useState<
    string | null
  >(null);

  const handleAgentSelect = useCallback((agentID: string) => {
    setSelectedAgentID(agentID);
    // Clear conversation selection when switching agents
    setSelectedConversationID(null);
  }, []);

  return (
    <XProvider>
      <div style={FLOATING_ASSISTANT_STYLES.chatWindow}>
        <AgentSelector
          selectedAgentID={selectedAgentID}
          onSelect={handleAgentSelect}
        />
        <ConversationList
          activeConversationID={selectedConversationID}
          selectedAgentID={selectedAgentID}
          onSelect={setSelectedConversationID}
        />
        <MessageArea conversationID={selectedConversationID} />
      </div>
      <HITLDialog />
    </XProvider>
  );
}
