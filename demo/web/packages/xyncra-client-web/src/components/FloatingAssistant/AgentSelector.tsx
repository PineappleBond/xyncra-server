/**
 * @packageDocumentation
 * AgentSelector — left column of the FloatingAssistant.
 *
 * Displays a list of available agents. Currently shows a single default
 * agent derived from the XyncraProvider context. Designed to be extended
 * for multi-agent support in future versions.
 *
 * @module
 */

import { RobotOutlined } from '@ant-design/icons';
import { Avatar, List, Typography } from 'antd';
import { ConnectionStatus } from './ConnectionStatus';
import { FLOATING_ASSISTANT_STYLES } from './styles';

/**
 * A minimal agent descriptor for the selector list.
 */
interface AgentItem {
  id: string;
  name: string;
  description: string;
}

/**
 * Default available agents.
 * In production, this should be fetched from the server.
 * Note: Agent IDs must be different from the current user ID.
 */
const DEFAULT_AGENTS: AgentItem[] = [
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
];

export interface AgentSelectorProps {
  /** The currently selected agent ID, or null if none. */
  selectedAgentID: string | null;
  /** Called when the user selects an agent. */
  onSelect: (agentID: string) => void;
}

/**
 * Renders the agent selection panel with connection status,
 * a header, and a list of available agents.
 */
export function AgentSelector({
  selectedAgentID,
  onSelect,
}: AgentSelectorProps): React.JSX.Element {
  // Use default agents list instead of current user ID
  const agents = DEFAULT_AGENTS;

  return (
    <div style={FLOATING_ASSISTANT_STYLES.agentSelector}>
      <ConnectionStatus />
      <div
        style={{
          padding: 12,
          borderBottom: '1px solid #f0f0f0',
        }}
      >
        <Typography.Text strong>Agents</Typography.Text>
      </div>
      <List
        dataSource={agents}
        renderItem={(agent) => (
          <List.Item
            onClick={() => onSelect(agent.id)}
            style={{
              cursor: 'pointer',
              backgroundColor:
                selectedAgentID === agent.id ? '#e6f7ff' : undefined,
              padding: '12px 16px',
            }}
          >
            <List.Item.Meta
              avatar={<Avatar icon={<RobotOutlined />} />}
              title={agent.name}
              description={agent.description}
            />
          </List.Item>
        )}
      />
    </div>
  );
}
