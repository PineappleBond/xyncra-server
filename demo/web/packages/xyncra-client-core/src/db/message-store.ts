/**
 * MessageStore — data access operations for the Message model.
 *
 * Mirrors Go MessageStore (pkg/store/message_store.go).
 *
 * Key constraints:
 *   C5 — Message uniqueness by composite index (client_message_id, sender_id).
 *   Soft delete — deleted_at field is used instead of hard delete.
 */

import type Dexie from 'dexie';
import type { Transaction } from 'dexie';

import { ErrDuplicateKey, ErrNotFound } from '../errors';
import type { XyncraDatabase } from './index';
import type { Message } from './models';

/** Safe upper bound for message_id range queries (uint32 max). */
const UINT32_MAX = 0xffffffff;

/**
 * MessageStore provides data access operations for the Message model.
 * Mirrors Go MessageStore (pkg/store/message_store.go).
 */
export class MessageStore {
  constructor(private readonly db: XyncraDatabase) {}

  // ---------------------------------------------------------------------------
  // CRUD
  // ---------------------------------------------------------------------------

  /**
   * Inserts a new message record.
   * Throws ErrDuplicateKey on unique constraint violation.
   */
  async create(msg: Message): Promise<void> {
    try {
      await this.db.messages.add(msg);
    } catch (error) {
      if (this.isConstraintError(error)) {
        throw ErrDuplicateKey;
      }
      throw error;
    }
  }

  /**
   * Retrieves a message by its primary key.
   * Returns undefined if not found or soft-deleted.
   */
  async get(id: string): Promise<Message | undefined> {
    const msg = await this.db.messages.get(id);
    if (!msg || msg.deleted_at !== null) {
      return undefined;
    }
    return msg;
  }

  /**
   * Retrieves a message by its client-generated unique ID and sender ID
   * (composite uniqueness, C5).
   * Returns undefined if no matching record exists.
   */
  async getByClientMessageID(
    clientMessageID: string,
    senderID: string,
  ): Promise<Message | undefined> {
    const msg = await this.db.messages
      .where('[client_message_id+sender_id]')
      .equals([clientMessageID, senderID])
      .first();
    if (!msg || msg.deleted_at !== null) {
      return undefined;
    }
    return msg;
  }

  /**
   * Returns messages for the given conversation with message_id greater than
   * afterMessageID, ordered by message_id ascending.
   */
  async listByConversation(
    convID: string,
    afterMessageID: number,
    limit: number,
  ): Promise<Message[]> {
    if (limit <= 0 || limit > 200) limit = 50;

    // Use compound index for efficient range query.
    const lowerBound: [string, number] = [convID, afterMessageID + 1];
    const upperBound: [string, number] = [convID, UINT32_MAX];

    const msgs = await this.db.messages
      .where('[conversation_id+message_id]')
      .between(lowerBound, upperBound, true, true)
      .toArray();

    // Filter out soft-deleted messages.
    const active = msgs.filter((m) => m.deleted_at === null);

    // Sort by message_id ascending (compound index ensures this order, but
    // explicit sort guarantees correctness).
    active.sort((a, b) => a.message_id - b.message_id);

    return active.slice(0, limit);
  }

  /**
   * Returns messages for the given conversation that contain the specified
   * content substring (case-insensitive), ordered by message_id descending
   * (newest first).
   *
   * Note: Dexie does not support LIKE queries. Content filtering is done in JS.
   */
  async searchByConversation(
    convID: string,
    content: string,
    afterMessageID: number,
    limit: number,
  ): Promise<Message[]> {
    if (limit <= 0 || limit > 201) limit = 50;
    if (!content) return [];

    const lowerContent = content.toLowerCase();

    // Fetch all messages for the conversation via index.
    let msgs = await this.db.messages
      .where('conversation_id')
      .equals(convID)
      .toArray();

    // Filter out soft-deleted.
    msgs = msgs.filter((m) => m.deleted_at === null);

    // Apply afterMessageID filter (message_id < afterMessageID for descending order).
    if (afterMessageID > 0) {
      msgs = msgs.filter((m) => m.message_id < afterMessageID);
    }

    // Content substring match (case-insensitive).
    msgs = msgs.filter((m) => m.content.toLowerCase().includes(lowerContent));

    // Sort by message_id descending (newest first).
    msgs.sort((a, b) => b.message_id - a.message_id);

    return msgs.slice(0, limit);
  }

  /**
   * Returns messages for the given conversation within the specified time range
   * (inclusive), ordered by message_id ascending.
   */
  async listByTimeRange(
    convID: string,
    startTime: Date,
    endTime: Date,
    limit: number,
  ): Promise<Message[]> {
    if (limit <= 0 || limit > 200) limit = 50;

    const startMs = startTime.getTime();
    const endMs = endTime.getTime();

    const msgs = await this.db.messages
      .where('conversation_id')
      .equals(convID)
      .toArray();

    const filtered = msgs.filter(
      (m) =>
        m.deleted_at === null &&
        m.created_at.getTime() >= startMs &&
        m.created_at.getTime() <= endMs,
    );

    // Sort by message_id ascending.
    filtered.sort((a, b) => a.message_id - b.message_id);

    return filtered.slice(0, limit);
  }

