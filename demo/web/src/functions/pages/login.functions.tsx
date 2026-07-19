import { useRegisterFunctions } from '@xyncra/client-web';
import type { FunctionEntry } from '@xyncra/client-web';
import {
  createClickFunction,
  createInputFunction,
  createCheckboxFunction,
  createLinkFunction,
  createTabFunction,
} from '../utils/factory';

const loginFunctions: FunctionEntry[] = [
  createTabFunction(
    'pg_login_tab_account',
    '在登录页切换到「账户密码登录」Tab',
    '账户密码登录',
    '.ant-tabs',
    ['page:login'],
  ),
  createTabFunction(
    'pg_login_tab_mobile',
    '在登录页切换到「手机号登录」Tab',
    '手机号登录',
    '.ant-tabs',
    ['page:login'],
  ),
  createInputFunction(
    'pg_login_username_input',
    '在登录页填写用户名输入框',
    'input[name="username"]',
    ['page:login'],
  ),
  createInputFunction(
    'pg_login_password_input',
    '在登录页填写密码输入框',
    'input[name="password"]',
    ['page:login'],
  ),
  createInputFunction(
    'pg_login_mobile_input',
    '在登录页填写手机号输入框',
    'input[name="mobile"]',
    ['page:login'],
  ),
  createInputFunction(
    'pg_login_captcha_input',
    '在登录页填写验证码输入框',
    'input[name="captcha"]',
    ['page:login'],
  ),
  createClickFunction(
    'pg_login_captcha_btn',
    '在登录页点击「获取验证码」按钮',
    '.ant-form-item .ant-btn:not(.ant-btn-primary)',
    ['page:login'],
  ),
  createClickFunction(
    'pg_login_submit_btn',
    '在登录页点击登录提交按钮',
    '.ant-pro-form-login-container button[type="submit"]',
    ['page:login'],
  ),
  createCheckboxFunction(
    'pg_login_remember_checkbox',
    '在登录页勾选/取消「自动登录」复选框',
    '.ant-checkbox-input',
    ['page:login'],
  ),
  createLinkFunction(
    'pg_login_forgot_link',
    '在登录页点击「忘记密码」链接',
    'a:has(span)',
    ['page:login'],
  ),
  createClickFunction(
    'pg_login_alipay_icon',
    '在登录页点击支付宝第三方登录图标',
    'span[aria-label="alipay"]',
    ['page:login'],
  ),
  createClickFunction(
    'pg_login_taobao_icon',
    '在登录页点击淘宝第三方登录图标',
    'span[aria-label="taobao"]',
    ['page:login'],
  ),
  createClickFunction(
    'pg_login_weibo_icon',
    '在登录页点击微博第三方登录图标',
    'span[aria-label="weibo"]',
    ['page:login'],
  ),
  createClickFunction(
    'pg_login_alert_close',
    '在登录页关闭错误提示 Alert',
    '.ant-alert .ant-alert-close-icon',
    ['page:login'],
  ),
];

export function LoginFunctions() {
  useRegisterFunctions(loginFunctions);
  return null;
}
