/**
 * @packageDocumentation
 * Agent user identification utilities.
 *
 * Mirrors Go pkg/client/agent.go.
 */

/**
 * AGENT_USER_ID_PREFIX is the reserved prefix for agent user IDs (D-054).
 * Matches Go's AgentUserIDPrefix = "agent/".
 */
export const AGENT_USER_ID_PREFIX = 'agent/';

/**
 * Returns true if the given userID belongs to an agent.
 * Checks the "agent/" prefix convention (D-054).
 *
 * Mirrors Go IsAgentUser().
 */
export function isAgentUser(userId: string): boolean {
  return userId.startsWith(AGENT_USER_ID_PREFIX);
}
