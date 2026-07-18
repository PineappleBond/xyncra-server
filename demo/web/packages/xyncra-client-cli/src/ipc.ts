/**
 * IPC (Inter-Process Communication) server and client over Unix domain sockets.
 *
 * Protocol: JSON-RPC 2.0, newline-delimited JSON over Unix domain socket.
 * Mirrors Go internal/cli/ipc.go exactly.
 *
 * @module
 */

import { createServer, type Server, type Socket } from 'node:net';
import { connect } from 'node:net';
import { unlink } from 'node:fs/promises';
import { chmodSync } from 'node:fs';
import { v4 as uuidv4 } from 'uuid';

// ---------------------------------------------------------------------------
// JSON-RPC 2.0 protocol types
// ---------------------------------------------------------------------------

/** JSON-RPC 2.0 request envelope. */
export interface IPCRequest {
  jsonrpc: '2.0';
  id: string;
  method: string;
  params?: unknown;
}

/** JSON-RPC 2.0 response envelope. */
export interface IPCResponse {
  jsonrpc: '2.0';
  id: string;
  result?: unknown;
  error?: IPCError;
}

/** JSON-RPC 2.0 error object. */
export interface IPCError {
  code: number;
  message: string;
  data?: unknown;
}

/** Handler function signature for IPC methods. */
export type IPCHandlerFunc = (req: IPCRequest) => Promise<IPCResponse>;

// ---------------------------------------------------------------------------
// JSON-RPC 2.0 error codes
// ---------------------------------------------------------------------------

/** Parse error: invalid JSON was received. */
export const ERR_PARSE = -32700;
/** Invalid Request: the JSON sent is not a valid Request object. */
export const ERR_INVALID_REQUEST = -32600;
/** Method not found. */
export const ERR_METHOD_NOT_FOUND = -32601;
/** Internal error (server-defined). */
export const ERR_SERVER = -32000;
/** Invalid params. */
export const ERR_INVALID_PARAMS = -32602;

// ---------------------------------------------------------------------------
// Factory helpers
// ---------------------------------------------------------------------------

/** Create a new IPCRequest with an auto-generated UUID v4 id. */
export function newIPCRequest(method: string, params?: unknown): IPCRequest {
  return {
    jsonrpc: '2.0',
    id: uuidv4(),
    method,
    ...(params !== undefined ? { params } : {}),
  };
}

/** Create a successful IPCResponse. */
export function newIPCResponse(id: string, result: unknown): IPCResponse {
  return { jsonrpc: '2.0', id, result };
}

/** Create an error IPCResponse. */
export function newIPCErrorResponse(id: string, code: number, message: string): IPCResponse {
  return {
    jsonrpc: '2.0',
    id,
    error: { code, message },
  };
}

// ---------------------------------------------------------------------------
// IPCServer
// ---------------------------------------------------------------------------

/**
 * IPCServer is a Unix domain socket server that dispatches JSON-RPC 2.0
 * requests to registered method handlers.
 *
 * Mirrors Go IPCServer in internal/cli/ipc.go.
 */
export class IPCServer {
  private readonly sockPath: string;
  private readonly handlers = new Map<string, IPCHandlerFunc>();
  private server: Server | null = null;
  private connections = new Set<Socket>();

  constructor(sockPath: string) {
    this.sockPath = sockPath;
  }

  /** Bind a handler to a method name. Must be called before start(). */
  register(method: string, handler: IPCHandlerFunc): void {
    this.handlers.set(method, handler);
  }

  /** Start accepting connections. Removes stale socket, chmod 0o600. */
  async start(): Promise<void> {
    // Remove stale socket.
    try {
      await unlink(this.sockPath);
    } catch {
      // May not exist — ignore.
    }

    return new Promise<void>((resolve, reject) => {
      this.server = createServer((conn) => {
        this.connections.add(conn);
        conn.on('close', () => this.connections.delete(conn));
        this.handleConn(conn);
      });

      this.server.on('error', (err) => {
        reject(new Error(`ipc server listen: ${err.message}`));
      });

      this.server.listen(this.sockPath, () => {
        try {
          chmodSync(this.sockPath, 0o600);
        } catch (err) {
          this.server?.close();
          reject(new Error(`ipc server chmod socket: ${(err as Error).message}`));
          return;
        }
        resolve();
      });
    });
  }

