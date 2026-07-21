/**
 * Page semantic descriptions for vue-pure-admin demo.
 * Maps route prefixes to page descriptions with associated pg_* functions.
 */

export interface PageRegionDesc {
  name: string
  purpose: string
  functions: string[]
}

export interface PageDescription {
  page_id: string
  route: string
  title: string
  summary: string
  business_goal: string
  regions: PageRegionDesc[]
}

const pageDescriptions: Record<string, Omit<PageDescription, 'route'>> = {
  '/login': {
    page_id: 'login',
    title: '用户登录页',
    summary: '系统登录入口。用户通过账户密码或手机号验证码完成身份认证。',
    business_goal: '身份认证',
    regions: [
      {
        name: '登录表单区',
        purpose: '填写用户名与密码完成登录',
        functions: ['pg_login_tab_account', 'pg_login_tab_mobile', 'pg_login_submit_btn'],
      },
    ],
  },
  '/welcome': {
    page_id: 'welcome',
    title: '欢迎页',
    summary: '系统首页，展示数据统计、分析概览、解决概率和最新动态。',
    business_goal: '工作概览',
    regions: [
      {
        name: '数据统计区',
        purpose: '展示关键指标和趋势图',
        functions: [],
      },
    ],
  },
  '/form/index': {
    page_id: 'schema-form',
    title: '表单页',
    summary: '基于 JSON Schema 的动态表单页，支持多种字段类型和校验。',
    business_goal: '信息收集',
    regions: [
      {
        name: '表单区',
        purpose: '填写和提交表单数据',
        functions: ['pg_schema_form_submit'],
      },
    ],
  },
  '/list/card': {
    page_id: 'card-list',
    title: '卡片列表页',
    summary: '产品卡片列表页，支持搜索、新建产品、查看详情和删除。',
    business_goal: '产品管理',
    regions: [
      {
        name: '工具栏区',
        purpose: '新建产品和搜索',
        functions: ['pg_card_list_add_product', 'pg_card_list_search'],
      },
      {
        name: '卡片列表区',
        purpose: '浏览和管理产品卡片',
        functions: ['pg_card_list_view_detail', 'pg_card_list_refresh'],
      },
    ],
  },
  '/result/success': {
    page_id: 'result-success',
    title: '成功结果页',
    summary: '操作成功反馈页。展示提交成功信息，提供返回列表、查看项目、打印等后续操作。',
    business_goal: '结果反馈',
    regions: [
      {
        name: '操作区',
        purpose: '返回列表、查看项目或打印',
        functions: ['pg_result_success_go_back', 'pg_result_success_back_to_list', 'pg_result_success_view_project', 'pg_result_success_print'],
      },
    ],
  },
  '/result/fail': {
    page_id: 'result-fail',
    title: '失败结果页',
    summary: '操作失败反馈页。展示失败信息和原因。',
    business_goal: '结果反馈',
    regions: [],
  },
  '/guide': {
    page_id: 'guide',
    title: '引导页',
    summary: '功能引导页，通过 intro.js 或 el-tour 引导用户了解系统功能。',
    business_goal: '功能引导',
    regions: [
      {
        name: '引导操作区',
        purpose: '启动引导流程',
        functions: ['pg_guide_start_guide'],
      },
    ],
  },
  '/table/index': {
    page_id: 'pure-table',
    title: '表格页',
    summary: '基于 Element Plus 的数据表格页，支持搜索、刷新、新增行等操作。',
    business_goal: '数据表格管理',
    regions: [
      {
        name: '工具栏区',
        purpose: '搜索、刷新和新增',
        functions: ['pg_pure_table_search', 'pg_pure_table_refresh', 'pg_pure_table_add_row'],
      },
    ],
  },
  '/tabs/index': {
    page_id: 'tabs',
    title: '标签页',
    summary: '标签页管理，支持多标签切换和详情查看。',
    business_goal: '多标签管理',
    regions: [
      {
        name: '标签切换区',
        purpose: '切换不同标签页',
        functions: ['pg_tabs_switch_tab'],
      },
    ],
  },
  '/account-settings': {
    page_id: 'account-settings',
    title: '账户设置页',
    summary: '用户账户管理中心。分为个人信息、偏好设置、安全日志、账户管理四个模块。',
    business_goal: '账户资料管理',
    regions: [
      {
        name: '设置导航区',
        purpose: '切换四个设置模块',
        functions: ['pg_account_settings_switch_pane'],
      },
      {
        name: '设置内容区',
        purpose: '维护个人资料和偏好',
        functions: ['pg_account_settings_set_field_value'],
      },
    ],
  },
  // Pages that may be created as stubs
  '/form/advanced': {
    page_id: 'advanced-form',
    title: '高级表单页',
    summary: '复杂配置表单，包含仓库信息、生效日期、任务字段等。',
    business_goal: '复杂配置表单',
    regions: [
      {
        name: '表单区',
        purpose: '填写高级表单字段',
        functions: ['pg_advanced_form_submit'],
      },
    ],
  },
  '/form/step': {
    page_id: 'step-form',
    title: '分步表单页',
    summary: '多步骤表单，分步填写信息并确认提交。',
    business_goal: '分步信息收集',
    regions: [
      {
        name: '步骤操作区',
        purpose: '填写当前步骤并进入下一步',
        functions: ['pg_step_form_next', 'pg_step_form_submit'],
      },
    ],
  },
  '/list/search': {
    page_id: 'list-search',
    title: '搜索列表页',
    summary: '支持 Tab 分类切换和关键词搜索的列表页。',
    business_goal: '内容搜索',
    regions: [
      {
        name: '搜索区',
        purpose: '切换分类和输入搜索关键词',
        functions: ['pg_list_search_tab', 'pg_list_search_input'],
      },
    ],
  },
  '/account/center': {
    page_id: 'account-center',
    title: '个人中心页',
    summary: '用户个人主页，展示个人资料与标签，支持 Tab 切换。',
    business_goal: '个人资料展示',
    regions: [
      {
        name: '内容切换区',
        purpose: '切换文章/应用/项目 Tab（tab 参数传 articles/applications/projects）',
        functions: ['pg_account_center_switchTab'],
      },
      {
        name: '标签区',
        purpose: '查看与添加个人标签（addTag 直接添加标签，setTagInput 仅设置输入框值）',
        functions: ['pg_account_center_addTag', 'pg_account_center_setTagInput'],
      },
    ],
  },
  '/profile/advanced': {
    page_id: 'profile-advanced',
    title: '高级详情页',
    summary: '项目高级详情页，展示多维度信息，提供操作按钮和 Tab 切换。',
    business_goal: '详情展示与操作',
    regions: [
      {
        name: '操作区',
        purpose: '执行页面主操作',
        functions: ['pg_profile_advanced_action'],
      },
      {
        name: '信息切换区',
        purpose: '切换详情/规则 Tab',
        functions: ['pg_profile_advanced_tab'],
      },
    ],
  },
  '/dashboard/analysis': {
    page_id: 'dashboard-analysis',
    title: '分析仪表盘',
    summary: '数据可视化分析页，支持指标切换和时间维度筛选。',
    business_goal: '数据可视化分析',
    regions: [
      {
        name: '指标切换区',
        purpose: '切换销售额/访问量等指标',
        functions: ['pg_dashboard_analysis_tab'],
      },
    ],
  },
}

export function matchPageDescription(pathname: string): PageDescription | null {
  for (const [prefix, desc] of Object.entries(pageDescriptions)) {
    if (pathname.startsWith(prefix)) {
      return { ...desc, route: pathname }
    }
  }
  return null
}
