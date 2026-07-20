/**
 * DemoFunctions — barrel component that registers all demo functions.
 *
 * Mount this once at the app root (inside XyncraProvider) so that every
 * demo function is available for the Xyncra server to invoke.
 *
 * @module
 */

import { NavigateToFunction } from './navigateTo';
import { GetCurrentPageFunction } from './getCurrentPage';
import { GetPageDescriptionFunction } from './getPageDescription';
import { GetPageStructureFunction } from './getPageStructure';
import { GetFormDataFunction } from './getFormData';
import { GetTableDataFunction } from './getTableData';

export function DemoFunctions() {
  return (
    <>
      <NavigateToFunction />
      <GetCurrentPageFunction />
      <GetPageDescriptionFunction />
      <GetPageStructureFunction />
      <GetFormDataFunction />
      <GetTableDataFunction />
    </>
  );
}

export { NavigateToFunction } from './navigateTo';
export { GetCurrentPageFunction } from './getCurrentPage';
export { GetPageDescriptionFunction } from './getPageDescription';
export { GetPageStructureFunction } from './getPageStructure';
export { GetFormDataFunction } from './getFormData';
export { GetTableDataFunction } from './getTableData';
