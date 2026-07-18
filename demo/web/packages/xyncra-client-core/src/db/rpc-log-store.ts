/**
 * RPCLogStore — data access operations for RPC call logging.
 *
 * Mirrors Go RPCLogStore (pkg/store/rpc_log_store.go).
 *
 * Includes aggregate query methods (Aggregate, AggregateByInterval) that
 * perform in-JS computation since IndexedDB does not support SQL aggregation.
 */

import { ErrNotFound } from '../errors';
import type { XyncraDatabase } from './index';
import type { RPCLog } from './models';

// ---------------------------------------------------------------------------
// Filter types
// ---------------------------------------------------------------------------

/**
 * RPCLogFilter defines optional filters for listing RPC logs.
 * Mirrors Go store.RPCLogFilter.
 */
export interface RPCLogFilter {
  start_time?: Date;
  end_time?: Date;
  method?: string;
  status_code?: number;
  /** Filters logs where status_code < value. */
  status_code_less_than?: number;
  conversation_id?: string;
  limit?: number;
}

/**
 * RPCAggregateRow represents a single row in an aggregate report.
 * Mirrors Go store.RPCAggregateRow.
 */
export interface RPCAggregateRow {
  method: string;
  count: number;
  success: number;
  error_count: number;
  avg_ms: number;
}

/**
 * RPCIntervalRow represents aggregated RPC log statistics for a time interval.
 * Mirrors Go store.RPCIntervalRow.
 */
export interface RPCIntervalRow {
  interval: string; // e.g. "2026-07-09 10:00"
  method: string;
  count: number;
  success: number;
  error_count: number;
  avg_ms: number;
}

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

/**
 * RPCLogStore provides data access operations for RPC call logging.
 */
export class RPCLogStore {
  constructor(private readonly db: XyncraDatabase) {}

  /**
   * Inserts a new RPC log record.
   */
  async save(log: RPCLog): Promise<void> {
    await this.db.rpcLogs.add(log);
  }

  /**
   * Updates an existing RPC log record (e.g. after receiving the response).
   */
  async update(log: RPCLog): Promise<void> {
    await this.db.rpcLogs.put(log);
  }

  /**
   * Returns RPC logs matching the given filters, ordered by created_at
   * descending (newest first).
   */
  async list(filter: RPCLogFilter): Promise<RPCLog[]> {
    let limit = filter.limit ?? 100;
    if (limit <= 0 || limit > 1000) limit = 100;

    // Start with the broadest indexed query possible, then filter in JS.
    let logs: RPCLog[];

    if (filter.conversation_id) {
      logs = await this.db.rpcLogs
        .where('conversation_id')
        .equals(filter.conversation_id)
        .toArray();
    } else if (filter.method) {
      logs = await this.db.rpcLogs
        .where('method')
        .equals(filter.method)
        .toArray();
    } else if (filter.status_code !== undefined) {
      logs = await this.db.rpcLogs
        .where('status_code')
        .equals(filter.status_code)
        .toArray();
    } else {
      logs = await this.db.rpcLogs.toArray();
    }

    // Apply remaining filters in JS.
    if (filter.start_time) {
      const startMs = filter.start_time.getTime();
      logs = logs.filter((l) => l.created_at.getTime() >= startMs);
    }
    if (filter.end_time) {
      const endMs = filter.end_time.getTime();
      logs = logs.filter((l) => l.created_at.getTime() <= endMs);
    }
    if (filter.method && filter.conversation_id) {
      // Both were not used as primary index.
      logs = logs.filter((l) => l.method === filter.method);
    }
    if (
      filter.status_code !== undefined &&
      !filter.method &&
      !filter.conversation_id
    ) {
      // Already filtered by index, but apply others.
    }
    if (filter.status_code_less_than !== undefined) {
      logs = logs.filter((l) => l.status_code < filter.status_code_less_than!);
    }

    // Sort by created_at descending.
    logs.sort(
      (a, b) =>
        new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
    );

    return logs.slice(0, limit);
  }

  /**
   * Retrieves an RPC log by its request ID.
   * Throws ErrNotFound if no matching record exists.
   */
  async getByRequestID(requestID: string): Promise<RPCLog> {
    const log = await this.db.rpcLogs
      .where('request_id')
      .equals(requestID)
      .first();
    if (!log) {
      throw ErrNotFound;
    }
    return log;
  }

  /**
   * Returns per-method RPC statistics for the given time range.
   * Computed in JS since IndexedDB does not support SQL aggregation.
   */
  async aggregate(startTime: Date, endTime: Date): Promise<RPCAggregateRow[]> {
    const startMs = startTime.getTime();
    const endMs = endTime.getTime();

    const logs = await this.db.rpcLogs.toArray();

    // Filter by time range.
    const filtered = logs.filter(
      (l) =>
        l.created_at.getTime() >= startMs && l.created_at.getTime() < endMs,
    );

    // Group by method.
    const groups = new Map<
      string,
      { count: number; success: number; error_count: number; totalMs: number }
    >();

    for (const log of filtered) {
      let g = groups.get(log.method);
      if (!g) {
        g = { count: 0, success: 0, error_count: 0, totalMs: 0 };
        groups.set(log.method, g);
      }
      g.count++;
      if (log.status_code >= 0) {
        g.success++;
      } else {
        g.error_count++;
      }
      g.totalMs += log.duration;
    }

    const rows: RPCAggregateRow[] = [];
    for (const [method, g] of groups) {
      rows.push({
        method,
        count: g.count,
        success: g.success,
        error_count: g.error_count,
        avg_ms: g.count > 0 ? g.totalMs / g.count : 0,
      });
    }

    return rows;
  }

