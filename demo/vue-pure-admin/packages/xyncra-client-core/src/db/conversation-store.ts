/**
 * ConversationStore — data access operations for the Conversation model.
 *
 * Mirrors Go ConversationStore (pkg/store/conversation_store.go).
 *
 * Key constraints:
 *   C4 — UpdateLastRead uses MAX semantics (only advances forward, never backward).
 *   Soft delete — deleted_at field is used instead of hard delete.
 *   Cascade — Delete/Restore cascade to messages.
 */

import type Dexie from 'dexie';
import type { Transaction } from 'dexie';

import { ErrDuplicateKey, ErrNotFound } from '../errors';
import type { XyncraDatabase } from './index';
import type { Conversation, Message } from './models';

/**
 * ConversationStore provides data access operations for the Conversation model.
 * Mirrors Go ConversationStore (pkg/store/conversation_store.go).
 */
export class ConversationStore {
  constructor(private readonly db: XyncraDatabase) {}

  // ---------------------------------------------------------------------------
  // CRUD
  // ---------------------------------------------------------------------------

  /**
   * Inserts a new conversation record.
   * Throws ErrDuplicateKey if a conversation with the same id already exists.
   */
  async create(conv: Conversation): Promise<void> {
    try {
      await this.db.conversations.add(conv);
    } catch (error) {
      if (this.isConstraintError(error)) {
        throw ErrDuplicateKey;
      }
      throw error;
    }
  }

  /**
   * Retrieves a conversation by its primary key.
   * Returns undefined if not found or soft-deleted.
   */
  async get(id: string): Promise<Conversation | undefined> {
    const conv = await this.db.conversations.get(id);
    // Use != null to match both null and undefined (deleted_at may not be initialized)
    if (!conv || conv.deleted_at != null) {
      return undefined;
    }
    return conv;
  }

  /**
   * Retrieves a conversation including soft-deleted records.
   * Returns undefined if not found at all.
   */
  async getUnscoped(id: string): Promise<Conversation | undefined> {
    return this.db.conversations.get(id);
  }

  /**
   * Returns the 1-on-1 conversation between user1 and user2.
   * Checks both (user1, user2) and (user2, user1) orderings.
   * Returns undefined if no matching conversation exists.
   */
  async getByUsers(
    user1: string,
    user2: string,
  ): Promise<Conversation | undefined> {
    // Try both orderings using the compound unique index.
    let conv = await this.db.conversations
      .where('[user_id1+user_id2]')
      .equals([user1, user2])
      .first();
    if (conv && conv.deleted_at == null) return conv;

    conv = await this.db.conversations
      .where('[user_id1+user_id2]')
      .equals([user2, user1])
      .first();
    if (conv && conv.deleted_at == null) return conv;

    return undefined;
  }

  /**
   * Returns conversations where the given user is either UserID1 or UserID2,
   * ordered by last_message_at descending, with offset/limit pagination.
   * Soft-deleted records are excluded.
   */
  async getByUser(
    userID: string,
    offset: number,
    limit: number,
  ): Promise<Conversation[]> {
    // Database is already scoped by userID+deviceID (xyncra-{userID}-{deviceID}),
    // so we can return all non-deleted conversations without filtering by user_id.
    if (limit <= 0 || limit > 101) limit = 20;
    if (offset < 0) offset = 0;

    // Get all conversations, filter out soft-deleted ones
    // Use == null to match both null and undefined (deleted_at may not be initialized)
    const all = await this.db.conversations
      .filter((conv) => conv.deleted_at == null && !!conv.user_id2)
      .toArray();

    // Sort by last_message_at descending.
    all.sort(
      (a, b) =>
        new Date(b.last_message_at).getTime() -
        new Date(a.last_message_at).getTime(),
    );

    return all.slice(offset, offset + limit);
  }

  /**
   * Saves all fields of the conversation back to the database.
   * Uses put() which handles both insert and update.
   */
  async update(conv: Conversation): Promise<void> {
    await this.db.conversations.put(conv);
  }

  /**
   * Creates the conversation if it does not exist, or saves (overwrites) it if
   * it already exists. Used by the client sync pipeline to apply conversation
   * create events idempotently (D-045).
   *
   * Uses Unscoped() semantics: also finds soft-deleted records, so that
   * restoring a previously deleted conversation correctly transitions it back
   * to active. If a concurrent insert causes a duplicate key error, retries as
   * an update to handle the TOCTOU race.
   */
  async upsert(conv: Conversation): Promise<void> {
    const existing = await this.db.conversations.get(conv.id);
    if (!existing) {
      try {
        await this.db.conversations.add(conv);
        return;
      } catch (error) {
        if (this.isConstraintError(error)) {
          // TOCTOU: another task inserted between our get() and add().
          await this.db.conversations.put(conv);
          return;
        }
        throw error;
      }
    }
    // Record exists (possibly soft-deleted) — overwrite.
    await this.db.conversations.put(conv);
  }

