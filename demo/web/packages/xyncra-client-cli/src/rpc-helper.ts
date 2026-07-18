/**
 * Standalone WebSocket RPC fallback for when the daemon IPC is unavailable.
 *
 * Mirrors Go internal/cli/rpc_helper.go.
 * Opens a fresh WebSocket connection, sends a PackageDataRequest, reads
 * the PackageDataResponse, and returns the response data.
 *
 * @module
 */

import WebSocket from 'ws';
import type { PackageDataRequest, PackageDataResponse, Package } from '@xyncra/protocol';
import { PackageType, ResponseCode } from '@xyncra/protocol';
import type { CLIContext } from './cli-context.js';

/**
 * Send a single JSON-RPC request over a fresh WebSocket connection.
 * This is the shared fallback for CLI commands when IPC is unavailable (D-032).
 */
export async function standaloneRPC(
  cliCtx: CLIContext,
  method: string,
  params: Record<string, unknown>,
): Promise<unknown> {
  const url = cliCtx.getServerURLWithUser();

  return new Promise<unknown>((resolve, reject) => {
    const ws = new WebSocket(url);
    let settled = false;

    const timer = setTimeout(() => {
      if (!settled) {
        settled = true;
        ws.close();
        reject(new Error(`${cliCtx.serverURL}: server timed out`));
      }
    }, 10000);

    ws.on('open', () => {
      const req: PackageDataRequest = {
        id: '1',
        method,
        params,
      };

      const pkg: Package = {
        version: 1,
        type: PackageType.Request,
        data: req,
      };

      ws.send(JSON.stringify(pkg));
    });

    ws.on('message', (data: WebSocket.Data) => {
      if (settled) return;
      clearTimeout(timer);
      settled = true;
      ws.close();

      try {
        const respPkg = JSON.parse(data.toString()) as Package;
        const resp = respPkg.data as PackageDataResponse;

        if (resp.code !== ResponseCode.OK) {
          reject(new Error(resp.msg || `RPC error code ${resp.code}`));
          return;
        }

        if (isMutationMethod(method)) {
          process.stderr.write(
            'Note: Operation succeeded on server. Local database will be updated when daemon starts.\n',
          );
          process.stderr.write(
            "Hint: Run 'xyncra-client listen --user-id <user>' to enable local queries.\n",
          );
        }

        resolve(resp.data);
      } catch (err) {
        reject(new Error(`standalone unmarshal response: ${(err as Error).message}`));
      }
    });

    ws.on('error', (err) => {
      if (!settled) {
        clearTimeout(timer);
        settled = true;
        reject(new Error(`${cliCtx.serverURL}: ${err.message}`));
      }
    });

    ws.on('close', () => {
      clearTimeout(timer);
      if (!settled) {
        settled = true;
        reject(new Error('standalone: connection closed before response'));
      }
    });
  });
}

/**
 * Returns true if the RPC method modifies server state.
 * Used to display a hint about local database sync.
 */
export function isMutationMethod(method: string): boolean {
  switch (method) {
    case 'send_message':
    case 'delete_message':
    case 'mark_as_read':
    case 'create_conversation':
    case 'delete_conversation':
    case 'restore_conversation':
      return true;
    default:
      return false;
  }
}
