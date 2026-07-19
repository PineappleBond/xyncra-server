import { SidebarPanel } from './SidebarPanel';

export interface ChatWindowProps {
  onClose: () => void;
}

export function ChatWindow({ onClose }: ChatWindowProps): React.JSX.Element {
  return <SidebarPanel open onClose={onClose} />;
}
