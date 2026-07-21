/**
 * SyncStateStore — key-value data access for client-side synchronization state.
 *
 * Mirrors Go SyncStateStore (pkg/store/sync_state_store.go).
 *
 * Well-known keys:
 *   - local_max_seq: the local maximum processed seq.
 *   - latest_seq: the server-reported latest seq.
 */

import type Dexie from 'dexie';
import type { Transaction } from 'dexie';

import { ErrNotFound } from '../errors';
import type { XyncraDatabase } from './index';
import type { SyncState } from './models';
import { SyncStateKey } from './models';

/**
 * SyncStateStore provides key-value data access for client-side synchronization
 * state tracking (e.g. local_max_seq, latest_seq).
 */
export class SyncStateStore {
  constructor(private readonly db: XyncraDatabase) {}

  /**
   * Retrieves the value for the given key.
   * Throws ErrNotFound if the key does not exist.
   */
  async get(key: string): Promise<string> {
    const state = await this.db.syncStates.get(key);
    if (!state) {
      throw ErrNotFound;
    }
    return state.value;
  }

  /**
   * Performs an UPSERT for the given key-value pair.
   * If the key already exists, the value is updated; otherwise a new record is
   * inserted.
   */
  async set(key: string, value: string): Promise<void> {
    const now = new Date();
    await this.db.syncStates.put({
      key,
      value,
      updated_at: now,
    });
  }

  /**
   * Returns the local_max_seq value. Returns 0 if not set.
   */
  async getLocalMaxSeq(): Promise<number> {
    try {
      const val = await this.get(SyncStateKey.LocalMaxSeq);
      const seq = Number.parseInt(val, 10);
      return Number.isNaN(seq) ? 0 : seq;
    } catch (error) {
      if (error === ErrNotFound) return 0;
      throw error;
    }
  }

  /**
   * Sets the local_max_seq value.
   */
  async setLocalMaxSeq(seq: number): Promise<void> {
    await this.set(SyncStateKey.LocalMaxSeq, String(seq));
  }

  /**
   * Returns the latest_seq value. Returns 0 if not set.
   */
  async getLatestSeq(): Promise<number> {
    try {
      const val = await this.get(SyncStateKey.LatestSeq);
      const seq = Number.parseInt(val, 10);
      return Number.isNaN(seq) ? 0 : seq;
    } catch (error) {
      if (error === ErrNotFound) return 0;
      throw error;
    }
  }

  /**
   * Sets the latest_seq value.
   */
  async setLatestSeq(seq: number): Promise<void> {
    await this.set(SyncStateKey.LatestSeq, String(seq));
  }

  /**
   * Sets local_max_seq within the given transaction.
   */
  async setLocalMaxSeqTx(tx: Transaction, seq: number): Promise<void> {
    const table = tx.table('syncStates') as Dexie.Table<SyncState, string>;
    await table.put({
      key: SyncStateKey.LocalMaxSeq,
      value: String(seq),
      updated_at: new Date(),
    });
  }
}
