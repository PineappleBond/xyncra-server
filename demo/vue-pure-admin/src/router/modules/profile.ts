import { system } from "@/router/enums";

export default {
  path: "/profile",
  meta: {
    icon: "ri:user-settings-line",
    title: "详情页",
    rank: system + 11
  },
  children: [
    {
      path: "/profile/advanced",
      name: "ProfileAdvanced",
      component: () => import("@/views/profile/advanced/index.vue"),
      meta: {
        title: "高级详情"
      }
    }
  ]
} satisfies RouteConfigsTable;
