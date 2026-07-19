/**
 * @packageDocumentation
 * ConnectionStatus — displays the current WebSocket connection state.
 *
 * Uses antd Badge to show a colored dot + label reflecting the
 * connection lifecycle tracked by XyncraProvider (D-4).
 *
 * @module
 */

import { Badge } from 'antd';
import type { ConnectionStatus as ConnectionStatusType } from '../../context/XyncraProvider';
import { useXyncra } from '../../hooks/useXyncra';

/**
 * Mapping from connection lifecycle status to antd Badge props.
 */
const STATUS_MAP: Record<
  ConnectionStatusType,
  { status: 'processing' | 'success' | 'error'; text: string }
> = {
  connecting: { status: 'processing', text: '连接中...' },
  syncing: { status: 'processing', text: '同步中...' },
  connected: { status: 'success', text: '已连接' },
  disconnected: { status: 'error', text: '未连接' },
};

/**
 * Renders a connection status indicator with a colored badge and label.
 */
export function ConnectionStatus(): React.JSX.Element {
  const { connectionStatus } = useXyncra();
  const { status, text } = STATUS_MAP[connectionStatus];

  return (
    <div style={{ padding: '8px 12px', borderBottom: '1px solid #f0f0f0' }}>
      <Badge status={status} text={text} />
    </div>
  );
}
