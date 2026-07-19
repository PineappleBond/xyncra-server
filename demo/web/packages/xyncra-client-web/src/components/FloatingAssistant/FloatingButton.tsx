import { RobotOutlined } from '@ant-design/icons';
import { useState } from 'react';
import { FLOATING_ASSISTANT_STYLES } from './styles';

export interface FloatingButtonProps {
  onClick: () => void;
  visible: boolean;
}

export function FloatingButton({ onClick, visible }: FloatingButtonProps): React.JSX.Element {
  const [hovered, setHovered] = useState(false);

  if (!visible) {
    return <></>;
  }

  return (
    <button
      style={{
        ...FLOATING_ASSISTANT_STYLES.floatingButton,
        ...(hovered ? FLOATING_ASSISTANT_STYLES.floatingButtonHover : {}),
      }}
      onClick={onClick}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      aria-label="打开 AI 助手"
    >
      <RobotOutlined style={{ fontSize: 24 }} />
    </button>
  );
}
