/**
 * @packageDocumentation
 * Shared agent catalog for the FloatingAssistant UI.
 *
 * Centralised so that AgentSelector (list rendering) and ConversationList
 * (create_conversation title) reference a single source of truth. In
 * production this should be fetched from the server.
 *
 * @module
 */

/**
 * A minimal agent descriptor for the selector list and conversation titles.
 */
export interface AgentItem {
  id: string;
  name: string;
  description: string;
}

/**
 * Default available agents.
 * In production, this should be fetched from the server.
 * Note: Agent IDs must be different from the current user ID.
 */
export const DEFAULT_AGENTS: AgentItem[] = [
  {
    id: 'agent/test-bot',
    name: 'Test Bot',
    description: '基础对话测试助手',
  },
  {
    id: 'agent/weather-bot',
    name: 'Weather Bot',
    description: '全球城市天气查询',
  },
  {
    id: 'agent/hitl-bot',
    name: 'HITL 测试助手',
    description: '需要用户确认的测试 Agent',
  },
  {
    id: 'agent/hitl-parent',
    name: 'HITL Parent',
    description: '并行协调助手 — 同时委派两个子任务',
  },
  {
    id: 'agent/hitl-child-a',
    name: 'HITL Child A',
    description: 'HITL 子 Agent A — 由 Parent 委派',
  },
  {
    id: 'agent/hitl-child-b',
    name: 'HITL Child B',
    description: 'HITL 子 Agent B — 由 Parent 委派',
  },
  {
    id: 'agent/mcp-bot',
    name: 'MCP Bot',
    description: 'MCP 工具调用测试助手',
  },
  {
    id: 'agent/ui-assistant',
    name: '前端页面助手',
    description: '操作前端 UI 的助手',
  },
];

/**
 * Look up an agent's display name by its ID.
 * Returns undefined if the ID is unknown.
 */
export function getAgentName(agentID: string | null): string | undefined {
  if (!agentID) return undefined;
  return DEFAULT_AGENTS.find((a) => a.id === agentID)?.name;
}
