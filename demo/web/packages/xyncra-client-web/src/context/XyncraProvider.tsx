/**
 * @packageDocumentation
 * XyncraProvider — React Context provider that owns the XyncraClient lifecycle.
 *
 * Design decisions implemented:
 * - D-4: Connection status tracking (connecting → connected → disconnected).
 * - D-5: deviceID auto-generation with localStorage persistence.
 * - D-6: Single XyncraClient instance shared across the app.
 * - D-3: Two-phase function registration (local registry + server sync).
 *
 * @module
 */

import type { ClientOptions } from '@xyncra/client-core';
import { XyncraClient } from '@xyncra/client-core';
import type { FunctionInfo } from '@xyncra/protocol';
import { message } from 'antd';
import {
  createContext,
  type JSX,
  type ReactNode,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import { BrowserIndexedDBProvider } from '../adapters/indexeddb';
import { ConsoleLogger } from '../adapters/logger';
import { BrowserWebSocketFactory } from '../adapters/websocket';
import type { UpdateHandlerEventMap } from '../internal/EventEmitter';
import { TypedEventEmitter } from '../internal/EventEmitter';
import type { FunctionHandler } from '../internal/FunctionRegistry';
import { FunctionRegistry } from '../internal/FunctionRegistry';
import { ReactUpdateHandler } from '../internal/ReactUpdateHandler';

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

/**
 * Props accepted by XyncraProvider.
 */
export interface XyncraProviderProps {
  /** React children to render inside the provider. */
  children: ReactNode;
  /** WebSocket URL to connect to (e.g. 'ws://localhost:8080/ws'). */
  wsUrl: string;
  /**
   * Device identifier. If omitted, one is generated via crypto.randomUUID()
   * and persisted to localStorage under the key `xyncra-device-id`.
   */
  deviceID?: string;
  /**
   * Agent / user identifier used as the userID for the WebSocket connection.
   * Defaults to 'agent'.
   */
  agentID?: string;
  /**
   * Optional callback that receives the ReactUpdateHandler instance,
   * allowing external code to subscribe to update events.
   */
  onUpdateHandler?: (handler: ReactUpdateHandler) => void;
}

/**
 * Connection lifecycle status.
 *
 * - `connecting`: client.start() has been called but no data received yet.
 * - `syncing`: handshake completed but no local data received within the 2s
 *   empty-database fallback window (per D-130). Also covers the in-progress
 *   sync phase before the first data event arrives.
 * - `connected`: at least one message or conversation event has been received.
 * - `disconnected`: client.start() has resolved (clean shutdown or 4001).
 */
export type ConnectionStatus =
  | 'connecting'
  | 'syncing'
  | 'connected'
  | 'disconnected';

/**
 * Value exposed through XyncraContext and returned by useXyncra().
 */
export interface XyncraContextValue {
  /** The underlying XyncraClient instance, or null before initialization. */
  client: XyncraClient | null;
  /** Current connection lifecycle status. */
  connectionStatus: ConnectionStatus;
  /** The device identifier in use (auto-generated if not provided). */
  deviceID: string;
  /** The agent / user identifier. */
  agentID: string;
  /** Client-side function registry. Register/unregister functions here. */
  functionRegistry: FunctionRegistry;
  /** Typed event emitter bridging IUpdateHandler callbacks to React hooks. */
  eventEmitter: TypedEventEmitter<UpdateHandlerEventMap>;
  /** Register a function (stores locally + syncs to server). */
  registerFunction: (info: FunctionInfo, handler: FunctionHandler) => void;
  /** Unregister a function by name (removes locally + syncs to server). */
  unregisterFunction: (name: string) => void;
}

// ---------------------------------------------------------------------------
// Context
// ---------------------------------------------------------------------------

/**
 * XyncraContext — consumed via useXyncra().
 * Null when used outside XyncraProvider.
 */
export const XyncraContext = createContext<XyncraContextValue | null>(null);

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

/** localStorage key for persisting the auto-generated deviceID (D-5). */
const DEVICE_ID_STORAGE_KEY = 'xyncra-device-id';

// ---------------------------------------------------------------------------
// DeviceID helper (D-5)
// ---------------------------------------------------------------------------

/**
 * Resolve the deviceID to use:
 * 1. If `provided` is non-empty, return it directly.
 * 2. Otherwise, read from localStorage.
 * 3. Otherwise, generate via crypto.randomUUID() and persist.
 *
 * @throws If crypto.randomUUID is not available and no deviceID was provided.
 */
function resolveDeviceID(provided?: string): string {
  if (provided && provided.length > 0) {
    return provided;
  }

  // Attempt to read from localStorage.
  try {
    const stored = globalThis.localStorage?.getItem(DEVICE_ID_STORAGE_KEY);
    if (stored && stored.length > 0) {
      return stored;
    }
  } catch {
    // localStorage unavailable (SSR, privacy mode, etc.) — fall through.
  }

  // Generate a new UUID.
  if (
    typeof crypto === 'undefined' ||
    typeof crypto.randomUUID !== 'function'
  ) {
    throw new Error(
      'XyncraProvider: crypto.randomUUID() is not available in this environment. ' +
        'Please provide a `deviceID` prop explicitly.',
    );
  }

  const id = crypto.randomUUID();

  // Persist for future sessions.
  try {
    globalThis.localStorage?.setItem(DEVICE_ID_STORAGE_KEY, id);
  } catch {
    // Ignore storage errors.
  }

  return id;
}

// ---------------------------------------------------------------------------
// XyncraProvider component
// ---------------------------------------------------------------------------

/**
 * XyncraProvider creates and manages a single XyncraClient instance for the
 * entire React application tree.
 *
 * Place it high in the component tree (e.g. in app.tsx's childrenRender) so
 * that all descendant components can access the client via useXyncra().
 *
 * ## Lifecycle
 *
 * 1. **Initialization** (synchronous, on first render):
 *    - Resolve deviceID (D-5).
 *    - Create ReactUpdateHandler, FunctionRegistry, TypedEventEmitter.
 *    - Create browser adapters (WebSocket, IndexedDB, Logger).
 *    - Instantiate XyncraClient.
 *
 * 2. **Start** (useEffect, runs once):
 *    - Set connection status to 'connecting'.
 *    - Listen for first message/conversation event → 'connected'.
 *    - Call client.start() (blocks until stop/4001).
 *    - On start() resolve → 'disconnected'.
 *
 * 3. **Function registration sync** (D-3):
 *    - Listen to functionRegistry.onChange.
 *    - On change: register request handlers on the client + RPC sync.
 *
 * 4. **Cleanup** (useEffect teardown):
 *    - Unsubscribe all event listeners.
 *    - Call client.stop().
 */
export function XyncraProvider({
  children,
  wsUrl,
  deviceID: providedDeviceID,
  agentID = 'agent',
  onUpdateHandler,
}: XyncraProviderProps): JSX.Element {
  // ---- Ant Design message API for error display ----
  const [messageApi, messageContextHolder] = message.useMessage();

  // ---- Connection status (the only piece of mutable React state) ----
  const [connectionStatus, setConnectionStatus] =
    useState<ConnectionStatus>('disconnected');

  // ---- Error handler for RPC failures ----
  const handleError = useCallback(
    (method: string, errorMessage: string, code: number) => {
      stableRef.current?.eventEmitter.emit('error:rpc', {
        method,
        message: errorMessage,
        code,
      });
      messageApi.error(`${errorMessage} (${method})`);
    },
    [messageApi],
  );

  // ---- Stable references (created once, never re-created) ----
  const stableRef = useRef<{
    client: XyncraClient;
    deviceID: string;
    functionRegistry: FunctionRegistry;
    eventEmitter: TypedEventEmitter<UpdateHandlerEventMap>;
    updateHandler: ReactUpdateHandler;
    lastSyncedVersion: number;
  } | null>(null);

  if (stableRef.current === null) {
    const deviceID = resolveDeviceID(providedDeviceID);
    const eventEmitter = new TypedEventEmitter<UpdateHandlerEventMap>();
    const updateHandler = new ReactUpdateHandler(eventEmitter);
    const functionRegistry = new FunctionRegistry();
    const wsFactory = new BrowserWebSocketFactory();
    const idbProvider = new BrowserIndexedDBProvider();
    const logger = new ConsoleLogger();

    const options: ClientOptions = {
      serverURL: wsUrl,
      userID: agentID,
      deviceID,
      wsFactory,
      idbProvider,
      logger,
      updateHandler,
      onError: handleError,
      onSyncComplete: () => {
        // Initial fullSync completed. If still in 'syncing' state
        // (empty database, no data events received), transition to 'connected'.
        setConnectionStatus((prev) => (prev === 'syncing' ? 'connected' : prev));
      },
      deviceInfo: {
        platform: 'web',
        userAgent:
          typeof navigator !== 'undefined' ? navigator.userAgent : 'unknown',
      },
    };

    const client = new XyncraClient(options);

    // Expose the updateHandler to external consumers if requested.
    if (onUpdateHandler) {
      onUpdateHandler(updateHandler);
    }

    stableRef.current = {
      client,
      deviceID,
      functionRegistry,
      eventEmitter,
      updateHandler,
      lastSyncedVersion: -1,
    };
  }

  const { client, deviceID, functionRegistry, eventEmitter, updateHandler } =
    stableRef.current;

  // ---- Lifecycle effect: start client, track status, sync functions ----
  useEffect(() => {
    setConnectionStatus('connecting');

    // -- Track connection established → 'connected' --
    // The connection is considered established when the WebSocket opens and
    // the initial handshake (system.reconnect + fullSync) completes.
    // We listen for the first data event as a confirmation, but also set
    // connected after a short delay if no data arrives (empty database case).
    let firstDataReceived = false;
    let connectionTimeout: ReturnType<typeof setTimeout> | null = null;

    const markConnected = () => {
      if (!firstDataReceived) {
        firstDataReceived = true;
        setConnectionStatus('connected');
        if (connectionTimeout) {
          clearTimeout(connectionTimeout);
          connectionTimeout = null;
        }
      }
    };

    const unsubMessage = eventEmitter.on('message:added', markConnected);
    const unsubConv = eventEmitter.on('conversation:added', markConnected);

    // Fallback: if no data arrives within 2 seconds, the connection has
    // successfully handshaked but the database is empty (the server has
    // nothing to send). Per D-130, we show 'syncing' rather than a fake
    // 'connected' so the UI reflects "connected to server, no local data".
    connectionTimeout = setTimeout(() => {
      if (!firstDataReceived) {
        setConnectionStatus('syncing');
      }
    }, 2000);

    // -- Function registry sync (D-3) --
    const unsubRegistryChange = functionRegistry.onChange(() => {
      const version = functionRegistry.getVersion();
      if (version === stableRef.current!.lastSyncedVersion) return;
      stableRef.current!.lastSyncedVersion = version;

      // Register reverse-RPC handlers for each function on the client.
      for (const info of functionRegistry.getFunctionInfos()) {
        const reqHandler = functionRegistry.createRequestHandler(info.name);
        if (reqHandler) {
          client.registerRequestHandler(info.name, reqHandler);
        }
      }

      // Sync the full function list to the server (full-replacement model).
      client
        .call('system.register_functions', {
          functions: functionRegistry.getFunctionInfos(),
        })
        .catch((err: unknown) => {
          // Log but do not throw — function registration failures are
          // non-fatal (fail-open, D-072).
          const msg = err instanceof Error ? err.message : String(err);
          handleError('system.register_functions', msg, 0);
        });
    });

    // -- Start the client (blocks until stop() or 4001) --
    client
      .start()
      .then(() => {
        setConnectionStatus('disconnected');
      })
      .catch((err: unknown) => {
        console.error('[xyncra] Client start failed:', err);
        setConnectionStatus('disconnected');
      });

    // -- Cleanup --
    return () => {
      unsubMessage();
      unsubConv();
      unsubRegistryChange();
      if (connectionTimeout) {
        clearTimeout(connectionTimeout);
      }
      client.stop();
    };
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // ---- Build the context value (re-computed only when connectionStatus changes) ----
  const contextValue = useMemo<XyncraContextValue>(
    () => ({
      client,
      connectionStatus,
      deviceID,
      agentID,
      functionRegistry,
      eventEmitter,
      registerFunction: (info: FunctionInfo, handler: FunctionHandler) => {
        functionRegistry.register(info, handler);
      },
      unregisterFunction: (name: string) => {
        functionRegistry.unregister(name);
      },
    }),
    // client, deviceID, agentID, functionRegistry, eventEmitter are all stable
    // (from the ref). Only connectionStatus changes during the lifecycle.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [connectionStatus],
  );

  return (
    <XyncraContext.Provider value={contextValue}>
      {messageContextHolder}
      {children}
    </XyncraContext.Provider>
  );
}
