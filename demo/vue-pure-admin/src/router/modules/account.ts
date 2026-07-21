import { system } from "@/router/enums";

export default {
  path: "/account",
  meta: {
    icon: "ri:user-3-line",
    title: "个人中心",
    rank: system + 10
  },
  children: [
    {
      path: "/account/center",
      name: "AccountCenter",
      component: () => import("@/views/account/center/index.vue"),
      meta: {
        title: "个人中心"
      }
    },
    {
      path: "/account/settings",
      name: "AccountSettingsNew",
      component: () => import("@/views/account-settings/index.vue"),
      meta: {
        title: "账户设置",
        showLink: false
      }
    }
  ]
} satisfies RouteConfigsTable;
