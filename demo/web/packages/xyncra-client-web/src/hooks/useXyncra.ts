/**
 * @packageDocumentation
 * useXyncra — core hook for accessing the XyncraContext.
 *
 * Returns the XyncraContextValue provided by the nearest XyncraProvider.
 * Throws if used outside of a XyncraProvider.
 *
 * @module
 */

import { useContext } from 'react';
import {
  XyncraContext,
  type XyncraContextValue,
} from '../context/XyncraProvider';

/**
 * Access the Xyncra client, connection status, function registry, and
 * event emitter from the nearest XyncraProvider.
 *
 * @throws If called outside of a XyncraProvider.
 */
export function useXyncra(): XyncraContextValue {
  const context = useContext(XyncraContext);
  if (context === null) {
    throw new Error(
      'useXyncra must be used within a <XyncraProvider>. ' +
        'Wrap your component tree with <XyncraProvider> before using this hook.',
    );
  }
  return context;
}
