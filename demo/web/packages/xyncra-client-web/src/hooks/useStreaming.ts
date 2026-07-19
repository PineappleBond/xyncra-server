/**
 * @packageDocumentation
 * useStreaming — React hook for streaming text state (D-7).
 *
 * Accumulates streaming text chunks per streamId using a ref-based buffer
 * and batches display updates via requestAnimationFrame for smooth rendering.
 *
 * Design decisions:
 * - D-7: useRef for accumulated text, useState for displayed text.
 * - isDone triggers delayed cleanup (500ms) to wait for the final message.
 * - requestAnimationFrame coalesces rapid text events into a single re-render.
 *
 * @module
 */

import { useEffect, useRef, useState } from 'react';
import { useXyncra } from './useXyncra';

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

export interface UseStreamingReturn {
  /** The current accumulated streaming text (empty when not streaming). */
  streamingText: string;
  /** Whether a stream is currently active. */
  isStreaming: boolean;
  /** The ID of the active stream, or null when idle. */
  currentStreamID: string | null;
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

/** Delay before clearing a completed stream's state (ms). */
const STREAM_CLEANUP_DELAY = 500;

// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

/**
 * Tracks the currently active streaming text, aggregating chunks by streamId
 * and flushing display updates on the next animation frame.
 */
export function useStreaming(): UseStreamingReturn {
  const { eventEmitter } = useXyncra();

  const [streamingText, setStreamingText] = useState('');
  const [isStreaming, setIsStreaming] = useState(false);
  const [currentStreamID, setCurrentStreamID] = useState<string | null>(null);

  // Accumulated text per streamId (mutable, not tied to render cycle).
  const accumulatedRef = useRef<Map<string, string>>(new Map());
  // The currently active stream ID (ref for stale-closure-safe comparison).
  const activeStreamRef = useRef<string | null>(null);
  // Pending text to flush on next rAF.
  const pendingTextRef = useRef('');
  // rAF handle for cancellation.
  const rafRef = useRef<number | null>(null);
  // Cleanup timer handles per streamId.
  const cleanupTimersRef = useRef<Map<string, ReturnType<typeof setTimeout>>>(
    new Map(),
  );

  useEffect(() => {
    /**
     * Flush pending text to React state on the next animation frame.
     * Coalesces rapid stream:text events into a single re-render.
     */
    const scheduleUpdate = (): void => {
      if (rafRef.current !== null) return; // Already scheduled.
      rafRef.current = requestAnimationFrame(() => {
        rafRef.current = null;
        setStreamingText(pendingTextRef.current);
      });
    };

    /**
     * Clean up a completed stream after a delay, allowing time for the
     * final persisted message to arrive via message:added.
     */
    const scheduleCleanup = (streamId: string): void => {
      // Clear any existing cleanup timer for this stream.
      const existing = cleanupTimersRef.current.get(streamId);
      if (existing !== undefined) {
        clearTimeout(existing);
      }

      const timer = setTimeout(() => {
        accumulatedRef.current.delete(streamId);
        cleanupTimersRef.current.delete(streamId);

        // Only reset display state if this stream is still the active one.
        if (activeStreamRef.current === streamId) {
          activeStreamRef.current = null;
          setCurrentStreamID(null);
          setIsStreaming(false);
          pendingTextRef.current = '';
          setStreamingText('');
        }
      }, STREAM_CLEANUP_DELAY);

      cleanupTimersRef.current.set(streamId, timer);
    };

    // -- Subscribe to stream:text --
    const unsubText = eventEmitter.on('stream:text', ({ streamId, text }) => {
      // Cancel any pending cleanup for this stream (e.g. a prior isDone that
      // was superseded by more text — unlikely but defensive).
      const existingTimer = cleanupTimersRef.current.get(streamId);
      if (existingTimer !== undefined) {
        clearTimeout(existingTimer);
        cleanupTimersRef.current.delete(streamId);
      }

      const acc = accumulatedRef.current;
      const updated = (acc.get(streamId) ?? '') + text;
      acc.set(streamId, updated);

      activeStreamRef.current = streamId;
      setCurrentStreamID(streamId);
      setIsStreaming(true);

      pendingTextRef.current = updated;
      scheduleUpdate();
    });

    // -- Subscribe to stream:done --
    const unsubDone = eventEmitter.on('stream:done', ({ streamId }) => {
      setIsStreaming(false);
      scheduleCleanup(streamId);
    });

    return () => {
      unsubText();
      unsubDone();

      // Cancel pending rAF.
      if (rafRef.current !== null) {
        cancelAnimationFrame(rafRef.current);
        rafRef.current = null;
      }

      // Cancel all pending cleanup timers.
      for (const timer of cleanupTimersRef.current.values()) {
        clearTimeout(timer);
      }
      cleanupTimersRef.current.clear();
    };
  }, [eventEmitter]);

  return { streamingText, isStreaming, currentStreamID };
}
