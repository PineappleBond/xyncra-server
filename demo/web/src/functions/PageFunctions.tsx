import { useLocation } from '@umijs/max';
import React from 'react';
import { LoginFunctions } from './pages/login.functions';
import { RegisterFunctions } from './pages/register.functions';
import { BasicFormFunctions } from './pages/basic-form.functions';
import { TableListFunctions } from './pages/table-list.functions';
import { BasicListFunctions } from './pages/basic-list.functions';
import { DashboardAnalysisFunctions } from './pages/dashboard-analysis.functions';
import { AccountSettingsFunctions } from './pages/account-settings.functions';
import { ChatbotFunctions } from './pages/chatbot.functions';
import { ListSearchFunctions } from './pages/list-search.functions';
import { StepFormFunctions } from './pages/step-form.functions';
import { AdvancedFormFunctions } from './pages/advanced-form.functions';
import { ProfileAdvancedFunctions } from './pages/profile-advanced.functions';
import { AccountCenterFunctions } from './pages/account-center.functions';
import { CardListFunctions } from './pages/card-list.functions';
import { WorkplaceFunctions } from './pages/workplace.functions';
import { ResultFunctions } from './pages/result.functions';
import { ExceptionFunctions } from './pages/exception.functions';
import { WelcomeFunctions } from './pages/welcome.functions';
import { RegisterResultFunctions } from './pages/register-result.functions';
import { MonitorFunctions } from './pages/monitor.functions';
import { ProfileBasicFunctions } from './pages/profile-basic.functions';
import { AdminFunctions } from './pages/admin.functions';

function matchPageFunctions(pathname: string): React.ReactElement | null {
  if (pathname.startsWith('/user/login')) return <LoginFunctions />;
  if (pathname.startsWith('/user/register')) return <RegisterFunctions />;
  if (pathname.startsWith('/user/register-result')) return <RegisterResultFunctions />;
  if (pathname.startsWith('/form/basic-form')) return <BasicFormFunctions />;
  if (pathname.startsWith('/form/step-form')) return <StepFormFunctions />;
  if (pathname.startsWith('/form/advanced-form')) return <AdvancedFormFunctions />;
  if (pathname.startsWith('/list/table-list')) return <TableListFunctions />;
  if (pathname.startsWith('/list/basic-list')) return <BasicListFunctions />;
  if (pathname.startsWith('/list/card-list')) return <CardListFunctions />;
  if (pathname.startsWith('/list/search')) return <ListSearchFunctions />;
  if (pathname.startsWith('/dashboard/analysis')) return <DashboardAnalysisFunctions />;
  if (pathname.startsWith('/dashboard/monitor')) return <MonitorFunctions />;
  if (pathname.startsWith('/dashboard/workplace')) return <WorkplaceFunctions />;
  if (pathname.startsWith('/account/center')) return <AccountCenterFunctions />;
  if (pathname.startsWith('/account/settings')) return <AccountSettingsFunctions />;
  if (pathname.startsWith('/chatbot')) return <ChatbotFunctions />;
  if (pathname.startsWith('/profile/basic')) return <ProfileBasicFunctions />;
  if (pathname.startsWith('/profile/advanced')) return <ProfileAdvancedFunctions />;
  if (pathname.startsWith('/result/')) return <ResultFunctions />;
  if (pathname.startsWith('/exception/')) return <ExceptionFunctions />;
  if (pathname.startsWith('/welcome')) return <WelcomeFunctions />;
  if (pathname.startsWith('/admin/')) return <AdminFunctions />;
  return null;
}

export function PageFunctions() {
  const location = useLocation();
  return matchPageFunctions(location.pathname);
}
