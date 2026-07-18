/**
 * Built-in diagnostic functions automatically registered by every client.
 *
 * Mirrors Go internal/cli/builtin_functions.go exactly.
 *
 * @module
 */

import type { FunctionInfo, PackageDataRequest } from '@xyncra/protocol';
import type { XyncraClient, RequestHandlerFunc } from '@xyncra/client-core';
import { hostname } from 'node:os';
import { arch, platform } from 'node:os';

/**
 * Return metadata for the 3 built-in diagnostic functions.
 * These are auto-registered on every client device (D-098, D-099).
 */
export function builtinFunctionInfos(): FunctionInfo[] {
  return [
    {
      name: 'ping',
      description: 'Echo back a message with timestamp',
      parameters: {
        type: 'object',
        properties: {
          message: {
            type: 'string',
            description: 'Message to echo back',
          },
        },
      },
      tags: ['diagnostic'],
      returns: {
        type: 'object',
        description: 'Echoed message and server timestamp',
      },
    },
    {
      name: 'get_device_info',
      description: 'Get basic device information',
      parameters: { type: 'object' },
      tags: ['diagnostic'],
      returns: {
        type: 'object',
        description: 'Device hostname, OS, architecture, and process ID',
      },
    },
    {
      name: 'get_time',
      description: 'Get current device time',
      parameters: { type: 'object' },
      tags: ['diagnostic'],
      returns: {
        type: 'object',
        description: 'Current UTC time, unix timestamp, and timezone',
      },
    },
  ];
}

/**
 * Register built-in function handlers on the XyncraClient.
 * The server can invoke these via reverse RPC (D-092).
 */
export function registerBuiltinHandlers(client: XyncraClient): void {
  client.registerRequestHandler('ping', pingHandler);
  client.registerRequestHandler('get_device_info', getDeviceInfoHandler);
  client.registerRequestHandler('get_time', getTimeHandler);
}

/** Echo back the input message with a timestamp. */
async function pingHandler(req: PackageDataRequest): Promise<unknown> {
  const params = (req.params as Record<string, unknown>) ?? {};
  const message = (params.message as string) ?? '';
  return {
    echo: message,
    timestamp: new Date().toISOString(),
  };
}

/** Return device hostname, OS, architecture, and PID. */
async function getDeviceInfoHandler(_req: PackageDataRequest): Promise<unknown> {
  let host: string;
  try {
    host = hostname();
  } catch {
    host = 'unknown';
  }
  return {
    hostname: host,
    os: platform(),
    arch: arch(),
    pid: process.pid,
  };
}

/** Return current UTC time, Unix timestamp, and timezone. */
async function getTimeHandler(_req: PackageDataRequest): Promise<unknown> {
  const now = new Date();
  return {
    utc: now.toISOString(),
    unix: Math.floor(now.getTime() / 1000),
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
  };
}
