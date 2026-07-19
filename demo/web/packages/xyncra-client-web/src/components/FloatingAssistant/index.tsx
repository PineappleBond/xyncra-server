/**
 * @packageDocumentation
 * Entry point for the FloatingAssistant component family.
 *
 * Re-exports all components so consumers can import from a single path:
 *
 * ```ts
 * import { FloatingAssistant, ChatWindow, FloatingButton } from '@xyncra/client-web';
 * ```
 *
 * @module
 */

export type { AgentDetailProps } from './AgentDetail';
export { AgentDetail } from './AgentDetail';
export type { AgentSelectorProps } from './AgentSelector';
export { AgentSelector } from './AgentSelector';
export type { ChatWindowProps } from './ChatWindow';
// Container & sub-components
export { ChatWindow } from './ChatWindow';
export { ConnectionStatus } from './ConnectionStatus';
export type { ConversationListProps } from './ConversationList';
export { ConversationList } from './ConversationList';
// Top-level orchestrator
export { FloatingAssistant } from './FloatingAssistant';
export type { FloatingButtonProps } from './FloatingButton';
export { FloatingButton } from './FloatingButton';
export type {
  FunctionCall,
  FunctionCallDisplayProps,
  FunctionResult,
} from './FunctionCallDisplay';

export { FunctionCallDisplay } from './FunctionCallDisplay';
export { HITLDialog } from './HITLDialog';
export type { MessageAreaProps } from './MessageArea';
export { MessageArea } from './MessageArea';
export type { SidebarPanelProps } from './SidebarPanel';
export { SidebarPanel } from './SidebarPanel';
export { MarkdownRenderer } from './MarkdownRenderer';

// Style constants (for consumers that want to customize)
export { FLOATING_ASSISTANT_STYLES } from './styles';
