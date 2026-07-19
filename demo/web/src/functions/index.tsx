/**
 * DemoFunctions — barrel component that registers all demo functions.
 *
 * Mount this once at the app root (inside XyncraProvider) so that every
 * demo function is available for the Xyncra server to invoke.
 *
 * @module
 */

import { NavigateToFunction } from './navigateTo';
import { ShowNotificationFunction } from './showNotification';
import { HighlightElementFunction } from './highlightElement';
import { GetCurrentPageFunction } from './getCurrentPage';

export function DemoFunctions() {
  return (
    <>
      <NavigateToFunction />
      <ShowNotificationFunction />
      <HighlightElementFunction />
      <GetCurrentPageFunction />
    </>
  );
}

export { NavigateToFunction } from './navigateTo';
export { ShowNotificationFunction } from './showNotification';
export { HighlightElementFunction } from './highlightElement';
export { GetCurrentPageFunction } from './getCurrentPage';
