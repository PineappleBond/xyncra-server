/**
 * UserUpdateStore — data access operations for the UserUpdate model.
 *
 * Mirrors Go UserUpdateStore (pkg/store/user_update_store.go).
 *
 * UserUpdate does not use soft delete — expired records are hard-deleted
 * during cleanup (D-016).
 */

import type { XyncraDatabase } from './index';
import type { UserUpdate } from './models';

/** Default retention period for user updates: 30 days in milliseconds. */
const DEFAULT_CLEANUP_RETENTION_MS = 30 * 24 * 60 * 60 * 1000;

/**
 * UserUpdateStore provides data access operations for the UserUpdate model.
 */
export class UserUpdateStore {
  constructor(private readonly db: XyncraDatabase) {}

  /**
   * Inserts a batch of user update records for efficient bulk insertion.
   */
  async create(updates: UserUpdate[]): Promise<void> {
    if (updates.length === 0) return;
    await this.db.userUpdates.bulkAdd(updates);
  }

  /**
   * Returns user updates with seq greater than afterSeq,
   * ordered by seq ascending, limited to at most limit rows.
   *
   * Note: Database is already scoped by userID+deviceID, so no user_id filtering needed.
   */
  async listByUser(
    _userID: string,
    afterSeq: number,
    limit: number,
  ): Promise<UserUpdate[]> {
    if (limit <= 0 || limit > 1000) limit = 100;

    // Database is single-user scoped; just filter by seq.
    const updates = await this.db.userUpdates
      .where('seq')
      .above(afterSeq)
      .toArray();

    // Sort by seq ascending.
    updates.sort((a, b) => a.seq - b.seq);

    return updates.slice(0, limit);
  }

  /**
   * Returns user updates with seq in the range (afterSeq, maxSeq]
   * (exclusive start, inclusive end), ordered by seq ascending.
   *
   * Note: Database is already scoped by userID+deviceID, so no user_id filtering needed.
   */
  async listByUserRange(
    _userID: string,
    afterSeq: number,
    maxSeq: number,
  ): Promise<UserUpdate[]> {
    if (maxSeq <= afterSeq) return [];

    // Database is single-user scoped; just filter by seq range.
    const updates = await this.db.userUpdates
      .where('seq')
      .between(afterSeq + 1, maxSeq, true, true)
      .toArray();

    // Sort by seq ascending.
    updates.sort((a, b) => a.seq - b.seq);

    return updates;
  }

  /**
   * Returns the highest seq value. Returns 0 if no update records exist.
   *
   * Note: Database is already scoped by userID+deviceID, so no user_id filtering needed.
   */
  async getLatestSeq(_userID: string): Promise<number> {
    const updates = await this.db.userUpdates.toArray();

    if (updates.length === 0) return 0;
    return Math.max(...updates.map((u) => u.seq));
  }

  /**
   * Hard-deletes all user updates with created_at strictly before the given time.
   * Returns the number of deleted rows.
   */
  async cleanupExpiredBefore(before: Date): Promise<number> {
    const beforeMs = before.getTime();
    const count = await this.db.userUpdates
      .where('created_at')
      .below(before)
      .delete();
    return count;
  }

  /**
   * Hard-deletes all user updates older than DEFAULT_CLEANUP_RETENTION_MS (30 days).
   * Convenience wrapper around cleanupExpiredBefore.
   */
  async cleanupExpired(): Promise<number> {
    const before = new Date(Date.now() - DEFAULT_CLEANUP_RETENTION_MS);
    return this.cleanupExpiredBefore(before);
  }
}
