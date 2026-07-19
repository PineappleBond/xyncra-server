/**
 * @packageDocumentation
 * Type-safe event emitter for bridging core client updates to React hooks.
 *
 * Design decision D-1: ReactUpdateHandler emits events through TypedEventEmitter;
 * each React hook subscribes in useEffect. This decouples the long-lived
 * IUpdateHandler from React's render cycle.
 *
 * @module
 */

/**
 * EventMap defines the mapping from event names to their payload types.
 * Each key is an event name; the value is the type of data emitted with that event.
 */
export type UpdateHandlerEventMap = {
  'conversation:added': { conversation: ConversationEvent };
  'conversation:updated': { conversation: ConversationEvent };
  'conversation:removed': { conversationId: string };
  'read:updated': { conversationId: string; lastReadMessageId: string };
  'message:added': { message: MessageEvent };
  'message:updated': { message: MessageEvent };
  'message:removed': { messageId: string; conversationId: string };
  'stream:text': {
    userId: string;
    conversationId: string;
    streamId: string;
    text: string;
  };
  'stream:done': {
    userId: string;
    conversationId: string;
    streamId: string;
  };
  'agent:status': {
    userId: string;
    conversationId: string;
    status: string;
  };
  'agent:thinking': {
    userId: string;
    conversationId: string;
    isTyping: boolean;
  };
  'hitl:question': {
    userId: string;
    conversationId: string;
    reason: string;
  };
  'error:rpc': {
    method: string;
    message: string;
    code: number;
  };
};

/**
 * Conversation data emitted on conversation add/update events.
 * Mirrors the camelCase Message type from @xyncra/client-core interfaces.ts.
 */
export interface ConversationEvent {
  id: string;
  userId1: string;
  userId2: string;
  title?: string;
  lastMessageId?: string;
  lastMessageAt?: string;
  lastReadMessageId1?: string;
  lastReadMessageId2?: string;
  createdAt: string;
  updatedAt?: string;
  deletedAt?: string;
}

/**
 * Message data emitted on message add/update events.
 * Mirrors the camelCase Message type from @xyncra/client-core interfaces.ts.
 */
export interface MessageEvent {
  id: string;
  conversationId: string;
  senderId: string;
  content: string;
  clientMessageId: string;
  replyToId?: string;
  createdAt: string;
  updatedAt?: string;
  deletedAt?: string;
}

/**
 * Listener function type — receives the event payload.
 */
type Listener<T> = (payload: T) => void;

/**
 * TypedEventEmitter provides type-safe event emission and subscription.
 *
 * @typeParam TEventMap - An interface mapping event names to payload types.
 */
export class TypedEventEmitter<
  TEventMap extends Record<string, unknown> = UpdateHandlerEventMap,
> {
  private listeners = new Map<keyof TEventMap, Set<Listener<unknown>>>();

  /**
   * Subscribe to an event.
   *
   * @param event - The event name to listen for.
   * @param listener - Callback invoked when the event is emitted.
   * @returns A function that removes this listener when called.
   */
  on<K extends keyof TEventMap>(
    event: K,
    listener: Listener<TEventMap[K]>,
  ): () => void {
    let set = this.listeners.get(event);
    if (!set) {
      set = new Set();
      this.listeners.set(event, set);
    }
    set.add(listener as Listener<unknown>);

    return () => {
      set!.delete(listener as Listener<unknown>);
      if (set!.size === 0) {
        this.listeners.delete(event);
      }
    };
  }

  /**
   * Subscribe to an event for a single emission only.
   *
   * @param event - The event name to listen for.
   * @param listener - Callback invoked once when the event is emitted.
   * @returns A function that removes this listener when called.
   */
  once<K extends keyof TEventMap>(
    event: K,
    listener: Listener<TEventMap[K]>,
  ): () => void {
    const wrapped: Listener<TEventMap[K]> = (payload) => {
      unsubscribe();
      listener(payload);
    };
    const unsubscribe = this.on(event, wrapped);
    return unsubscribe;
  }

  /**
   * Emit an event with the given payload.
   *
   * @param event - The event name to emit.
   * @param payload - The data to pass to listeners.
   */
  emit<K extends keyof TEventMap>(event: K, payload: TEventMap[K]): void {
    const set = this.listeners.get(event);
    if (!set) return;
    for (const listener of set) {
      try {
        listener(payload);
      } catch {
        // Swallow listener errors to prevent one bad listener from
        // breaking others or the emitter.
      }
    }
  }

  /**
   * Remove all listeners for a specific event, or all events if no
   * event name is provided.
   *
   * @param event - Optional event name. If omitted, all listeners are removed.
   */
  off<K extends keyof TEventMap>(event?: K): void {
    if (event === undefined) {
      this.listeners.clear();
    } else {
      this.listeners.delete(event);
    }
  }

  /**
   * Remove all listeners and reset the emitter.
   */
  removeAllListeners(): void {
    this.listeners.clear();
  }

  /**
   * Returns the number of listeners registered for a given event.
   *
   * @param event - The event name to check.
   */
  listenerCount<K extends keyof TEventMap>(event: K): number {
    return this.listeners.get(event)?.size ?? 0;
  }
}
