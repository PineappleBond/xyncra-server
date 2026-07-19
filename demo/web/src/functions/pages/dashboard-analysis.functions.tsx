import { useRegisterFunctions } from '@xyncra/client-web';
import type { FunctionEntry } from '@xyncra/client-web';
import {
  createClickFunction,
  createTabFunction,
  createSegmentFunction,
  createDateRangeFunction,
} from '../utils/factory';

const dashboardAnalysisFunctions: FunctionEntry[] = [
  createTabFunction(
    'pg_dash_analysis_tab_sales',
    '在分析仪表盘切换到「销售额」Tab',
    '销售额',
    '.salesCard',
    ['page:dashboard-analysis'],
  ),
  createTabFunction(
    'pg_dash_analysis_tab_visits',
    '在分析仪表盘切换到「访问量」Tab',
    '访问量',
    '.salesCard',
    ['page:dashboard-analysis'],
  ),
  createClickFunction(
    'pg_dash_analysis_date_today',
    '在分析仪表盘查询「今日」数据',
    'button:contains("今日")',
    ['page:dashboard-analysis'],
  ),
  createClickFunction(
    'pg_dash_analysis_date_week',
    '在分析仪表盘查询「本周」数据',
    'button:contains("本周")',
    ['page:dashboard-analysis'],
  ),
  createClickFunction(
    'pg_dash_analysis_date_month',
    '在分析仪表盘查询「本月」数据',
    'button:contains("本月")',
    ['page:dashboard-analysis'],
  ),
  createClickFunction(
    'pg_dash_analysis_date_year',
    '在分析仪表盘查询「本年」数据',
    'button:contains("本年")',
    ['page:dashboard-analysis'],
  ),
  createDateRangeFunction(
    'pg_dash_analysis_date_range',
    '在分析仪表盘选择自定义日期范围',
    '.ant-picker',
    ['page:dashboard-analysis'],
  ),
  createSegmentFunction(
    'pg_dash_analysis_segment_all',
    '在分析仪表盘筛选「全部渠道」',
    '全部渠道',
    '.salesCard',
    ['page:dashboard-analysis'],
  ),
  createSegmentFunction(
    'pg_dash_analysis_segment_online',
    '在分析仪表盘筛选「线上」渠道',
    '线上',
    '.salesCard',
    ['page:dashboard-analysis'],
  ),
  createSegmentFunction(
    'pg_dash_analysis_segment_store',
    '在分析仪表盘筛选「门店」渠道',
    '门店',
    '.salesCard',
    ['page:dashboard-analysis'],
  ),
];

export function DashboardAnalysisFunctions() {
  useRegisterFunctions(dashboardAnalysisFunctions);
  return null;
}
