/**
 * RemoteCallingStore — client-side RemoteCalling persistence (D-137).
 *
 * Mirrors Go RemoteCallingStore (pkg/store/remote_calling_store.go).
 */

import type Dexie from 'dexie';
import type { Transaction } from 'dexie';

import type { XyncraDatabase } from './index';
import type { RemoteCalling } from './models';

/**
 * RemoteCallingStore provides client-side RemoteCalling persistence (D-137).
 */
export class RemoteCallingStore {
  constructor(private readonly db: XyncraDatabase) {}

  /**
   * Creates or updates a remote calling (idempotent by id).
   */
  async upsert(rc: RemoteCalling): Promise<void> {
    await this.db.remoteCallings.put(rc);
  }

  /**
   * Returns all remote callings for a conversation, ordered by creation time ascending.
   */
  async getByConversation(convID: string): Promise<RemoteCalling[]> {
    const rcs = await this.db.remoteCallings
      .where('conversation_id')
      .equals(convID)
      .toArray();

    // Sort by created_at ascending.
    rcs.sort(
      (a, b) =>
        new Date(a.created_at).getTime() - new Date(b.created_at).getTime(),
    );

    return rcs;
  }

  /**
   * Returns all pending remote callings for a conversation.
   */
  async getPendingByConversation(convID: string): Promise<RemoteCalling[]> {
    const rcs = await this.db.remoteCallings
      .where('[conversation_id+status]')
      .equals([convID, 'pending'])
      .toArray();

    // Sort by created_at ascending.
    rcs.sort(
      (a, b) =>
        new Date(a.created_at).getTime() - new Date(b.created_at).getTime(),
    );

    return rcs;
  }

  /**
   * Removes all remote callings for a conversation.
   */
  async deleteByConversation(convID: string): Promise<void> {
    await this.db.remoteCallings
      .where('conversation_id')
      .equals(convID)
      .delete();
  }

  /**
   * Removes all remote callings for a conversation within the given transaction.
   */
  async deleteByConversationTx(tx: Transaction, convID: string): Promise<void> {
    const table = tx.table('remoteCallings') as Dexie.Table<
      RemoteCalling,
      string
    >;
    await table.where('conversation_id').equals(convID).delete();
  }
}
