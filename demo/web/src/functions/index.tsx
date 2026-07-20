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
import { GetPageDescriptionFunction } from './getPageDescription';
import { GetPageStructureFunction } from './getPageStructure';
import { GetFormDataFunction } from './getFormData';
import { GetTableDataFunction } from './getTableData';
import { ClickElementFunction } from './clickElement';
import { TypeTextFunction } from './typeText';
import { SelectOptionFunction } from './selectOption';
import { DatePickerFunction } from './datePicker';
import { ScrollToFunction } from './scrollTo';
import { WaitForElementFunction } from './waitForElement';
import { ConfirmActionFunction } from './confirmAction';
import { UploadFileFunction } from './uploadFile';
import { TableSearchFunction } from './tableSearch';
import { TableSortFunction } from './tableSort';
import { TableRefreshFunction } from './tableRefresh';
import { FormSubmitFunction } from './formSubmit';
import { FormResetFunction } from './formReset';

export function DemoFunctions() {
  return (
    <>
      <NavigateToFunction />
      <ShowNotificationFunction />
      <HighlightElementFunction />
      <GetCurrentPageFunction />
      <GetPageDescriptionFunction />
      <GetPageStructureFunction />
      <GetFormDataFunction />
      <GetTableDataFunction />
      <ClickElementFunction />
      <TypeTextFunction />
      <SelectOptionFunction />
      <DatePickerFunction />
      <ScrollToFunction />
      <WaitForElementFunction />
      <ConfirmActionFunction />
      <UploadFileFunction />
      <TableSearchFunction />
      <TableSortFunction />
      <TableRefreshFunction />
      <FormSubmitFunction />
      <FormResetFunction />
    </>
  );
}

export { NavigateToFunction } from './navigateTo';
export { ShowNotificationFunction } from './showNotification';
export { HighlightElementFunction } from './highlightElement';
export { GetCurrentPageFunction } from './getCurrentPage';
export { GetPageDescriptionFunction } from './getPageDescription';
export { GetPageStructureFunction } from './getPageStructure';
export { GetFormDataFunction } from './getFormData';
export { GetTableDataFunction } from './getTableData';
export { ClickElementFunction } from './clickElement';
export { TypeTextFunction } from './typeText';
export { SelectOptionFunction } from './selectOption';
export { DatePickerFunction } from './datePicker';
export { ScrollToFunction } from './scrollTo';
export { WaitForElementFunction } from './waitForElement';
export { ConfirmActionFunction } from './confirmAction';
export { UploadFileFunction } from './uploadFile';
export { TableSearchFunction } from './tableSearch';
export { TableSortFunction } from './tableSort';
export { TableRefreshFunction } from './tableRefresh';
export { FormSubmitFunction } from './formSubmit';
export { FormResetFunction } from './formReset';
