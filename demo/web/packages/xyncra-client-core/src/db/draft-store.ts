/**
 * DraftStore — data access operations for message drafts.
 *
 * Mirrors Go DraftStore (pkg/store/draft_store.go).
 *
 * Each conversation can have at most one draft (conversation_id is uniqueIndex).
 */

import { ErrNotFound } from '../errors';
import type { XyncraDatabase } from './index';
import type { Draft } from './models';

/**
 * DraftStore provides data access operations for message drafts.
 * Each conversation can have at most one draft (one-draft-per-conversation).
 */
export class DraftStore {
  constructor(private readonly db: XyncraDatabase) {}

  /**
   * Performs an UPSERT for a draft. If a draft for the conversation already
   * exists (by conversation_id uniqueIndex), it is updated; otherwise a new
   * record is inserted.
   */
  async save(draft: Draft): Promise<void> {
    // Check for existing draft by conversation_id.
    const existing = await this.db.drafts
      .where('conversation_id')
      .equals(draft.conversation_id)
      .first();

    if (existing) {
      // Update existing draft.
      await this.db.drafts.update(existing.id, {
        content: draft.content,
        updated_at: new Date(),
      });
    } else {
      await this.db.drafts.add(draft);
    }
  }

  /**
   * Retrieves the draft for the given conversation.
   * Throws ErrNotFound if no draft exists.
   */
  async getByConversation(conversationID: string): Promise<Draft> {
    const draft = await this.db.drafts
      .where('conversation_id')
      .equals(conversationID)
      .first();
    if (!draft) {
      throw ErrNotFound;
    }
    return draft;
  }

  /**
   * Removes a draft by its primary key.
   * Throws ErrNotFound if not found.
   */
  async delete(id: string): Promise<void> {
    const existing = await this.db.drafts.get(id);
    if (!existing) {
      throw ErrNotFound;
    }
    await this.db.drafts.delete(id);
  }

  /**
   * Removes the draft for the given conversation.
   * Throws ErrNotFound if no draft exists.
   */
  async deleteByConversation(conversationID: string): Promise<void> {
    const draft = await this.db.drafts
      .where('conversation_id')
      .equals(conversationID)
      .first();
    if (!draft) {
      throw ErrNotFound;
    }
    await this.db.drafts.delete(draft.id);
  }

  /**
   * Returns all drafts ordered by updated_at descending.
   */
  async list(): Promise<Draft[]> {
    const drafts = await this.db.drafts.toArray();
    drafts.sort(
      (a, b) =>
        new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime(),
    );
    return drafts;
  }
}
