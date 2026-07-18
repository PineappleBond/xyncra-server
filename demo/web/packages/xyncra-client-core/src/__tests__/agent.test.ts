/**
 * Agent utility tests.
 *
 * Tests cover:
 *   - C16: Agent userID prefix "agent/"
 */

import { AGENT_USER_ID_PREFIX, isAgentUser } from '../agent';

describe('Agent', () => {
  test('C16: AGENT_USER_ID_PREFIX is "agent/"', () => {
    expect(AGENT_USER_ID_PREFIX).toBe('agent/');
  });

  test('C16: isAgentUser returns true for agent/ prefix', () => {
    expect(isAgentUser('agent/claude')).toBe(true);
    expect(isAgentUser('agent/gpt-4')).toBe(true);
    expect(isAgentUser('agent/')).toBe(true);
  });

  test('C16: isAgentUser returns false for non-agent users', () => {
    expect(isAgentUser('user1')).toBe(false);
    expect(isAgentUser('admin')).toBe(false);
    expect(isAgentUser('')).toBe(false);
    expect(isAgentUser('agents')).toBe(false);
    expect(isAgentUser('Agent/test')).toBe(false); // case sensitive
  });
});
