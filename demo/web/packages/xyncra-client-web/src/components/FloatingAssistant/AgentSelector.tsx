import { RobotOutlined } from '@ant-design/icons';
import { Avatar, List } from 'antd';
import { DEFAULT_AGENTS } from '../../constants/agents';

export interface AgentSelectorProps {
  selectedAgentID: string | null;
  onSelect: (agentID: string) => void;
}

export function AgentSelector({
  selectedAgentID,
  onSelect,
}: AgentSelectorProps): React.JSX.Element {
  return (
    <List
      dataSource={DEFAULT_AGENTS}
      renderItem={(agent) => (
        <List.Item
          onClick={() => onSelect(agent.id)}
          style={{
            cursor: 'pointer',
            padding: '12px 16px',
            backgroundColor:
              selectedAgentID === agent.id ? 'var(--color-primary-bg, #e6f4ff)' : undefined,
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
  );
}
