/**
 * @packageDocumentation
 * AgentDetail — displays detailed information about a selected agent.
 *
 * Shows agent ID, device ID, and online status. Reserved for future
 * expansion when multi-agent support is added.
 *
 * @module
 */

import { Descriptions, Typography } from 'antd';
import { useXyncra } from '../../hooks/useXyncra';

export interface AgentDetailProps {
  /** The agent ID to display details for. */
  agentID: string;
}

/**
 * Renders a detail panel for the given agent.
 */
export function AgentDetail({ agentID }: AgentDetailProps): React.JSX.Element {
  const { deviceID } = useXyncra();

  return (
    <div style={{ padding: 16 }}>
      <Typography.Title level={5}>Agent 详情</Typography.Title>
      <Descriptions column={1} size="small">
        <Descriptions.Item label="Agent ID">{agentID}</Descriptions.Item>
        <Descriptions.Item label="Device ID">{deviceID}</Descriptions.Item>
        <Descriptions.Item label="状态">在线</Descriptions.Item>
      </Descriptions>
    </div>
  );
}
