/**
 * Tests for ipc.ts — JSON-RPC 2.0 IPC over Unix domain sockets.
 */

import { mkdtempSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { createConnection } from 'node:net';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import {
  newIPCRequest,
  newIPCResponse,
  newIPCErrorResponse,
  IPCServer,
  IPCClient,
  ERR_PARSE,
  ERR_INVALID_REQUEST,
  ERR_METHOD_NOT_FOUND,
  ERR_SERVER,
  type IPCRequest,
  type IPCResponse,
} from '../ipc';

const UUID_V4_RE =
  /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

describe('newIPCRequest', () => {
  test('creates valid request with jsonrpc "2.0", uuid id, method', () => {
    const req = newIPCRequest('test_method', { key: 'value' });
    expect(req.jsonrpc).toBe('2.0');
    expect(req.method).toBe('test_method');
    expect(req.id).toMatch(UUID_V4_RE);
    expect(req.params).toEqual({ key: 'value' });
  });

  test('omits params field when params is undefined', () => {
    const req = newIPCRequest('no_params');
    expect(req.jsonrpc).toBe('2.0');
    expect(req.method).toBe('no_params');
    expect('params' in req).toBe(false);
  });

  test('generates unique ids for each request', () => {
    const a = newIPCRequest('m1');
    const b = newIPCRequest('m2');
    expect(a.id).not.toBe(b.id);
  });
});

describe('newIPCResponse', () => {
  test('creates valid success response', () => {
    const resp = newIPCResponse('req-1', { count: 42 });
    expect(resp.jsonrpc).toBe('2.0');
    expect(resp.id).toBe('req-1');
    expect(resp.result).toEqual({ count: 42 });
    expect(resp.error).toBeUndefined();
  });
});

describe('newIPCErrorResponse', () => {
  test('creates error response with code and message', () => {
    const resp = newIPCErrorResponse('req-2', -32601, 'Method not found');
    expect(resp.jsonrpc).toBe('2.0');
    expect(resp.id).toBe('req-2');
    expect(resp.error).toBeDefined();
    expect(resp.error!.code).toBe(-32601);
    expect(resp.error!.message).toBe('Method not found');
    expect(resp.result).toBeUndefined();
  });
});

/** Helper: create a fresh temp directory and return its path. */
function makeTmpDir(): string {
  return mkdtempSync(join(tmpdir(), 'xyncra-ipc-'));
}

/**
 * Helper: start an IPCServer with an echo handler registered.
 * Returns { server, sockPath, cleanup }.
 */
function startEchoServer() {
  const tmpDir = makeTmpDir();
  const sockPath = join(tmpDir, 'test.sock');
  const server = new IPCServer(sockPath);

  server.register('echo', async (req: IPCRequest) => {
    return newIPCResponse(req.id, req.params);
  });

  return { server, sockPath, tmpDir };
}

describe('IPCServer + IPCClient round-trip', () => {
  let tmpDir: string;
  let server: IPCServer;
  let sockPath: string;

  beforeEach(async () => {
    ({ server, sockPath, tmpDir } = startEchoServer());
    await server.start();
  });

  afterEach(async () => {
    await server.stop();
    rmSync(tmpDir, { recursive: true, force: true });
  });

  test('echo handler round-trip returns params as result', async () => {
    const client = new IPCClient(sockPath, 5000);
    const resp = await client.call('echo', { msg: 'hello' });
    expect(resp.error).toBeUndefined();
    expect(resp.result).toEqual({ msg: 'hello' });
  });

  test('socket file has 0o600 permissions', async () => {
    const stat = statSync(sockPath);
    // Mask out file type bits, just permission bits.
    const perm = stat.mode & 0o777;
    expect(perm).toBe(0o600);
  });
});

describe('IPC error propagation', () => {
  let tmpDir: string;
  let server: IPCServer;
  let sockPath: string;

  beforeEach(async () => {
    tmpDir = makeTmpDir();
    sockPath = join(tmpDir, 'test.sock');
    server = new IPCServer(sockPath);
    server.register('fail', async (_req: IPCRequest) => {
      throw new Error('something went wrong');
    });
    await server.start();
  });

  afterEach(async () => {
    await server.stop();
    rmSync(tmpDir, { recursive: true, force: true });
  });

  test('handler that throws produces error response with code -32000', async () => {
    const client = new IPCClient(sockPath, 5000);
    const resp = await client.call('fail');
    expect(resp.error).toBeDefined();
    expect(resp.error!.code).toBe(ERR_SERVER);
    expect(resp.error!.message).toBe('something went wrong');
  });

  test('unknown method produces method not found error (-32601)', async () => {
    const client = new IPCClient(sockPath, 5000);
    const resp = await client.call('nonexistent_method');
    expect(resp.error).toBeDefined();
    expect(resp.error!.code).toBe(ERR_METHOD_NOT_FOUND);
    expect(resp.error!.message).toBe('Method not found');
  });
});

describe('IPC raw protocol errors', () => {
  let tmpDir: string;
  let server: IPCServer;
  let sockPath: string;

  beforeEach(async () => {
    tmpDir = makeTmpDir();
    sockPath = join(tmpDir, 'test.sock');
    server = new IPCServer(sockPath);
    server.register('echo', async (req: IPCRequest) => {
      return newIPCResponse(req.id, 'ok');
    });
    await server.start();
  });

  afterEach(async () => {
    await server.stop();
    rmSync(tmpDir, { recursive: true, force: true });
  });

  /** Send raw bytes to the socket and read back the response line. */
  function sendRaw(data: string): Promise<IPCResponse> {
    return new Promise((resolve, reject) => {
      const conn = createConnection({ path: sockPath });
      let buf = '';

      conn.on('connect', () => {
        conn.write(data + '\n');
      });

      conn.on('data', (chunk) => {
        buf += chunk.toString('utf8');
        const idx = buf.indexOf('\n');
        if (idx !== -1) {
          const line = buf.slice(0, idx).trim();
          conn.end();
          try {
            resolve(JSON.parse(line) as IPCResponse);
          } catch (err) {
            reject(err);
          }
        }
      });

      conn.on('error', reject);

      setTimeout(() => {
        conn.destroy();
        reject(new Error('timeout'));
      }, 3000);
    });
  }

  test('invalid JSON produces parse error (-32700)', async () => {
    const resp = await sendRaw('this is not json');
    expect(resp.error).toBeDefined();
    expect(resp.error!.code).toBe(ERR_PARSE);
    expect(resp.error!.message).toBe('Parse error');
  });

  test('invalid jsonrpc version produces invalid request error (-32600)', async () => {
    const resp = await sendRaw('{"jsonrpc":"1.0","id":"1","method":"test"}');
    expect(resp.error).toBeDefined();
    expect(resp.error!.code).toBe(ERR_INVALID_REQUEST);
    expect(resp.error!.message).toBe('Invalid Request');
  });
});

describe('IPCServer stale socket cleanup', () => {
  test('start removes a pre-existing stale socket file', async () => {
    const tmpDir = makeTmpDir();
    const sockPath = join(tmpDir, 'stale.sock');
    // Create a stale socket file.
    writeFileSync(sockPath, 'stale');

    const server = new IPCServer(sockPath);
    server.register('ping', async (req: IPCRequest) => {
      return newIPCResponse(req.id, 'pong');
    });

    // Should not throw — stale socket is removed.
    await server.start();
    try {
      const client = new IPCClient(sockPath, 5000);
      const resp = await client.call('ping');
      expect(resp.error).toBeUndefined();
      expect(resp.result).toBe('pong');
    } finally {
      await server.stop();
      rmSync(tmpDir, { recursive: true, force: true });
    }
  });
});
