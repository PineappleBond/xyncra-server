import { $t } from "@/plugins/i18n";
import { form } from "@/router/enums";

export default {
  path: "/form",
  redirect: "/form/index",
  meta: {
    icon: "ri/edit-box-line",
    title: $t("menus.pureSchemaForm"),
    rank: form
  },
  children: [
    {
      path: "/form/index",
      name: "SchemaForm",
      component: () => import("@/views/schema-form/index.vue"),
      meta: {
        title: $t("menus.pureSchemaForm")
      }
    },
    {
      path: "/form/advanced",
      name: "AdvancedForm",
      component: () => import("@/views/form/advanced/index.vue"),
      meta: {
        title: "高级表单"
      }
    },
    {
      path: "/form/step",
      name: "StepForm",
      component: () => import("@/views/form/step/index.vue"),
      meta: {
        title: "分步表单"
      }
    },
    {
      path: "/form/test",
      name: "FormTest",
      component: () => import("@/views/form/test/index.vue"),
      meta: {
        title: "Xyncra 测试表单"
      }
    }
  ]
} satisfies RouteConfigsTable;
