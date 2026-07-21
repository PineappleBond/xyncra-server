/**
 * Mock WebSocket server for testing ConnectionManager and XyncraClient.
 *
 * Mirrors Go pkg/client/mock_ws_test.go.
 * Uses the `ws` npm package to spin up a real WebSocket server on a random port.
 */

import { WebSocket, WebSocketServer } from 'ws';

/**
 * MockWSServer is a lightweight WebSocket server for integration tests.
 * It allows tests to simulate server behaviour such as message echoes,
 * close-frame injection, and device-replacement simulation.
 */
export class MockWSServer {
  private wss: WebSocketServer | null = null;
  private port = 0;
  private clients: Set<WebSocket> = new Set();

  private connectionHandlers: Array<(ws: WebSocket) => void> = [];
  private messageHandlers: Array<(ws: WebSocket, data: string) => void> = [];

  constructor(port = 0) {
    this.port = port;
  }

  /**
   * Starts the mock WebSocket server. Returns the ws:// URL.
   */
  async start(): Promise<string> {
    return new Promise<string>((resolve, reject) => {
      this.wss = new WebSocketServer({ port: this.port });

      this.wss.on('listening', () => {
        const addr = this.wss!.address();
        if (typeof addr === 'string' || addr === null) {
          resolve(addr ?? `ws://127.0.0.1:${this.port}`);
        } else {
          this.port = addr.port;
          resolve(`ws://127.0.0.1:${addr.port}`);
        }
      });

      this.wss.on('error', reject);

      this.wss.on('connection', (ws) => {
        this.clients.add(ws);

        ws.on('close', () => {
          this.clients.delete(ws);
        });

        ws.on('message', (rawData) => {
          const data = rawData.toString();
          for (const handler of this.messageHandlers) {
            handler(ws, data);
          }
        });

        for (const handler of this.connectionHandlers) {
          handler(ws);
        }
      });
    });
  }

  /**
   * Stops the mock WebSocket server and closes all client connections.
   */
  async stop(): Promise<void> {
    for (const client of this.clients) {
      try {
        client.close(1000, 'server stopping');
      } catch {
        // ignore
      }
    }
    this.clients.clear();

    return new Promise<void>((resolve) => {
      if (!this.wss) {
        resolve();
        return;
      }
      this.wss.close(() => {
        this.wss = null;
        resolve();
      });
    });
  }

  /**
   * Registers a handler called when a new client connects.
   */
  onConnection(handler: (ws: WebSocket) => void): void {
    this.connectionHandlers.push(handler);
  }

  /**
   * Registers a handler called when any client sends a message.
   */
  onMessage(handler: (ws: WebSocket, data: string) => void): void {
    this.messageHandlers.push(handler);
  }

  /**
   * Sends data to all connected clients.
   */
  broadcast(data: string): void {
    for (const client of this.clients) {
      if (client.readyState === WebSocket.OPEN) {
        client.send(data);
      }
    }
  }

  /**
   * Simulates device replacement by sending a 4001 close frame to all clients.
   */
  simulateDeviceReplacement(): void {
    for (const client of this.clients) {
      if (client.readyState === WebSocket.OPEN) {
        client.close(4001, 'device replaced');
      }
    }
  }

  /**
   * Closes all connections with the given code and reason.
   */
  simulateDisconnect(code = 1000, reason = 'test'): void {
    for (const client of this.clients) {
      if (client.readyState === WebSocket.OPEN) {
        client.close(code, reason);
      }
    }
  }

  /** Returns the number of currently connected clients. */
  clientCount(): number {
    return this.clients.size;
  }

  /** Returns the URL of the running server. */
  url(): string {
    return `ws://127.0.0.1:${this.port}`;
  }
}