  /**
   * Returns per-interval, per-method RPC statistics for the given time range
   * [startTime, endTime). Supported intervals: "1m", "5m", "15m", "1h", "1d".
   * Results are ordered by interval ASC, method ASC.
   */
  async aggregateByInterval(
    startTime: Date,
    endTime: Date,
    interval: string,
  ): Promise<RPCIntervalRow[]> {
    const bucketFn = getIntervalBucketFn(interval);
    const startMs = startTime.getTime();
    const endMs = endTime.getTime();

    const logs = await this.db.rpcLogs.toArray();

    // Filter by time range.
    const filtered = logs.filter(
      (l) =>
        l.created_at.getTime() >= startMs && l.created_at.getTime() < endMs,
    );

    // Group by (interval_bucket, method).
    type GroupKey = string; // "bucket|method"
    const groups = new Map<
      GroupKey,
      {
        interval: string;
        method: string;
        count: number;
        success: number;
        error_count: number;
        totalMs: number;
      }
    >();

    for (const log of filtered) {
      const bucket = bucketFn(log.created_at);
      const key = `${bucket}|${log.method}`;
      let g = groups.get(key);
      if (!g) {
        g = {
          interval: bucket,
          method: log.method,
          count: 0,
          success: 0,
          error_count: 0,
          totalMs: 0,
        };
        groups.set(key, g);
      }
      g.count++;
      if (log.status_code >= 0) {
        g.success++;
      } else {
        g.error_count++;
      }
      g.totalMs += log.duration;
    }

    const rows: RPCIntervalRow[] = [];
    for (const g of groups.values()) {
      rows.push({
        interval: g.interval,
        method: g.method,
        count: g.count,
        success: g.success,
        error_count: g.error_count,
        avg_ms: g.count > 0 ? g.totalMs / g.count : 0,
      });
    }

    // Sort by interval ASC, method ASC.
    rows.sort((a, b) => {
      if (a.interval < b.interval) return -1;
      if (a.interval > b.interval) return 1;
      return a.method < b.method ? -1 : a.method > b.method ? 1 : 0;
    });

    return rows;
  }

  /**
   * Exports RPC logs matching the filter as CSV string.
   */
  async exportCSV(filter: RPCLogFilter): Promise<string> {
    const logs = await this.list(filter);

    const header =
      'id,request_id,method,status_code,conversation_id,duration_ms,error,created_at';
    const rows = logs.map((l) => {
      const durationMs = l.duration.toFixed(3);
      const createdAt = l.created_at.toISOString();
      // Escape CSV fields containing commas or quotes.
      const escapeField = (s: string) =>
        s.includes(',') || s.includes('"') || s.includes('\n')
          ? `"${s.replace(/"/g, '""')}"`
          : s;
      return [
        l.id,
        l.request_id,
        l.method,
        String(l.status_code),
        l.conversation_id,
        durationMs,
        escapeField(l.error_msg),
        createdAt,
      ].join(',');
    });

    return [header, ...rows].join('\n');
  }

  /**
   * Exports RPC logs matching the filter as JSON string.
   */
  async exportJSON(filter: RPCLogFilter): Promise<string> {
    const logs = await this.list(filter);
    return JSON.stringify(logs, null, 2);
  }

  /**
   * Hard-deletes RPC logs with created_at strictly before the given time.
   * Returns the number of deleted rows.
   */
  async cleanupBefore(before: Date): Promise<number> {
    return this.db.rpcLogs.where('created_at').below(before).delete();
  }

  /**
   * Hard-deletes RPC logs older than the given duration (in milliseconds).
   */
  async cleanupOlderThan(retentionMs: number): Promise<number> {
    const before = new Date(Date.now() - retentionMs);
    return this.cleanupBefore(before);
  }

  /**
   * Returns the number of RPC logs with created_at strictly before the given
   * time without deleting them.
   */
  async countBefore(before: Date): Promise<number> {
    return this.db.rpcLogs.where('created_at').below(before).count();
  }
}

// ---------------------------------------------------------------------------
// Interval bucket helpers
// ---------------------------------------------------------------------------

/**
 * Returns a function that maps a Date to an interval bucket string.
 */
function getIntervalBucketFn(interval: string): (date: Date) => string {
  switch (interval) {
    case '1m':
      return (d) => {
        const pad = (n: number) => String(n).padStart(2, '0');
        return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())} ${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}:00`;
      };
    case '5m':
      return (d) => {
        const minutes = Math.floor(d.getUTCMinutes() / 5) * 5;
        const pad = (n: number) => String(n).padStart(2, '0');
        return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())} ${pad(d.getUTCHours())}:${pad(minutes)}:00`;
      };
    case '15m':
      return (d) => {
        const minutes = Math.floor(d.getUTCMinutes() / 15) * 15;
        const pad = (n: number) => String(n).padStart(2, '0');
        return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())} ${pad(d.getUTCHours())}:${pad(minutes)}:00`;
      };
    case '1h':
      return (d) => {
        const pad = (n: number) => String(n).padStart(2, '0');
        return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())} ${pad(d.getUTCHours())}:00:00`;
      };
    case '1d':
      return (d) => {
        const pad = (n: number) => String(n).padStart(2, '0');
        return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}`;
      };
    default:
      throw new Error(
        `store: invalid interval "${interval}": must be one of 1m, 5m, 15m, 1h, 1d`,
      );
  }
}
