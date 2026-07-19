/**
 * @packageDocumentation
 * FloatingAssistant — top-level orchestrator component.
 *
 * Manages the collapsed/expanded state: renders a FloatingButton when
 * collapsed, and the full ChatWindow when expanded. Uses fixed positioning
 * at the bottom-right corner of the viewport.
 *
 * Usage:
 * ```tsx
 * import { FloatingAssistant } from '@xyncra/client-web';
 *
 * <XyncraProvider wsUrl="ws://localhost:8080/ws">
 *   <FloatingAssistant />
 * </XyncraProvider>
 * ```
 *
 * @module
 */

import { useCallback, useState } from 'react';
import { ChatWindow } from './ChatWindow';
import { FloatingButton } from './FloatingButton';
import { FLOATING_ASSISTANT_STYLES } from './styles';

/**
 * The top-level FloatingAssistant component.
 *
 * Renders a circular chat button in the bottom-right corner. Clicking it
 * opens a three-column chat window (AgentSelector | ConversationList |
 * MessageArea) with full conversation management, streaming, and HITL support.
 */
export function FloatingAssistant(): React.JSX.Element {
  const [isOpen, setIsOpen] = useState(false);

  const handleOpen = useCallback(() => setIsOpen(true), []);
  const handleClose = useCallback(() => setIsOpen(false), []);

  return (
    <div style={FLOATING_ASSISTANT_STYLES.container}>
      {isOpen ? (
        <ChatWindow onClose={handleClose} />
      ) : (
        <FloatingButton onClick={handleOpen} />
      )}
    </div>
  );
}