  /**
   * Performs a soft delete on the message identified by id.
   * Throws ErrNotFound if the message does not exist or is already deleted.
   */
  async delete(id: string): Promise<void> {
    const updated = await this.db.messages
      .where('id')
      .equals(id)
      .modify((msg) => {
        if (msg.deleted_at !== null) return;
        msg.deleted_at = new Date();
      });
    if (updated === 0) {
      throw ErrNotFound;
    }
  }

  /**
   * Undeletes a soft-deleted message.
   * Throws ErrNotFound if the message does not exist or is not soft-deleted.
   */
  async restore(id: string): Promise<void> {
    const updated = await this.db.messages
      .where('id')
      .equals(id)
      .modify((msg) => {
        if (msg.deleted_at === null) return; // Not soft-deleted — no-op but still counts
        msg.deleted_at = null;
      });
    if (updated === 0) {
      throw ErrNotFound;
    }
  }

  /**
   * Performs a soft delete on all messages belonging to the given conversation.
   */
  async deleteByConversation(convID: string): Promise<void> {
    const now = new Date();
    await this.db.messages
      .where('conversation_id')
      .equals(convID)
      .modify((msg) => {
        if (msg.deleted_at === null) {
          msg.deleted_at = now;
        }
      });
  }

  /**
   * Restores all soft-deleted messages belonging to the given conversation.
   * Returns the number of restored rows.
   */
  async restoreByConversation(convID: string): Promise<number> {
    let count = 0;
    await this.db.messages
      .where('conversation_id')
      .equals(convID)
      .modify((msg) => {
        if (msg.deleted_at !== null) {
          msg.deleted_at = null;
          count++;
        }
      });
    return count;
  }

  /**
   * Returns the most recent messages for a conversation, ordered by message_id
   * descending (newest first), limited to at most limit rows.
   * Used by the Agent context manager to load conversation history.
   */
  async listRecentByConversation(
    convID: string,
    limit: number,
  ): Promise<Message[]> {
    if (limit <= 0 || limit > 500) limit = 50;

    const msgs = await this.db.messages
      .where('conversation_id')
      .equals(convID)
      .toArray();

    // Filter out soft-deleted.
    const active = msgs.filter((m) => m.deleted_at === null);

    // Sort by message_id descending.
    active.sort((a, b) => b.message_id - a.message_id);

    return active.slice(0, limit);
  }

  /**
   * Returns the number of messages in the given conversation with message_id
   * greater than afterMessageID. Soft-deleted messages are excluded.
   */
  async countUnread(convID: string, afterMessageID: number): Promise<number> {
    const lowerBound: [string, number] = [convID, afterMessageID + 1];
    const upperBound: [string, number] = [convID, UINT32_MAX];

    const msgs = await this.db.messages
      .where('[conversation_id+message_id]')
      .between(lowerBound, upperBound, true, true)
      .toArray();

    return msgs.filter((m) => m.deleted_at === null).length;
  }

  /**
   * Inserts a message within the given transaction.
   */
  async createTx(tx: Transaction, msg: Message): Promise<void> {
    const table = tx.table('messages') as Dexie.Table<Message, string>;
    try {
      await table.add(msg);
    } catch (error) {
      if (this.isConstraintError(error)) {
        throw ErrDuplicateKey;
      }
      throw error;
    }
  }

  /**
   * Creates the message if it does not exist, or updates it if it does.
   * Uniqueness is determined by the composite index (client_message_id, sender_id).
   * Handles TOCTOU race between SELECT and INSERT.
   */
  async upsert(msg: Message): Promise<void> {
    const existing = await this.getByClientMessageID(
      msg.client_message_id,
      msg.sender_id,
    );
    if (!existing) {
      try {
        await this.db.messages.add(msg);
        return;
      } catch (error) {
        if (this.isConstraintError(error)) {
          // TOCTOU: retry as update by composite key.
          await this.updateByCompositeKey(msg);
          return;
        }
        throw error;
      }
    }
    await this.updateByCompositeKey(msg);
  }

  /**
   * Performs a soft delete within the given transaction.
   */
  async softDeleteTx(tx: Transaction, id: string): Promise<void> {
    const table = tx.table('messages') as Dexie.Table<Message, string>;
    const updated = await table
      .where('id')
      .equals(id)
      .modify((msg) => {
        if (msg.deleted_at !== null) return;
        msg.deleted_at = new Date();
      });
    if (updated === 0) {
      throw ErrNotFound;
    }
  }

  // ---------------------------------------------------------------------------
  // Private helpers
  // ---------------------------------------------------------------------------

  /**
   * Updates a message identified by (client_message_id, sender_id).
   */
  private async updateByCompositeKey(msg: Message): Promise<void> {
    const existing = await this.db.messages
      .where('[client_message_id+sender_id]')
      .equals([msg.client_message_id, msg.sender_id])
      .first();
    if (!existing) {
      throw ErrNotFound;
    }
    await this.db.messages.update(existing.id, {
      conversation_id: msg.conversation_id,
      message_id: msg.message_id,
      content: msg.content,
      type: msg.type,
      reply_to: msg.reply_to,
      status: msg.status,
    });
  }

  /** Checks if an error is a constraint violation. */
  private isConstraintError(error: unknown): boolean {
    if (error instanceof Error) {
      return error.name === 'ConstraintError';
    }
    return false;
  }
}
