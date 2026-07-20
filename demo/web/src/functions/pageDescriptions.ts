export interface PageRegionDesc {
  /** 语义化区块名（非 DOM label） */
  name: string;
  /** 该区块的业务用途 */
  purpose: string;
  /** 该区块关联的 pg_ 函数名列表 */
  functions: string[];
}

export interface PageDescription {
  /** 由路由 pathname 推导的页面标识，如 login */
  page_id: string;
  /** 路由路径，如 /user/login */
  route: string;
  /** 页面中文名 */
  title: string;
  /** 一段中文业务语义描述 */
  summary: string;
  /** 页面核心业务目的 */
  business_goal: string;
  /** 语义区块列表 */
  regions: PageRegionDesc[];
}

/**
 * 页面语义映射表 —— 按路由前缀匹配，提供"页面是什么 + 有哪些业务区块 +
 * 每个区块关联哪些 pg_ 函数"的聚合视图，弥补 get_page_structure 只返回裸
 * DOM 元素、get_current_page 只返回 URL 的不足。
 *
 * key 为路由前缀（与 PageFunctions.tsx 的匹配逻辑保持一致）。
 */
export const pageDescriptions: Record<string, Omit<PageDescription, 'route'>> = {
  '/user/login': {
    page_id: 'login',
    title: '用户登录页',
    summary:
      '系统登录入口。用户通过「账户密码」或「手机号+验证码」两种方式完成身份认证，认证成功后进入工作台。也支持第三方账号（支付宝/淘宝/微博）快捷登录。',
    business_goal: '身份认证',
    regions: [
      {
        name: '登录方式切换区',
        purpose: '在账户密码登录与手机号登录之间切换',
        functions: ['pg_login_tab_account', 'pg_login_tab_mobile'],
      },
      {
        name: '账户密码凭证区',
        purpose: '填写用户名与密码完成账户密码登录',
        functions: [
          'pg_login_username_input',
          'pg_login_password_input',
          'pg_login_remember_checkbox',
        ],
      },
      {
        name: '手机号凭证区',
        purpose: '填写手机号与验证码完成手机登录',
        functions: [
          'pg_login_mobile_input',
          'pg_login_captcha_input',
          'pg_login_captcha_btn',
        ],
      },
      {
        name: '提交与辅助区',
        purpose: '提交登录、找回密码、第三方登录',
        functions: [
          'pg_login_submit_btn',
          'pg_login_forgot_link',
          'pg_login_alipay_icon',
          'pg_login_taobao_icon',
          'pg_login_weibo_icon',
        ],
      },
    ],
  },
  '/user/register': {
    page_id: 'register',
    title: '用户注册页',
    summary:
      '新用户注册入口。填写邮箱、密码、手机号与验证码完成账户创建，注册成功后跳转至注册结果页。',
    business_goal: '账户注册',
    regions: [
      {
        name: '注册表单区',
        purpose: '填写注册所需的账户与联系信息',
        functions: [
          'pg_register_email_input',
          'pg_register_password_input',
          'pg_register_confirm_input',
          'pg_register_country_select',
          'pg_register_mobile_input',
          'pg_register_captcha_input',
          'pg_register_captcha_btn',
        ],
      },
      {
        name: '提交与跳转区',
        purpose: '提交注册或返回登录',
        functions: ['pg_register_submit_btn', 'pg_register_login_link'],
      },
    ],
  },
  '/form/basic-form': {
    page_id: 'basic-form',
    title: '基础表单页',
    summary:
      '用于收集或校验信息的标准表单页。常见场景为创建目标/任务，填写标题、起止日期、描述、客户、邀评人、权重及公开范围后提交。',
    business_goal: '信息收集',
    regions: [
      {
        name: '基础信息区',
        purpose: '填写目标标题、起止日期与描述',
        functions: [
          'pg_basic_form_fill_all',
          'pg_basic_form_title_input',
          'pg_basic_form_date_range',
          'pg_basic_form_desc_textarea',
          'pg_basic_form_standard_textarea',
        ],
      },
      {
        name: '协作信息区',
        purpose: '指定客户、邀评人、权重与公开范围',
        functions: [
          'pg_basic_form_client_input',
          'pg_basic_form_reviewer_input',
          'pg_basic_form_weight_input',
          'pg_basic_form_public_radio',
          'pg_basic_form_public_user_select',
        ],
      },
      {
        name: '提交区',
        purpose: '提交表单',
        functions: ['pg_basic_form_submit'],
      },
    ],
  },
  '/list/table-list': {
    page_id: 'table-list',
    title: '表格列表页',
    summary:
      '规则管理表格页。支持新建规则、搜索过滤、刷新、批量删除/审批，以及查看单行详情（配置/订阅）。',
    business_goal: '数据表格管理',
    regions: [
      {
        name: '工具栏区',
        purpose: '新建规则、搜索、刷新',
        functions: [
          'pg_table_list_new_btn',
          'pg_table_list_search_input',
          'pg_table_list_refresh',
        ],
      },
      {
        name: '批量操作区',
        purpose: '对勾选的多行执行批量删除/审批',
        functions: [
          'pg_table_list_batch_delete',
          'pg_table_list_batch_approve',
          'pg_table_list_row_select',
        ],
      },
      {
        name: '行操作区',
        purpose: '查看单行配置或订阅告警',
        functions: [
          'pg_table_list_row_config',
          'pg_table_list_row_subscribe',
          'pg_table_list_drawer_close',
        ],
      },
      {
        name: '分页区',
        purpose: '翻页与调整每页条数',
        functions: ['pg_table_list_next_page', 'pg_table_list_page_size'],
      },
    ],
  },
  '/list/basic-list': {
    page_id: 'basic-list',
    title: '基础列表页',
    summary:
      '任务列表页。支持按状态（全部/进行中/等待中）筛选、关键词搜索、编辑/删除单行任务、批量添加新任务。',
    business_goal: '任务列表管理',
    regions: [
      {
        name: '筛选与搜索区',
        purpose: '按状态分段筛选与关键词搜索',
        functions: [
          'pg_basic_list_segment_all',
          'pg_basic_list_segment_progress',
          'pg_basic_list_segment_waiting',
          'pg_basic_list_search_input',
        ],
      },
      {
        name: '行操作区',
        purpose: '编辑或删除列表项',
        functions: [
          'pg_basic_list_row_edit',
          'pg_basic_list_row_delete',
          'pg_basic_list_add_btn',
        ],
      },
      {
        name: '删除确认弹窗',
        purpose: '确认或取消删除操作',
        functions: [
          'pg_basic_list_confirm_delete',
          'pg_basic_list_confirm_cancel',
        ],
      },
      {
        name: '分页区',
        purpose: '翻到下一页',
        functions: ['pg_basic_list_next_page'],
      },
    ],
  },
  '/dashboard/analysis': {
    page_id: 'dashboard-analysis',
    title: '分析仪表盘',
    summary:
      '销售与访问数据可视化分析页。可按销售额/访问量切换、按时间维度（今日/本周/本月/本年）查询、自定义日期范围，并按渠道（全部/线上/门店）筛选。',
    business_goal: '数据可视化分析',
    regions: [
      {
        name: '指标切换与时间段区',
        purpose: '切换销售额/访问量，选择查询时间维度',
        functions: [
          'pg_dash_analysis_tab_sales',
          'pg_dash_analysis_tab_visits',
          'pg_dash_analysis_date_today',
          'pg_dash_analysis_date_week',
          'pg_dash_analysis_date_month',
          'pg_dash_analysis_date_year',
          'pg_dash_analysis_date_range',
        ],
      },
      {
        name: '渠道筛选区',
        purpose: '按销售渠道筛选数据',
        functions: [
          'pg_dash_analysis_segment_all',
          'pg_dash_analysis_segment_online',
          'pg_dash_analysis_segment_store',
        ],
      },
    ],
  },
  '/account/settings': {
    page_id: 'account-settings',
    title: '账户设置页',
    summary:
      '用户账户管理中心。分为基本设置、安全设置、账号绑定、新消息通知四个模块，可维护个人资料、登录安全、第三方绑定与通知偏好。',
    business_goal: '账户资料管理',
    regions: [
      {
        name: '设置导航区',
        purpose: '切换四个设置模块',
        functions: [
          'pg_acc_settings_menu_basic',
          'pg_acc_settings_menu_security',
          'pg_acc_settings_menu_binding',
          'pg_acc_settings_menu_notification',
        ],
      },
      {
        name: '基本资料区',
        purpose: '维护邮箱、昵称、简介、地区与头像',
        functions: [
          'pg_acc_settings_email_input',
          'pg_acc_settings_nickname_input',
          'pg_acc_settings_bio_textarea',
          'pg_acc_settings_country_select',
          'pg_acc_settings_address_input',
          'pg_acc_settings_avatar_upload',
          'pg_acc_settings_submit',
        ],
      },
      {
        name: '安全设置区',
        purpose: '修改密码与绑定手机',
        functions: ['pg_acc_settings_security_password_edit', 'pg_acc_settings_security_phone_edit'],
      },
      {
        name: '账号绑定区',
        purpose: '绑定第三方账号',
        functions: [
          'pg_acc_settings_binding_taobao',
          'pg_acc_settings_binding_alipay',
          'pg_acc_settings_binding_dingtalk',
        ],
      },
      {
        name: '新消息通知区',
        purpose: '开关各类消息通知',
        functions: [
          'pg_acc_settings_notification_user_switch',
          'pg_acc_settings_notification_system_switch',
          'pg_acc_settings_notification_task_switch',
        ],
      },
    ],
  },
  '/chatbot': {
    page_id: 'chatbot',
    title: '智能对话页',
    summary:
      'AI 聊天机器人对话页。可发起新对话、在多个历史对话间切换、删除对话，并向 AI 发送消息。',
    business_goal: 'AI 对话',
    regions: [
      {
        name: '对话列表区',
        purpose: '新建、选择或删除历史对话',
        functions: [
          'pg_chatbot_new_conversation',
          'pg_chatbot_conversation_item_1',
          'pg_chatbot_conversation_item_2',
          'pg_chatbot_conversation_item_3',
          'pg_chatbot_conversation_item_4',
          'pg_chatbot_conversation_item_5',
          'pg_chatbot_conversation_item_6',
          'pg_chatbot_conversation_item_7',
          'pg_chatbot_conversation_item_8',
          'pg_chatbot_conversation_item_9',
          'pg_chatbot_conversation_delete',
        ],
      },
      {
        name: '消息输入区',
        purpose: '向 AI 发送消息',
        functions: ['pg_chatbot_send_input'],
      },
    ],
  },
  '/list/search': {
    page_id: 'list-search',
    title: '搜索列表页',
    summary:
      '全局搜索页。可在文章/项目/应用三个分类间切换，并输入关键词执行搜索。',
    business_goal: '内容搜索',
    regions: [
      {
        name: '分类切换区',
        purpose: '切换搜索分类',
        functions: [
          'pg_search_tab_articles',
          'pg_search_tab_projects',
          'pg_search_tab_applications',
        ],
      },
      {
        name: '搜索输入区',
        purpose: '输入关键词并搜索',
        functions: ['pg_search_input', 'pg_search_btn'],
      },
    ],
  },
  '/form/step-form': {
    page_id: 'step-form',
    title: '分步表单页',
    summary:
      '转账类多步骤表单。第一步填写付款/收款账户、收款人、金额，第二步确认并提交。',
    business_goal: '分步信息收集',
    regions: [
      {
        name: '第一步凭证区',
        purpose: '填写转账双方账户与金额',
        functions: [
          'pg_step_form_pay_account',
          'pg_step_form_receive_account',
          'pg_step_form_receiver_name',
          'pg_step_form_amount',
          'pg_step_form_next',
        ],
      },
      {
        name: '第二步确认区',
        purpose: '确认并提交转账',
        functions: ['pg_step_form_confirm', 'pg_step_form_transfer_again'],
      },
    ],
  },
  '/form/advanced-form': {
    page_id: 'advanced-form',
    title: '高级表单页',
    summary:
      '复杂仓库/任务配置表单。包含仓库基本信息、生效日期、类型，以及任务执行人、责任人等字段，支持可编辑表格。',
    business_goal: '复杂配置表单',
    regions: [
      {
        name: '仓库配置区',
        purpose: '填写仓库名、域名、管理员、审批人、生效日期与类型',
        functions: [
          'pg_adv_form_name_input',
          'pg_adv_form_url_input',
          'pg_adv_form_owner_select',
          'pg_adv_form_approver_select',
          'pg_adv_form_date_range',
          'pg_adv_form_type_select',
        ],
      },
      {
        name: '任务配置区',
        purpose: '填写任务名、描述、执行人与责任人',
        functions: [
          'pg_adv_form_task_name',
          'pg_adv_form_task_desc',
          'pg_adv_form_task_owner',
          'pg_adv_form_task_approver',
        ],
      },
      {
        name: '提交区',
        purpose: '提交高级表单',
        functions: ['pg_adv_form_submit'],
      },
    ],
  },
  '/profile/advanced': {
    page_id: 'profile-advanced',
    title: '高级详情页',
    summary:
      '用户/项目高级详情页。展示多维度信息，提供主操作与下拉操作菜单，以及详情/规则等 Tab 切换。',
    business_goal: '详情展示与操作',
    regions: [
      {
        name: '操作区',
        purpose: '执行页面主操作与下拉菜单',
        functions: [
          'pg_profile_adv_action_1',
          'pg_profile_adv_action_2',
          'pg_profile_adv_dropdown',
        ],
      },
      {
        name: '信息切换区',
        purpose: '切换详情/规则 Tab',
        functions: [
          'pg_profile_adv_tab_detail',
          'pg_profile_adv_tab_rule',
        ],
      },
    ],
  },
  '/account/center': {
    page_id: 'account-center',
    title: '个人中心页',
    summary:
      '用户个人主页。展示个人资料与标签，可在文章/应用/项目三个维度间切换查看。',
    business_goal: '个人资料展示',
    regions: [
      {
        name: '内容切换区',
        purpose: '切换文章/应用/项目 Tab',
        functions: [
          'pg_acc_center_tab_articles',
          'pg_acc_center_tab_applications',
          'pg_acc_center_tab_projects',
        ],
      },
      {
        name: '标签区',
        purpose: '查看与添加个人标签',
        functions: ['pg_acc_center_tag_input', 'pg_acc_center_add_tag'],
      },
    ],
  },
  '/list/card-list': {
    page_id: 'card-list',
    title: '卡片列表页',
    summary: '产品/资源卡片墙。提供快速开始、产品简介、产品文档等引导链接，并支持新增产品。',
    business_goal: '资源卡片展示',
    regions: [
      {
        name: '引导链接区',
        purpose: '跳转文档与引导页',
        functions: [
          'pg_card_list_quick_start',
          'pg_card_list_product_intro',
          'pg_card_list_product_docs',
        ],
      },
      {
        name: '操作区',
        purpose: '新增产品',
        functions: ['pg_card_list_add_product'],
      },
    ],
  },
  '/dashboard/workplace': {
    page_id: 'workplace',
    title: '工作台页',
    summary: '用户工作台首页。聚合动态项目、团队成员与快捷操作入口。',
    business_goal: '工作概览',
    regions: [
      {
        name: '快捷操作区',
        purpose: '执行常用操作',
        functions: [
          'pg_workplace_link_op1',
          'pg_workplace_link_op2',
          'pg_workplace_link_op3',
          'pg_workplace_link_op4',
          'pg_workplace_link_op5',
          'pg_workplace_link_op6',
        ],
      },
    ],
  },
  '/result': {
    page_id: 'result',
    title: '结果页',
    summary: '操作结果反馈页。展示提交成功等信息，提供返回列表、查看项目、打印等后续操作。',
    business_goal: '结果反馈',
    regions: [
      {
        name: '操作区',
        purpose: '返回列表、查看项目或打印',
        functions: [
          'pg_result_back_list',
          'pg_result_view_project',
          'pg_result_print',
          'pg_result_urge',
        ],
      },
    ],
  },
  '/exception': {
    page_id: 'exception',
    title: '异常页',
    summary: '404/403/500 等错误提示页。提供返回首页的入口。',
    business_goal: '错误提示',
    regions: [
      {
        name: '操作区',
        purpose: '返回首页',
        functions: ['pg_exception_back_home'],
      },
    ],
  },
  '/welcome': {
    page_id: 'welcome',
    title: '欢迎页',
    summary: '系统欢迎与文档引导页。提供 umi / Ant Design / Pro Components 的学习入口卡片。',
    business_goal: '文档引导',
    regions: [
      {
        name: '信息卡片区',
        purpose: '跳转学习资源',
        functions: [
          'pg_welcome_card_umi',
          'pg_welcome_card_antd',
          'pg_welcome_card_procomponents',
        ],
      },
    ],
  },
  '/user/register-result': {
    page_id: 'register-result',
    title: '注册结果页',
    summary: '注册完成反馈页。提示激活邮件已发送，引导查看邮箱或返回首页。',
    business_goal: '注册结果反馈',
    regions: [
      {
        name: '操作区',
        purpose: '查看邮箱或返回首页',
        functions: [
          'pg_register_result_check_email',
          'pg_register_result_back_home',
        ],
      },
    ],
  },
};

/** 根据路由 pathname 匹配页面语义描述 */
export function matchPageDescription(pathname: string): PageDescription | null {
  for (const [prefix, desc] of Object.entries(pageDescriptions)) {
    if (pathname.startsWith(prefix)) {
      return { ...desc, route: pathname };
    }
  }
  return null;
}
