/**
 * @packageDocumentation
 * FunctionCallDisplay — visualizes agent function calls and their results.
 *
 * Renders a compact card showing the function name, serialized arguments,
 * and the execution result (or error). Used as an auxiliary display
 * alongside message bubbles when function call data is available.
 *
 * @module
 */

import { CodeOutlined } from '@ant-design/icons';
import { Tag, Typography } from 'antd';

/**
 * A function invocation request from the agent.
 */
export interface FunctionCall {
  name: string;
  arguments: Record<string, unknown>;
}

/**
 * The result of a function invocation.
 */
export interface FunctionResult {
  name: string;
  result: unknown;
  error?: string;
}

export interface FunctionCallDisplayProps {
  /** The function call request, if available. */
  functionCall?: FunctionCall;
  /** The function call result, if available. */
  functionResult?: FunctionResult;
}

/**
 * Renders a function call and/or its result in a compact card layout.
 * Returns null if neither functionCall nor functionResult is provided.
 */
export function FunctionCallDisplay({
  functionCall,
  functionResult,
}: FunctionCallDisplayProps): React.JSX.Element | null {
  if (!functionCall && !functionResult) return null;

  return (
    <div
      style={{
        padding: '8px 12px',
        backgroundColor: '#fafafa',
        borderRadius: 6,
        marginTop: 8,
      }}
    >
      {functionCall && (
        <div>
          <Tag icon={<CodeOutlined />} color="blue">
            调用函数: {functionCall.name}
          </Tag>
          <Typography.Text
            code
            style={{ fontSize: 12, marginTop: 4, display: 'block' }}
          >
            {JSON.stringify(functionCall.arguments, null, 2)}
          </Typography.Text>
        </div>
      )}
      {functionResult && (
        <div style={{ marginTop: 8 }}>
          <Tag color={functionResult.error ? 'red' : 'green'}>
            {functionResult.error ? '执行失败' : '执行结果'}
          </Tag>
          <Typography.Text
            code
            style={{
              fontSize: 12,
              marginTop: 4,
              display: 'block',
              color: functionResult.error ? '#ff4d4f' : undefined,
            }}
          >
            {functionResult.error ||
              JSON.stringify(functionResult.result, null, 2)}
          </Typography.Text>
        </div>
      )}
    </div>
  );
}
