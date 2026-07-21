import { type App, type InjectionKey, ref, watch, type Ref } from 'vue'
import { XyncraClient, type ClientOptions } from '@xyncra/client-core'
import type { FunctionInfo } from '@xyncra/protocol'
import { BrowserWebSocketFactory } from './adapters/websocket'
import type { ReconnectionState } from './adapters/websocket'
import { BrowserIndexedDBProvider } from './adapters/indexeddb'
import { ConsoleLogger } from './adapters/logger'
import { FunctionRegistry } from './internal/FunctionRegistry'
import { VueUpdateHandler } from './internal/VueUpdateHandler'
import { TypedEventEmitter, type UpdateHandlerEventMap } from './internal/EventEmitter'
import { initComponentRegistry } from './utils/component-accessor'
import { generalFunctions } from './functions/general'

export type ConnectionStatus = 'disconnected' | 'connecting' | 'syncing' | 'connected'

export interface ReconnectionInfo {
  isReconnecting: Ref<boolean>
  attempt: Ref<number>
  maxRetries: Ref<number>
  nextRetryIn: Ref<number>
}

export const XyncraClientKey: InjectionKey<{
  client: XyncraClient
  connectionStatus: Ref<ConnectionStatus>
  reconnection: ReconnectionInfo
  registry: FunctionRegistry
  eventEmitter: TypedEventEmitter<UpdateHandlerEventMap>
  registerFunction: (info: FunctionInfo, handler: (params: Record<string, unknown>) => Promise<unknown>) => void
  unregisterFunction: (name: string) => void
  call: (method: string, params: unknown) => Promise<unknown>
  reconnect: () => void
}> = Symbol('xyncra-client')

export interface XyncraPluginOptions {
  serverURL?: string
  userID?: string
  deviceID?: string
  autoConnect?: boolean
}

function resolveDeviceID(provided?: string): string {
  if (provided) return provided
  const key = 'xyncra-device-id'
  const stored = localStorage.getItem(key)
  if (stored) return stored
  const id = crypto.randomUUID()
  localStorage.setItem(key, id)
  return id
}

export function createXyncraPlugin(options: XyncraPluginOptions = {}) {
  return {
    install(app: App) {
      const {
        serverURL = 'ws://localhost:18080/ws',
        userID = 'agent',
        deviceID,
        autoConnect = true,
      } = options

      const resolvedDeviceID = resolveDeviceID(deviceID)
      const connectionStatus = ref<ConnectionStatus>('disconnected')

      const wsFactory = new BrowserWebSocketFactory()
      const idbProvider = new BrowserIndexedDBProvider()
      const logger = new ConsoleLogger()
      const eventEmitter = new TypedEventEmitter<UpdateHandlerEventMap>()
      const updateHandler = new VueUpdateHandler(eventEmitter)
      const registry = new FunctionRegistry()

      // Reconnection state
      const reconnecting = ref(false)
      const reconnectAttempt = ref(0)
      const reconnectMaxRetries = ref(5)
      const reconnectNextRetryIn = ref(0)

      const clientOptions: ClientOptions = {
        serverURL,
        userID,
        deviceID: resolvedDeviceID,
        wsFactory,
        idbProvider,
        logger,
        updateHandler,
        functions: [],
        deviceInfo: {
          platform: 'web',
          userAgent: navigator.userAgent,
        },
        // Agent replies can be large (100KB+); increase max message size to 512KB
        maxMessageSize: 512 * 1024,
        // FullSync with many updates may need more time
        rpcTimeout: 120_000,
        onSyncComplete: () => {
          connectionStatus.value = 'connected'
        },
      }

      const client = new XyncraClient(clientOptions)

      // Wire up reconnection state from the WebSocket bridge
      const bridge = wsFactory.getLastBridge()
      if (bridge) {
        bridge.onreconnection((state: ReconnectionState) => {
          reconnecting.value = state.isReconnecting
          reconnectAttempt.value = state.attempt
          reconnectMaxRetries.value = state.maxRetries
          reconnectNextRetryIn.value = state.nextRetryIn
          if (state.isReconnecting) {
            connectionStatus.value = 'connecting'
          }
        })
      }

      // syncFunctionsToClient pushes the full function list to the client:
      // (1) registers reverse-RPC request handlers so the server's tool calls
      //     can be dispatched to the local handler,
      // (2) keeps client.options.functions in sync so the reconnect handshake
      //     re-sends them after the socket is open,
      // (3) sends system.register_functions to the server (fail-open).
      const syncFunctionsToClient = () => {
        const fns = registry.getFunctionInfos()
        if (fns.length === 0) return

        // 1. Register reverse-RPC handlers for each function on the client.
        for (const info of fns) {
          const reqHandler = registry.createRequestHandler(info.name)
          if (reqHandler) {
            client.registerRequestHandler(info.name, reqHandler)
          }
        }

        // 2. Keep client.options.functions in sync for the reconnect handshake
        //    and trigger immediate re-registration if connected.
        client.setFunctions(fns)
      }

      registry.onChange(() => {
        const fns = registry.getFunctionInfos()
        console.log(`[xyncra-plugin] registry.onChange: ${fns.length} functions`, fns.map(f => f.name))
        syncFunctionsToClient()
      })

      // Re-sync functions when the connection becomes ready.
      // This mirrors the React XyncraProvider's useEffect that calls
      // syncFunctionsToClient when connectionStatus changes to 'connected'
      // or 'syncing'. Without this, functions registered before the
      // WebSocket is open would only be synced during the next reconnect
      // handshake, which may miss the first agent invocation.
      watch(connectionStatus, (status) => {
        if (status === 'connected' || status === 'syncing') {
          syncFunctionsToClient()
        }
      })

      const reconnection: ReconnectionInfo = {
        isReconnecting: reconnecting,
        attempt: reconnectAttempt,
        maxRetries: reconnectMaxRetries,
        nextRetryIn: reconnectNextRetryIn,
      }

      function reconnect() {
        const b = wsFactory.getLastBridge()
        if (b) {
          b.reconnect()
        }
        connectionStatus.value = 'connecting'
      }

      const provided = {
        client,
        connectionStatus,
        reconnection,
        registry,
        eventEmitter,
        registerFunction: (info: FunctionInfo, handler: (params: Record<string, unknown>) => Promise<unknown>) => {
          registry.register(info, handler)
        },
        unregisterFunction: (name: string) => {
          registry.unregister(name)
        },
        call: (method: string, params: unknown) => client.call(method, params),
        reconnect,
      }

      initComponentRegistry()
      for (const fn of generalFunctions) {
        registry.register(fn.info, fn.handler)
      }
      app.provide(XyncraClientKey, provided)
      app.config.globalProperties.$xyncra = provided

      if (autoConnect) {
        app.mixin({
          mounted() {
            if (!(app as any).__xyncra_started) {
              ;(app as any).__xyncra_started = true
              connectionStatus.value = 'connecting'
              client.start().catch((err) => {
                logger.error('Xyncra client start failed', err)
                connectionStatus.value = 'disconnected'
              })
              setTimeout(() => {
                if (connectionStatus.value === 'connecting') {
                  connectionStatus.value = 'syncing'
                }
              }, 2000)
            }
          },
        })
      }


    },
  }
}
