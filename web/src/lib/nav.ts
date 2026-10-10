import {
  LayoutDashboardIcon,
  ShieldCheckIcon,
  ServerIcon,
  MonitorSmartphoneIcon,
  BellIcon,
  ListChecksIcon,
  UsersIcon,
  KeyRoundIcon,
  BookOpenIcon,
  ScrollTextIcon,
  ContactRoundIcon,
  NetworkIcon,
  LockKeyholeIcon,
} from "lucide-react"

export const nav = [
  {
    label: "概览",
    items: [
      {
        title: "仪表盘",
        url: "/dashboard",
        icon: LayoutDashboardIcon,
      },
    ],
  },
  {
    label: "证书",
    items: [
      {
        title: "证书管理",
        url: "/certificates",
        icon: ShieldCheckIcon,
      },
      {
        title: "SSL 渠道账户",
        url: "/acme-accounts",
        icon: LockKeyholeIcon,
      },
      {
        title: "DNS 验证账户",
        url: "/dns-accounts",
        icon: NetworkIcon,
      },
    ],
  },
  {
    label: "自动化",
    items: [
      {
        title: "部署目标",
        url: "/deploy-targets",
        icon: ServerIcon,
      },
      {
        title: "Agent",
        url: "/agents",
        icon: MonitorSmartphoneIcon,
      },
      {
        title: "通知",
        url: "/notification",
        icon: BellIcon,
      },
      {
        title: "任务日志",
        url: "/task-logs",
        icon: ListChecksIcon,
      },
    ],
  },
  {
    label: "系统",
    items: [
      {
        title: "API 文档",
        url: "/api-docs",
        icon: BookOpenIcon,
      },
      {
        title: "用户管理",
        url: "/users",
        icon: UsersIcon,
      },
      {
        title: "申请人信息",
        url: "/contacts",
        icon: ContactRoundIcon,
      },
      {
        title: "API Token",
        url: "/api-tokens",
        icon: KeyRoundIcon,
      },
      {
        title: "审计日志",
        url: "/audit-logs",
        icon: ScrollTextIcon,
      },
    ],
  },
] as const
