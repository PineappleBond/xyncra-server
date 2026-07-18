/**
 * Daemon lifecycle — the "listen" command implementation.
 *
 * Mirrors Go runListen in internal/cli/listen.go.
 * Acquires the process lock, opens the database, starts the IPC server,
 * builds the XyncraClient, registers handlers, and blocks until shutdown.
 *
 * @module
 */

import type { Command } from 'commander';
import { XyncraClient } from '@xyncra/client-core';
import type { ClientOptions, IWebSocket, IWebSocketFactory, IIndexedDBProvider } from '@xyncra/client-core';
import WebSocket from 'ws';
import { CLIContext } from './cli-context.js';
import { acquireLock, type LockInfo } from './lock.js';
import { IPCServer } from './ipc.js';
import { CLILogger } from './logger.js';
import { CLIUpdateHandler } from './update-handler.js';
import { builtinFunctionInfos, registerBuiltinHandlers } from './builtin-functions.js';
import { registerIPCHandlers } from './ipc-handlers.js';
import { startLogCleanup } from './log-cleanup.js';

/**
 * Run the listen daemon.
 *
 * Flow (mirrors Go runListen):
 * 1. Parse CLIContext from command flags
 * 2. Acquire exclusive process lock
 * 3. Open IndexedDB (fake-indexeddb for Node.js)
 * 4. Create IPC Server
 * 5. Create UpdateHandler + Logger
 * 6. Build XyncraClient with injected dependencies
 * 7. Register built-in function handlers
 * 8. Register IPC handlers
 * 9. Start IPC Server
 * 10. Set up signal handling (SIGINT, SIGTERM)
 * 11. Watch client.done() for 4001 replacement exit
 * 12. Start automatic log cleanup
 * 13. Start the client (blocks until shutdown)
 */
export async function runListen(cmd: Command): Promise<void> {
  // 1. Parse CLIContext.
  const cliCtx = CLIContext.fromCommand(cmd);

  // 2. Acquire exclusive process lock.
  const lockInfo: LockInfo = {
    pid: process.pid,
    started_at: new Date().toISOString(),
    device_id: cliCtx.deviceID,
  };
  const unlock = await acquireLock(cliCtx.getLockPath(), lockInfo);

  try {
    // 3. Open IndexedDB (use fake-indexeddb for Node.js).
    const fakeIndexedDB = await import('fake-indexeddb/auto');
    const idbProvider: IIndexedDBProvider = {
      getIDBFactory: () => globalThis.indexedDB,
    };

    // 4. Create IPC Server.
    const ipcServer = new IPCServer(cliCtx.getSocketPath());

    // 5. Create UpdateHandler + Logger.
    const handler = new CLIUpdateHandler();
    const logger = new CLILogger();

    // 6. Build XyncraClient.
    const wsFactory: IWebSocketFactory = {
      create: (url: string): IWebSocket => {
        const ws = new WebSocket(url);
        // Wrap ws WebSocket to implement IWebSocket interface.
        return createWebSocketAdapter(ws);
      },
    };

    // Parse device-info flag.
    const deviceInfoOpt = cmd.opts()['deviceInfo'] as string | undefined;
    const deviceInfo = parseDeviceInfo(deviceInfoOpt ?? '');

    const clientOpts: ClientOptions = {
      serverURL: cliCtx.getServerURLWithUser(),
      userID: cliCtx.userID,
      deviceID: cliCtx.deviceID,
      dbPath: cliCtx.dbPath,
      wsFactory,
      idbProvider,
      logger,
      updateHandler: handler,
      deviceInfo,
      functions: builtinFunctionInfos(),
    };

    const client = new XyncraClient(clientOpts);

    // 7. Register built-in function handlers.
    registerBuiltinHandlers(client);

    // 8. Register IPC handlers.
    registerIPCHandlers(ipcServer, client, cliCtx.userID);

    // 9. Start IPC Server.
    await ipcServer.start();

    // Print startup banner.
    process.stderr.write('[xyncra] Starting listener daemon...\n');
    process.stderr.write(`[xyncra] Device: ${cliCtx.deviceID}\n`);
    process.stderr.write(`[xyncra] Connecting to ${cliCtx.getServerURLWithUser()} ...\n`);
    process.stderr.write(`[xyncra] IPC server listening at ${cliCtx.getSocketPath()}\n`);
    process.stderr.write('[xyncra] Listening for updates... (Ctrl+C to stop)\n');

    // 10. Set up signal handling.
    const abortController = new AbortController();
    const onSignal = (): void => {
      abortController.abort();
    };
    process.on('SIGINT', onSignal);
    process.on('SIGTERM', onSignal);

    // 11. Watch client.done() for graceful exit (4001 replacement).
    void client.done().then(() => {
      abortController.abort();
    });

    // 12. Start automatic log cleanup.
    const stopCleanup = startLogCleanup(undefined, undefined, logger);

    // 13. Start the client (blocks until abort).
    try {
      await client.start(abortController.signal);
    } catch {
      // Client may throw when aborted — that's expected.
    }

    // Cleanup.
    stopCleanup();
    process.removeListener('SIGINT', onSignal);
    process.removeListener('SIGTERM', onSignal);
    client.stop();
    await ipcServer.stop();

    // Remove socket file.
    try {
      const { unlinkSync } = await import('node:fs');
      unlinkSync(cliCtx.getSocketPath());
    } catch {
      // Best effort.
    }
  } finally {
    await unlock();
  }
}

/** Parse a JSON string into a device info map. */
function parseDeviceInfo(jsonStr: string): Record<string, string> | undefined {
  if (!jsonStr) return undefined;
  try {
    return JSON.parse(jsonStr) as Record<string, string>;
  } catch {
    return {};
  }
}

/**
 * Adapt a Node.js WebSocket (ws package) to the IWebSocket interface.
 */
function createWebSocketAdapter(ws: WebSocket): IWebSocket {
  const adapter: IWebSocket = {
    get readyState(): number {
      return ws.readyState;
    },
    send(data: string | Uint8Array): void {
      ws.send(data);
    },
    close(code?: number, reason?: string): void {
      ws.close(code, reason);
    },
    onmessage(handler: (data: string | Uint8Array) => void): void {
      ws.on('message', (data: WebSocket.Data) => {
        if (typeof data === 'string') {
          handler(data);
        } else if (data instanceof Buffer) {
          handler(new Uint8Array(data));
        } else {
          // ArrayBuffer or Buffer[]
          handler(data.toString());
        }
      });
    },
    onclose(handler: (code: number, reason: string) => void): void {
      ws.on('close', (code: number, reason: Buffer) => {
        handler(code, reason.toString());
      });
    },
    onerror(handler: (error: Error) => void): void {
      ws.on('error', (err) => handler(err));
    },
    onopen(handler: () => void): void {
      ws.on('open', handler);
    },
  };
  return adapter;
}