  /**
   * Performs a cascading soft delete: the conversation and all its messages
   * are soft-deleted within a single transaction (D-013).
   * Throws ErrNotFound if the conversation does not exist (or is already deleted).
   */
  async delete(id: string): Promise<void> {
    await this.db.transaction(
      'rw',
      [this.db.conversations, this.db.messages],
      async () => {
        const conv = await this.db.conversations.get(id);
        if (!conv || conv.deleted_at != null) {
          throw ErrNotFound;
        }

        const now = new Date();
        await this.db.conversations.update(id, { deleted_at: now });

        // Cascade soft-delete all messages in this conversation (D-013).
        await this.db.messages
          .where('conversation_id')
          .equals(id)
          .modify((msg) => {
            if (msg.deleted_at == null) {
              msg.deleted_at = now;
            }
          });
      },
    );
  }

  /**
   * Undeletes a soft-deleted conversation and cascades the restore to all its
   * messages within a single transaction (D-015).
   *
   * Idempotent: calling on a non-deleted conversation is a no-op.
   * Throws ErrNotFound if the conversation does not exist at all.
   */
  async restore(id: string): Promise<void> {
    await this.db.transaction(
      'rw',
      [this.db.conversations, this.db.messages],
      async () => {
        const conv = await this.db.conversations.get(id);
        if (!conv) {
          throw ErrNotFound;
        }

        // Restore the conversation if it was soft-deleted.
        if (conv.deleted_at != null) {
          await this.db.conversations.update(id, { deleted_at: null });

          // Cascade restore all messages in this conversation (D-015).
          await this.db.messages
            .where('conversation_id')
            .equals(id)
            .modify((msg) => {
              if (msg.deleted_at != null) {
                msg.deleted_at = null;
              }
            });
        }
      },
    );
  }

  /**
   * Updates the last_message_at and last_processed_message_id fields.
   * Throws ErrNotFound if the conversation does not exist.
   */
  async updateLastMessage(
    convID: string,
    lastMessageAt: Date,
    lastProcessedMessageID: number,
  ): Promise<void> {
    const updated = await this.db.conversations
      .where('id')
      .equals(convID)
      .modify((conv) => {
        conv.last_message_at = lastMessageAt;
        conv.last_processed_message_id = lastProcessedMessageID;
      });
    if (updated === 0) {
      throw ErrNotFound;
    }
  }

  /**
   * Updates the last-read message ID for the specified user.
   * Uses MAX semantics: only advances forward, never backward (C4, D-012).
   *
   * Throws ErrNotFound if the conversation does not exist or the user does
   * not belong to it.
   */
  async updateLastRead(
    convID: string,
    userID: string,
    messageID: number,
  ): Promise<void> {
    await this.db.transaction('rw', this.db.conversations, async () => {
      const conv = await this.db.conversations.get(convID);
      if (!conv) {
        throw ErrNotFound;
      }
      if (conv.user_id1 !== userID && conv.user_id2 !== userID) {
        throw ErrNotFound;
      }

      // MAX semantics: only advance forward.
      const updates: Partial<Conversation> = {};
      if (conv.user_id1 === userID) {
        if (messageID > conv.last_read_message_id1) {
          updates.last_read_message_id1 = messageID;
        }
      }
      if (conv.user_id2 === userID) {
        if (messageID > conv.last_read_message_id2) {
          updates.last_read_message_id2 = messageID;
        }
      }

      if (Object.keys(updates).length > 0) {
        await this.db.conversations.update(convID, updates);
      }
    });
  }

  /**
   * Searches conversations that contain the specified title substring
   * (case-insensitive), ordered by last_message_at descending.
   *
   * Note: Dexie does not support LIKE queries. We perform case-insensitive
   * substring matching in JavaScript after fetching candidates.
   *
   * Note: Database is already scoped by userID+deviceID, so no user_id filtering needed.
   */
  async searchByTitle(
    _userID: string,
    title: string,
    limit: number,
  ): Promise<Conversation[]> {
    if (limit <= 0 || limit > 101) limit = 20;
    if (!title) return [];

    const lowerTitle = title.toLowerCase();

    // Database is single-user scoped; just filter by title.
    const matches = await this.db.conversations
      .filter((conv) => conv.deleted_at == null && conv.title.toLowerCase().includes(lowerTitle))
      .toArray();

    // Sort by last_message_at descending.
    matches.sort(
      (a, b) =>
        new Date(b.last_message_at).getTime() -
        new Date(a.last_message_at).getTime(),
    );

    return matches.slice(0, limit);
  }

