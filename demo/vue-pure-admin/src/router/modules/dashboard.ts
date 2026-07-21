import { system } from "@/router/enums";

export default {
  path: "/dashboard",
  meta: {
    icon: "ri:dashboard-3-line",
    title: "仪表盘",
    rank: system + 12
  },
  children: [
    {
      path: "/dashboard/analysis",
      name: "DashboardAnalysis",
      component: () => import("@/views/dashboard/analysis/index.vue"),
      meta: {
        title: "分析页"
      }
    }
  ]
} satisfies RouteConfigsTable;
