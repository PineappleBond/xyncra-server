import { useRegisterFunctions } from '@xyncra/client-web';
import type { FunctionEntry } from '@xyncra/client-web';
import {
  createClickFunction,
  createInputFunction,
  createSelectFunction,
  createLinkFunction,
} from '../utils/factory';

const registerFunctions: FunctionEntry[] = [
  createInputFunction(
    'pg_register_email_input',
    '在注册页填写邮箱输入框',
    'input[placeholder="邮箱"]',
    ['page:register'],
  ),
  createInputFunction(
    'pg_register_password_input',
    '在注册页填写密码输入框',
    'input[placeholder="至少6位密码，区分大小写"]',
    ['page:register'],
  ),
  createInputFunction(
    'pg_register_confirm_input',
    '在注册页填写确认密码输入框',
    'input[placeholder="确认密码"]',
    ['page:register'],
  ),
  createSelectFunction(
    'pg_register_country_select',
    '在注册页选择国际区号',
    '.ant-select',
    ['page:register'],
  ),
  createInputFunction(
    'pg_register_mobile_input',
    '在注册页填写手机号输入框',
    'input[placeholder="手机号"]',
    ['page:register'],
  ),
  createInputFunction(
    'pg_register_captcha_input',
    '在注册页填写验证码输入框',
    'input[placeholder="验证码"]',
    ['page:register'],
  ),
  createClickFunction(
    'pg_register_captcha_btn',
    '在注册页点击「获取验证码」按钮',
    'button:not([disabled])',
    ['page:register'],
  ),
  createClickFunction(
    'pg_register_submit_btn',
    '在注册页点击注册提交按钮',
    'button[type="primary"]',
    ['page:register'],
  ),
  createLinkFunction(
    'pg_register_login_link',
    '在注册页点击「使用已有账户登录」链接',
    'a[href="/user/login"]',
    ['page:register'],
  ),
];

export function RegisterFunctions() {
  useRegisterFunctions(registerFunctions);
  return null;
}