  // ---------------------------------------------------------------------------
  // Transaction variants (Tx)
  // ---------------------------------------------------------------------------

  /**
   * Updates last message fields within the given transaction.
   */
  async updateLastMessageTx(
    tx: Transaction,
    convID: string,
    lastMessageAt: Date,
    lastProcessedMessageID: number,
  ): Promise<void> {
    const table = tx.table('conversations') as Dexie.Table<
      Conversation,
      string
    >;
    const updated = await table
      .where('id')
      .equals(convID)
      .modify((conv) => {
        conv.last_message_at = lastMessageAt;
        conv.last_processed_message_id = lastProcessedMessageID;
      });
    if (updated === 0) {
      throw ErrNotFound;
    }
  }

  /**
   * Updates read cursor within the given transaction.
   * Uses MAX semantics: only advances forward (C4, D-012).
   */
  async updateLastReadTx(
    tx: Transaction,
    convID: string,
    userID: string,
    messageID: number,
  ): Promise<void> {
    const table = tx.table('conversations') as Dexie.Table<
      Conversation,
      string
    >;
    const conv = await table.get(convID);
    if (!conv) {
      throw ErrNotFound;
    }
    if (conv.user_id1 !== userID && conv.user_id2 !== userID) {
      throw ErrNotFound;
    }

    const updates: Partial<Conversation> = {};
    if (conv.user_id1 === userID) {
      if (messageID > conv.last_read_message_id1) {
        updates.last_read_message_id1 = messageID;
      }
    }
    if (conv.user_id2 === userID) {
      if (messageID > conv.last_read_message_id2) {
        updates.last_read_message_id2 = messageID;
      }
    }

    if (Object.keys(updates).length > 0) {
      await table.update(convID, updates);
    }
  }

  /**
   * Creates or updates a conversation within the given transaction.
   * Uses Unscoped() semantics (finds soft-deleted records too).
   */
  async upsertTx(tx: Transaction, conv: Conversation): Promise<void> {
    const table = tx.table('conversations') as Dexie.Table<
      Conversation,
      string
    >;
    const existing = await table.get(conv.id);
    if (!existing) {
      try {
        await table.add(conv);
        return;
      } catch (error) {
        if (this.isConstraintError(error)) {
          await table.put(conv);
          return;
        }
        throw error;
      }
    }
    await table.put(conv);
  }

  /**
   * Performs cascading soft delete within the given transaction (D-013).
   */
  async softDeleteTx(tx: Transaction, id: string): Promise<void> {
    const convTable = tx.table('conversations') as Dexie.Table<
      Conversation,
      string
    >;
    const msgTable = tx.table('messages') as Dexie.Table<Message, string>;

    const conv = await convTable.get(id);
    if (!conv || conv.deleted_at != null) {
      throw ErrNotFound;
    }

    const now = new Date();
    await convTable.update(id, { deleted_at: now });

    // Cascade soft-delete messages (D-013).
    await msgTable
      .where('conversation_id')
      .equals(id)
      .modify((msg) => {
        if (msg.deleted_at == null) {
          msg.deleted_at = now;
        }
      });
  }

  /**
   * Performs cascading restore within the given transaction (D-015).
   */
  async restoreTx(tx: Transaction, id: string): Promise<void> {
    const convTable = tx.table('conversations') as Dexie.Table<
      Conversation,
      string
    >;
    const msgTable = tx.table('messages') as Dexie.Table<Message, string>;

    const conv = await convTable.get(id);
    if (!conv) {
      throw ErrNotFound;
    }

    if (conv.deleted_at != null) {
      await convTable.update(id, { deleted_at: null });

      // Cascade restore messages (D-015).
      await msgTable
        .where('conversation_id')
        .equals(id)
        .modify((msg) => {
          if (msg.deleted_at != null) {
            msg.deleted_at = null;
          }
        });
    }
  }

  // ---------------------------------------------------------------------------
  // Private helpers
  // ---------------------------------------------------------------------------

  /**
   * Checks if an error is a constraint violation (unique index conflict).
   * Dexie throws ConstraintError for unique index violations.
   */
  private isConstraintError(error: unknown): boolean {
    if (error instanceof Error) {
      return error.name === 'ConstraintError';
    }
    return false;
  }
}
