/**
 * @packageDocumentation
 * Style constants for the FloatingAssistant component family.
 *
 * Design decision D-9: fixed-position div (not Modal/Drawer), 800x600,
 * right-bottom anchored, three-column flexbox layout.
 *
 * @module
 */

import type { CSSProperties } from 'react';

/**
 * All inline styles used by the FloatingAssistant components.
 *
 * Using plain objects rather than CSS Modules keeps the component
 * self-contained and portable across host applications.
 */
export const FLOATING_ASSISTANT_STYLES: Record<string, CSSProperties> = {
  // -- Top-level container (fixed, bottom-right) --
  container: {
    position: 'fixed',
    bottom: 24,
    right: 24,
    zIndex: 1000,
  },

  // -- Expanded chat window (800x600, three-column flex) --
  chatWindow: {
    width: 800,
    height: 600,
    backgroundColor: '#fff',
    borderRadius: 12,
    boxShadow: '0 8px 24px rgba(0, 0, 0, 0.15)',
    display: 'flex',
    flexDirection: 'row',
    overflow: 'hidden',
  },

  // -- Left column: Agent selector (200px) --
  agentSelector: {
    width: 200,
    borderRight: '1px solid #f0f0f0',
    display: 'flex',
    flexDirection: 'column',
  },

  // -- Middle column: Conversation list (240px) --
  conversationList: {
    width: 240,
    borderRight: '1px solid #f0f0f0',
    display: 'flex',
    flexDirection: 'column',
  },

  // -- Right column: Message area (flex: 1) --
  messageArea: {
    flex: 1,
    display: 'flex',
    flexDirection: 'column',
  },

  // -- Collapsed floating button (56x56 circle) --
  floatingButton: {
    width: 56,
    height: 56,
    borderRadius: '50%',
    backgroundColor: '#1890ff',
    color: '#fff',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    cursor: 'pointer',
    boxShadow: '0 4px 12px rgba(24, 144, 255, 0.4)',
    transition: 'all 0.3s',
  },
};
