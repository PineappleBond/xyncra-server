/**
 * QuestionStore — client-side Question persistence (D-125).
 *
 * Mirrors Go QuestionStore (pkg/store/question_store.go).
 */

import type Dexie from 'dexie';
import type { Transaction } from 'dexie';

import type { XyncraDatabase } from './index';
import type { Question } from './models';

/**
 * QuestionStore provides client-side Question persistence (D-125).
 */
export class QuestionStore {
  constructor(private readonly db: XyncraDatabase) {}

  /**
   * Creates or updates a question (idempotent by id).
   */
  async upsert(q: Question): Promise<void> {
    await this.db.questions.put(q);
  }

  /**
   * Returns all questions for a conversation, ordered by creation time ascending.
   */
  async getByConversation(convID: string): Promise<Question[]> {
    const questions = await this.db.questions
      .where('conversation_id')
      .equals(convID)
      .toArray();

    // Sort by created_at ascending.
    questions.sort(
      (a, b) =>
        new Date(a.created_at).getTime() - new Date(b.created_at).getTime(),
    );

    return questions;
  }

  /**
   * Removes all questions for a conversation.
   */
  async deleteByConversation(convID: string): Promise<void> {
    await this.db.questions.where('conversation_id').equals(convID).delete();
  }

  /**
   * Removes all questions for a conversation within the given transaction.
   */
  async deleteByConversationTx(tx: Transaction, convID: string): Promise<void> {
    const table = tx.table('questions') as Dexie.Table<Question, string>;
    await table.where('conversation_id').equals(convID).delete();
  }
}
