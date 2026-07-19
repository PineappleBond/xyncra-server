import { useRegisterFunctions } from '@xyncra/client-web';
import type { FunctionEntry } from '@xyncra/client-web';
import {
  createClickFunction,
  createInputFunction,
  createSelectFunction,
  createTextareaFunction,
  createLinkFunction,
  createToggleFunction,
} from '../utils/factory';

const accountSettingsFunctions: FunctionEntry[] = [
  createClickFunction(
    'pg_acc_settings_menu_basic',
    '在账户设置页切换到「基本设置」菜单',
    '.ant-menu-item:contains("基本设置")',
    ['page:account-settings'],
  ),
  createClickFunction(
    'pg_acc_settings_menu_security',
    '在账户设置页切换到「安全设置」菜单',
    '.ant-menu-item:contains("安全设置")',
    ['page:account-settings'],
  ),
  createClickFunction(
    'pg_acc_settings_menu_binding',
    '在账户设置页切换到「账号绑定」菜单',
    '.ant-menu-item:contains("账号绑定")',
    ['page:account-settings'],
  ),
  createClickFunction(
    'pg_acc_settings_menu_notification',
    '在账户设置页切换到「新消息通知」菜单',
    '.ant-menu-item:contains("新消息通知")',
    ['page:account-settings'],
  ),
  createInputFunction(
    'pg_acc_settings_email_input',
    '在账户设置页填写邮箱输入框',
    'input[name="email"]',
    ['page:account-settings'],
  ),
  createInputFunction(
    'pg_acc_settings_nickname_input',
    '在账户设置页填写昵称输入框',
    'input[name="name"]',
    ['page:account-settings'],
  ),
  createTextareaFunction(
    'pg_acc_settings_bio_textarea',
    '在账户设置页填写个人简介文本框',
    'textarea[name="profile"]',
    ['page:account-settings'],
  ),
  createSelectFunction(
    'pg_acc_settings_country_select',
    '在账户设置页选择国家/地区',
    '.ant-select',
    ['page:account-settings'],
  ),
  createInputFunction(
    'pg_acc_settings_address_input',
    '在账户设置页填写街道地址',
    'input[name="address"]',
    ['page:account-settings'],
  ),
  createClickFunction(
    'pg_acc_settings_avatar_upload',
    '在账户设置页点击「更换头像」按钮',
    'button:contains("更换头像")',
    ['page:account-settings'],
  ),
  createClickFunction(
    'pg_acc_settings_submit',
    '在账户设置页提交更新基本信息',
    'button[type="submit"]',
    ['page:account-settings'],
  ),
  createLinkFunction(
    'pg_acc_settings_security_password_edit',
    '在账户设置页安全设置中点击「修改」密码',
    '.ant-list-item:contains("账户密码") a:contains("修改")',
    ['page:account-settings'],
  ),
  createLinkFunction(
    'pg_acc_settings_security_phone_edit',
    '在账户设置页安全设置中点击「修改」手机',
    '.ant-list-item:contains("密保手机") a:contains("修改")',
    ['page:account-settings'],
  ),
  createLinkFunction(
    'pg_acc_settings_binding_taobao',
    '在账户设置页账号绑定中点击「绑定」淘宝',
    '.ant-list-item:contains("绑定淘宝") a',
    ['page:account-settings'],
  ),
  createLinkFunction(
    'pg_acc_settings_binding_alipay',
    '在账户设置页账号绑定中点击「绑定」支付宝',
    '.ant-list-item:contains("绑定支付宝") a',
    ['page:account-settings'],
  ),
  createLinkFunction(
    'pg_acc_settings_binding_dingtalk',
    '在账户设置页账号绑定中点击「绑定」钉钉',
    '.ant-list-item:contains("绑定钉钉") a',
    ['page:account-settings'],
  ),
  createToggleFunction(
    'pg_acc_settings_notification_user_switch',
    '在账户设置页切换「用户消息」通知开关',
    '.ant-list-item:contains("用户消息") .ant-switch',
    ['page:account-settings'],
  ),
  createToggleFunction(
    'pg_acc_settings_notification_system_switch',
    '在账户设置页切换「系统消息」通知开关',
    '.ant-list-item:contains("系统消息") .ant-switch',
    ['page:account-settings'],
  ),
  createToggleFunction(
    'pg_acc_settings_notification_task_switch',
    '在账户设置页切换「待办任务」通知开关',
    '.ant-list-item:contains("待办任务") .ant-switch',
    ['page:account-settings'],
  ),
];

export function AccountSettingsFunctions() {
  useRegisterFunctions(accountSettingsFunctions);
  return null;
}