  /** Handle a single connection: read newline-delimited JSON, dispatch, respond. */
  private handleConn(conn: Socket): void {
    let buffer = '';

    conn.on('data', (chunk) => {
      buffer += chunk.toString('utf8');
      let newlineIdx: number;
      while ((newlineIdx = buffer.indexOf('\n')) !== -1) {
        const line = buffer.slice(0, newlineIdx).trim();
        buffer = buffer.slice(newlineIdx + 1);
        if (line.length === 0) continue;

        void this.dispatch(line).then((resp) => {
          const respJson = JSON.stringify(resp) + '\n';
          conn.write(respJson);
        });
      }
    });

    conn.on('error', () => {
      // Connection errors are transient — just close.
      conn.destroy();
    });
  }

  /** Parse a single JSON-RPC request line and invoke the handler. */
  private async dispatch(line: string): Promise<IPCResponse> {
    let req: IPCRequest;
    try {
      req = JSON.parse(line) as IPCRequest;
    } catch {
      return newIPCErrorResponse('', ERR_PARSE, 'Parse error');
    }

    if (req.jsonrpc !== '2.0') {
      return newIPCErrorResponse(req.id ?? '', ERR_INVALID_REQUEST, 'Invalid Request');
    }

    const handler = this.handlers.get(req.method);
    if (!handler) {
      return newIPCErrorResponse(req.id, ERR_METHOD_NOT_FOUND, 'Method not found');
    }

    // Handler must catch its own errors and return error responses.
    try {
      return await handler(req);
    } catch (err) {
      return newIPCErrorResponse(req.id, ERR_SERVER, (err as Error).message);
    }
  }

  /** Stop the server and close all connections. */
  async stop(): Promise<void> {
    // Close all active connections.
    for (const conn of this.connections) {
      conn.destroy();
    }
    this.connections.clear();

    return new Promise<void>((resolve) => {
      if (!this.server) {
        resolve();
        return;
      }
      this.server.close(() => resolve());
    });
  }
}

// ---------------------------------------------------------------------------
// IPCClient
// ---------------------------------------------------------------------------

/**
 * IPCClient connects to a running daemon over a Unix domain socket.
 * Each call() opens a fresh connection, sends one request, reads one
 * response, and closes.
 *
 * Mirrors Go IPCClient in internal/cli/ipc.go.
 */
export class IPCClient {
  private readonly sockPath: string;
  private readonly timeout: number;

  constructor(sockPath: string, timeout = 5000) {
    this.sockPath = sockPath;
    this.timeout = timeout;
  }

  /**
   * Send a single JSON-RPC request and return the response.
   * Opens a new connection per call.
   */
  async call(method: string, params?: unknown): Promise<IPCResponse> {
    const req = newIPCRequest(method, params);
    const reqJson = JSON.stringify(req) + '\n';

    return new Promise<IPCResponse>((resolve, reject) => {
      const conn = connect({ path: this.sockPath });
      let responseBuffer = '';
      let settled = false;

      const timer = setTimeout(() => {
        if (!settled) {
          settled = true;
          conn.destroy();
          reject(new Error('ipc client: connection timed out'));
        }
      }, this.timeout);

      conn.on('connect', () => {
        conn.write(reqJson);
      });

      conn.on('data', (chunk) => {
        responseBuffer += chunk.toString('utf8');
        const newlineIdx = responseBuffer.indexOf('\n');
        if (newlineIdx !== -1) {
          const line = responseBuffer.slice(0, newlineIdx).trim();
          clearTimeout(timer);
          settled = true;
          conn.end();
          try {
            const resp = JSON.parse(line) as IPCResponse;
            resolve(resp);
          } catch (err) {
            reject(new Error(`ipc client unmarshal response: ${(err as Error).message}`));
          }
        }
      });

      conn.on('error', (err) => {
        if (!settled) {
          clearTimeout(timer);
          settled = true;
          reject(new Error(`ipc client dial: ${err.message}`));
        }
      });

      conn.on('close', () => {
        clearTimeout(timer);
        if (!settled) {
          settled = true;
          reject(new Error('ipc client: connection closed before response'));
        }
      });
    });
  }
}
