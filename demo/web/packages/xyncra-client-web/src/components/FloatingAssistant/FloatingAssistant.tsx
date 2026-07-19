import { useCallback, useState } from 'react';
import { FloatingButton } from './FloatingButton';
import { SidebarPanel } from './SidebarPanel';

export function FloatingAssistant(): React.JSX.Element {
  const [isOpen, setIsOpen] = useState(false);

  const handleOpen = useCallback(() => setIsOpen(true), []);
  const handleClose = useCallback(() => setIsOpen(false), []);

  return (
    <>
      <FloatingButton onClick={handleOpen} visible={!isOpen} />
      <SidebarPanel open={isOpen} onClose={handleClose} />
    </>
  );
}
