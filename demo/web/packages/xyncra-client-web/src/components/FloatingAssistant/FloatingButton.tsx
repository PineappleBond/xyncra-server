/**
 * @packageDocumentation
 * FloatingButton — the collapsed-state circular button.
 *
 * Rendered when the chat window is closed. Clicking opens the full
 * chat window.
 *
 * @module
 */

import { MessageOutlined } from '@ant-design/icons';
import { FLOATING_ASSISTANT_STYLES } from './styles';

export interface FloatingButtonProps {
  /** Called when the user clicks the floating button. */
  onClick: () => void;
}

/**
 * A circular, fixed-position button with a chat icon.
 */
export function FloatingButton({
  onClick,
}: FloatingButtonProps): React.JSX.Element {
  return (
    <div
      style={FLOATING_ASSISTANT_STYLES.floatingButton}
      onClick={onClick}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') onClick();
      }}
    >
      <MessageOutlined style={{ fontSize: 24 }} />
    </div>
  );
}
