/**
 * QuestionStore unit tests.
 */

import {
  createFreshDatabase,
  createQuestion,
  resetIdCounter,
} from '../test-helpers';

describe('QuestionStore', () => {
  let db: ReturnType<typeof createFreshDatabase>;

  beforeEach(async () => {
    resetIdCounter();
    db = createFreshDatabase(`test-question-${Date.now()}-${Math.random()}`);
    await db.open();
  });

  afterEach(async () => {
    await db.delete();
  });

  test('upsert creates a new question', async () => {
    const q = createQuestion({ conversation_id: 'conv-1' });
    await db.questionsStore.upsert(q);

    const results = await db.questionsStore.getByConversation('conv-1');
    expect(results).toHaveLength(1);
    expect(results[0].question_text).toBe('Are you sure?');
  });

  test('upsert updates existing question', async () => {
    const q = createQuestion({
      conversation_id: 'conv-1',
      question_text: 'v1',
    });
    await db.questionsStore.upsert(q);

    q.question_text = 'v2';
    await db.questionsStore.upsert(q);

    const results = await db.questionsStore.getByConversation('conv-1');
    expect(results).toHaveLength(1);
    expect(results[0].question_text).toBe('v2');
  });

  test('getByConversation returns sorted by created_at asc', async () => {
    const q1 = createQuestion({
      conversation_id: 'conv-1',
      created_at: new Date('2026-01-01'),
    });
    const q2 = createQuestion({
      conversation_id: 'conv-1',
      created_at: new Date('2026-06-01'),
    });
    await db.questionsStore.upsert(q1);
    await db.questionsStore.upsert(q2);

    const results = await db.questionsStore.getByConversation('conv-1');
    expect(results).toHaveLength(2);
    expect(results[0].id).toBe(q1.id); // older first
  });

  test('getByConversation returns empty for unknown conv', async () => {
    const results = await db.questionsStore.getByConversation('nope');
    expect(results).toHaveLength(0);
  });

  test('deleteByConversation removes all questions', async () => {
    await db.questionsStore.upsert(
      createQuestion({ conversation_id: 'conv-1' }),
    );
    await db.questionsStore.upsert(
      createQuestion({ conversation_id: 'conv-1' }),
    );

    await db.questionsStore.deleteByConversation('conv-1');

    const results = await db.questionsStore.getByConversation('conv-1');
    expect(results).toHaveLength(0);
  });
});
