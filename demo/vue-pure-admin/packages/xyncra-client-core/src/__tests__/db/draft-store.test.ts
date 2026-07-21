/**
 * DraftStore unit tests.
 */

import { ErrNotFound } from '../../errors';
import {
  createDraft,
  createFreshDatabase,
  resetIdCounter,
} from '../test-helpers';

describe('DraftStore', () => {
  let db: ReturnType<typeof createFreshDatabase>;

  beforeEach(async () => {
    resetIdCounter();
    db = createFreshDatabase(`test-draft-${Date.now()}-${Math.random()}`);
    await db.open();
  });

  afterEach(async () => {
    await db.delete();
  });

  test('save creates a new draft', async () => {
    const draft = createDraft({ conversation_id: 'conv-1', content: 'hello' });
    await db.draftsStore.save(draft);

    const result = await db.draftsStore.getByConversation('conv-1');
    expect(result.content).toBe('hello');
  });

  test('save updates existing draft for same conversation', async () => {
    const draft1 = createDraft({ conversation_id: 'conv-1', content: 'v1' });
    await db.draftsStore.save(draft1);

    const draft2 = createDraft({ conversation_id: 'conv-1', content: 'v2' });
    await db.draftsStore.save(draft2);

    const result = await db.draftsStore.getByConversation('conv-1');
    expect(result.content).toBe('v2');
  });

  test('getByConversation throws ErrNotFound when none exists', async () => {
    await expect(db.draftsStore.getByConversation('nope')).rejects.toBe(
      ErrNotFound,
    );
  });

  test('delete removes a draft', async () => {
    const draft = createDraft({ conversation_id: 'conv-1' });
    await db.draftsStore.save(draft);

    const saved = await db.draftsStore.getByConversation('conv-1');
    await db.draftsStore.delete(saved.id);

    await expect(db.draftsStore.getByConversation('conv-1')).rejects.toBe(
      ErrNotFound,
    );
  });

  test('deleteByConversation removes the draft', async () => {
    const draft = createDraft({ conversation_id: 'conv-1' });
    await db.draftsStore.save(draft);

    await db.draftsStore.deleteByConversation('conv-1');

    await expect(db.draftsStore.getByConversation('conv-1')).rejects.toBe(
      ErrNotFound,
    );
  });

  test('delete throws ErrNotFound for missing id', async () => {
    await expect(db.draftsStore.delete('nope')).rejects.toBe(ErrNotFound);
  });

  test('deleteByConversation throws ErrNotFound when none exists', async () => {
    await expect(db.draftsStore.deleteByConversation('nope')).rejects.toBe(
      ErrNotFound,
    );
  });

  test('list returns all drafts sorted by updated_at desc', async () => {
    const draft1 = createDraft({
      conversation_id: 'conv-1',
      updated_at: new Date('2026-01-01'),
    });
    const draft2 = createDraft({
      conversation_id: 'conv-2',
      updated_at: new Date('2026-06-01'),
    });
    await db.drafts.add(draft1);
    await db.drafts.add(draft2);

    const results = await db.draftsStore.list();
    expect(results).toHaveLength(2);
    expect(results[0].conversation_id).toBe('conv-2'); // newer first
  });
});
