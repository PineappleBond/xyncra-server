import { BrowserIndexedDBProvider } from '../../adapters/indexeddb';

describe('BrowserIndexedDBProvider', () => {
  const provider = new BrowserIndexedDBProvider();
  const protoMethods = Object.getOwnPropertyNames(
    Object.getPrototypeOf(provider),
  );

  it('should expose getIDBFactory method', () => {
    expect(typeof (provider as any).getIDBFactory).toBe('function');
  });

  it('should not expose CRUD methods', () => {
    const crudMethods = [
      'init',
      'getConversation',
      'saveConversation',
      'deleteConversation',
      'listConversations',
      'getMessage',
      'saveMessage',
      'deleteMessage',
      'listMessages',
      'clear',
    ];
    for (const m of crudMethods) {
      expect(protoMethods).not.toContain(m);
    }
  });
});
