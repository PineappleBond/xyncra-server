/**
 * NotificationLogStore — data access operations for push notification logging.
 *
 * Mirrors Go NotificationLogStore (pkg/store/notification_log_store.go).
 *
 * Key constraint:
 *   C6 — NotificationLog.Seq has a unique index for deduplication.
 *        The save() method catches ConstraintError and throws ErrDuplicateKey.
 */

import type Dexie from 'dexie';
import type { Transaction } from 'dexie';

import { ErrDuplicateKey, ErrNotFound } from '../errors';
import type { XyncraDatabase } from './index';
import type { NotificationLog } from './models';

// ---------------------------------------------------------------------------
// Filter types
// ---------------------------------------------------------------------------

/**
 * NotificationLogFilter defines optional filters for listing notification logs.
 * Mirrors Go store.NotificationLogFilter.
 */
export interface NotificationLogFilter {
  start_time?: Date;
  end_time?: Date;
  type?: string;
  limit?: number;
}

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

/**
 * NotificationLogStore provides data access operations for push notification
 * logging and deduplication.
 */
export class NotificationLogStore {
  constructor(private readonly db: XyncraDatabase) {}

  /**
   * Inserts a new notification log record.
   * Throws ErrDuplicateKey if a record with the same seq already exists (C6).
   */
  async save(log: NotificationLog): Promise<void> {
    try {
      await this.db.notificationLogs.add(log);
    } catch (error) {
      if (this.isConstraintError(error)) {
        throw ErrDuplicateKey;
      }
      throw error;
    }
  }

  /**
   * Returns notification logs matching the given filters, ordered by created_at
   * descending (newest first).
   */
  async list(filter: NotificationLogFilter): Promise<NotificationLog[]> {
    let limit = filter.limit ?? 100;
    if (limit <= 0 || limit > 1000) limit = 100;

    let logs: NotificationLog[];

    if (filter.type) {
      logs = await this.db.notificationLogs
        .where('type')
        .equals(filter.type)
        .toArray();
    } else {
      logs = await this.db.notificationLogs.toArray();
    }

    // Apply time range filters in JS.
    if (filter.start_time) {
      const startMs = filter.start_time.getTime();
      logs = logs.filter((l) => l.created_at.getTime() >= startMs);
    }
    if (filter.end_time) {
      const endMs = filter.end_time.getTime();
      logs = logs.filter((l) => l.created_at.getTime() <= endMs);
    }

    // Sort by created_at descending.
    logs.sort(
      (a, b) =>
        new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
    );

    return logs.slice(0, limit);
  }

  /**
   * Returns notification logs with seq in the range [startSeq, endSeq]
   * (inclusive), ordered by seq ascending.
   */
  async listBySeqRange(
    startSeq: number,
    endSeq: number,
  ): Promise<NotificationLog[]> {
    const logs = await this.db.notificationLogs
      .where('seq')
      .between(startSeq, endSeq, true, true)
      .toArray();

    // Sort by seq ascending.
    logs.sort((a, b) => a.seq - b.seq);

    return logs;
  }

  /**
   * Exports notification logs matching the filter as CSV string.
   */
  async exportCSV(filter: NotificationLogFilter): Promise<string> {
    const logs = await this.list(filter);

    const header = 'id,seq,type,created_at';
    const rows = logs.map((l) => {
      return [l.id, String(l.seq), l.type, l.created_at.toISOString()].join(
        ',',
      );
    });

    return [header, ...rows].join('\n');
  }

  /**
   * Exports notification logs matching the filter as JSON string.
   */
  async exportJSON(filter: NotificationLogFilter): Promise<string> {
    const logs = await this.list(filter);
    return JSON.stringify(logs, null, 2);
  }

  /**
   * Hard-deletes notification logs with created_at strictly before the given time.
   * Returns the number of deleted rows.
   */
  async cleanupBefore(before: Date): Promise<number> {
    return this.db.notificationLogs.where('created_at').below(before).delete();
  }

  /**
   * Returns the number of notification logs with created_at strictly before the
   * given time without deleting them.
   */
  async countBefore(before: Date): Promise<number> {
    return this.db.notificationLogs.where('created_at').below(before).count();
  }

  /**
   * Returns the highest seq value in the notification log.
   * Returns 0 if the log is empty.
   */
  async getLatestSeq(): Promise<number> {
    // seq is the primary key; lastKey() returns the highest key without
    // loading all records.
    const maxSeq = await this.db.notificationLogs.orderBy('seq').lastKey();
    return (maxSeq as number) ?? 0;
  }

  /**
   * Inserts a notification log record within the given transaction.
   * Throws ErrDuplicateKey if a record with the same seq already exists (C6).
   */
  async saveTx(tx: Transaction, log: NotificationLog): Promise<void> {
    const table = tx.table('notificationLogs') as Dexie.Table<
      NotificationLog,
      number
    >;
    try {
      await table.add(log);
    } catch (error) {
      if (this.isConstraintError(error)) {
        throw ErrDuplicateKey;
      }
      throw error;
    }
  }

  /** Checks if an error is a constraint violation. */
  private isConstraintError(error: unknown): boolean {
    if (error instanceof Error) {
      return error.name === 'ConstraintError';
    }
    return false;
  }
}
