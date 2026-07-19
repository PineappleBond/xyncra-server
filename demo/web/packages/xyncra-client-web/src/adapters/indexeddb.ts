/**
 * @packageDocumentation
 * Browser IndexedDB provider — supplies the IndexedDB factory to the web client.
 *
 * BrowserIndexedDBProvider implements IIndexedDBProvider from
 * @xyncra/client-core and is injected into ClientOptions as the IDBFactory
 * provider. The underlying IndexedDB access is handled by the core client.
 *
 * @module
 */

import type { IIndexedDBProvider } from '@xyncra/client-core';

// ---------------------------------------------------------------------------
// BrowserIndexedDBProvider
// ---------------------------------------------------------------------------

/**
 * BrowserIndexedDBProvider exposes the browser's `indexedDB` factory to the
 * core XyncraClient. Implements {@link IIndexedDBProvider} from
 * `@xyncra/client-core`.
 */
export class BrowserIndexedDBProvider implements IIndexedDBProvider {
  getIDBFactory(): IDBFactory {
    return indexedDB;
  }
}
