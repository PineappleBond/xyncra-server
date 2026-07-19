/**
 * @packageDocumentation
 * Entry point for the `@xyncra/client-web` package.
 *
 * Re-exports all public types, classes, and functions from the web
 * sub-modules so consumers can import everything from a single path:
 *
 * ```ts
 * import {
 *   BrowserWebSocketAdapter, BrowserWebSocketFactory,
 *   BrowserIndexedDBProvider,
 *   ConsoleLogger,
 *   ReactUpdateHandler,
 *   FunctionRegistry,
 *   TypedEventEmitter,
 * } from '@xyncra/client-web';
 * ```
 */

export { BrowserIndexedDBProvider } from './adapters/indexeddb';
export type { Logger } from './adapters/logger';
export { ConsoleLogger } from './adapters/logger';
export type { CloseEvent, WebSocketAdapter } from './adapters/websocket';
// Adapters
export {
  BrowserWebSocketAdapter,
  BrowserWebSocketFactory,
} from './adapters/websocket';
export type {
  AgentDetailProps,
  AgentSelectorProps,
  ChatWindowProps,
  ConversationListProps,
  FloatingButtonProps,
  FunctionCall,
  FunctionCallDisplayProps,
  FunctionResult,
  MessageAreaProps,
} from './components/FloatingAssistant';
// UI Components (Phase 4 — FloatingAssistant)
export {
  AgentDetail,
  AgentSelector,
  ChatWindow,
  ConnectionStatus as ConnectionStatusBadge,
  ConversationList,
  FLOATING_ASSISTANT_STYLES,
  FloatingAssistant,
  FloatingButton,
  FunctionCallDisplay,
  HITLDialog,
  MessageArea,
} from './components/FloatingAssistant';
export type {
  ConnectionStatus,
  XyncraContextValue,
  XyncraProviderProps,
} from './context/XyncraProvider';
// React integration (Phase 2)
export { XyncraContext, XyncraProvider } from './context/XyncraProvider';
export type {
  AgentStatus,
  UseAgentStatusReturn,
} from './hooks/useAgentStatus';
export { useAgentStatus } from './hooks/useAgentStatus';
export type { UseConversationsReturn } from './hooks/useConversations';
// Data hooks (Phase 3)
export { useConversations } from './hooks/useConversations';
export type { HITLQuestion, UseHITLReturn } from './hooks/useHITL';
export { useHITL } from './hooks/useHITL';
export type {
  UseMessagesParams,
  UseMessagesReturn,
} from './hooks/useMessages';
export { useMessages } from './hooks/useMessages';
export { useRegisterFunction } from './hooks/useRegisterFunction';
export type { FunctionEntry } from './hooks/useRegisterFunctions';
export { useRegisterFunctions } from './hooks/useRegisterFunctions';
export type { UseStreamingReturn } from './hooks/useStreaming';
export { useStreaming } from './hooks/useStreaming';
export { useXyncra } from './hooks/useXyncra';
export type {
  ConversationEvent,
  MessageEvent,
  UpdateHandlerEventMap,
} from './internal/EventEmitter';
// Internal modules
export { TypedEventEmitter } from './internal/EventEmitter';
export type { FunctionHandler } from './internal/FunctionRegistry';
export { FunctionRegistry } from './internal/FunctionRegistry';
export { isAgentUser, ReactUpdateHandler } from './internal/ReactUpdateHandler';
