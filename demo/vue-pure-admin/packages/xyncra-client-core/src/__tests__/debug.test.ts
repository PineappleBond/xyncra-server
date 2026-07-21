/**
 * Debug test - investigate MissingAPI in draft-store.
 */

import { IDBFactory } from 'fake-indexeddb';
import * as fs from 'fs';

const LOG_FILE = '/tmp/xyncra-debug.log';
function log(msg: string) {
  fs.appendFileSync(LOG_FILE, msg + '\n');
}

describe('debug draft', () => {
  beforeAll(() => {
    fs.writeFileSync(LOG_FILE, '');
  });

  test('track db state through save', async () => {
    const { createFreshDatabase, createDraft } = require('./test-helpers');
    const db = createFreshDatabase('test-draft-debug-' + Date.now());

    log('1. _deps.indexedDB truthy: ' + !!(db as any)._deps?.indexedDB);
    log('1. idbdb: ' + (db as any).idbdb);
    log('1. state.openComplete: ' + (db as any)._state?.openComplete);
    log('1. state.isBeingOpened: ' + (db as any)._state?.isBeingOpened);

    // Monitor close
    const origClose = db.close.bind(db);
    (db as any).close = (...args: any[]) => {
      log(
        'CLOSE called! stack: ' +
          new Error().stack?.split('\n').slice(0, 5).join(' | '),
      );
      return origClose(...args);
    };

    // Monitor versionchange
    db.on('versionchange', (ev: any) => {
      log(
        'VERSIONCHANGE event: newVersion=' +
          ev?.newVersion +
          ' oldVersion=' +
          ev?.oldVersion,
      );
    });

    db.on('close', () => {
      log('CLOSE event fired');
    });

    await db.open();
    log('2. After open - idbdb: ' + !!(db as any).idbdb);
    log('2. After open - openComplete: ' + (db as any)._state?.openComplete);
    log('2. After open - _deps.indexedDB: ' + !!(db as any)._deps?.indexedDB);

    // Now try accessing the drafts table
    try {
      const count = await db.drafts.count();
      log('3. count() succeeded: ' + count);
      log('3. After count - idbdb: ' + !!(db as any).idbdb);
      log('3. After count - openComplete: ' + (db as any)._state?.openComplete);
    } catch (e: any) {
      log('3. count() FAILED: ' + e.name + ': ' + e.message);
      log('3. After fail - idbdb: ' + !!(db as any).idbdb);
      log('3. After fail - openComplete: ' + (db as any)._state?.openComplete);
      log('3. After fail - _deps.indexedDB: ' + !!(db as any)._deps?.indexedDB);
    }

    await db.delete();
  });
});
