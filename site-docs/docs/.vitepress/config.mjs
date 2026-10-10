import { defineConfig } from "vitepress"

export default defineConfig({
  title: "Argus",
  description: "永不眨眼的 SSL 证书守护者 - 集中式 SSL 证书管理系统",
  lang: "zh-CN",
  lastUpdated: false,
  // GitHub Pages 项目站点（username.github.io/Argus/）需要 base；
  // 本地开发（npm run dev）不受影响
  base: "/Argus/",
  themeConfig: {
    logo: "/logo.svg",
    nav: [
      { text: "首页", link: "/" },
      { text: "快速开始", link: "/guide/quickstart" },
      { text: "用户手册", link: "/manual/certificates" },
      { text: "部署指南", link: "/deploy/" },
      { text: "API 参考", link: "/api/overview" },
      { text: "架构", link: "/architecture/overview" },
    ],
    sidebar: {
      "/guide/": [
        {
          text: "快速开始",
          items: [
            { text: "安装与启动", link: "/guide/quickstart" },
            { text: "签发第一张证书", link: "/guide/first-certificate" },
          ],
        },
      ],
      "/manual/": [
        {
          text: "用户手册",
          items: [
            { text: "证书管理", link: "/manual/certificates" },
            { text: "SSL 渠道账户", link: "/manual/accounts" },
            { text: "DNS 验证账户", link: "/manual/accounts#dns-验证账户" },
            { text: "部署目标", link: "/manual/deploy-targets" },
            { text: "Agent 管理", link: "/manual/agents" },
            { text: "通知规则", link: "/manual/notifications" },
            { text: "任务日志", link: "/manual/task-logs" },
            { text: "API Token 与用户", link: "/manual/system" },
          ],
        },
      ],
      "/deploy/": [
        {
          text: "部署指南",
          items: [
            { text: "总览", link: "/deploy/" },
            { text: "升级指南", link: "/deploy/upgrade" },
            {
              text: "服务端部署",
              collapsed: false,
              items: [
                { text: "服务端总览", link: "/deploy/server/" },
                { text: "Linux / systemd", link: "/deploy/server/linux" },
                { text: "Windows 服务", link: "/deploy/server/windows" },
                { text: "Docker", link: "/deploy/server/docker" },
              ],
            },
            {
              text: "部署目标",
              collapsed: false,
              items: [
                { text: "部署目标总览", link: "/deploy/deploy-targets/" },
                { text: "SSH 服务器", link: "/deploy/deploy-targets/ssh" },
                { text: "Agent", link: "/deploy/deploy-targets/agent" },
                { text: "本机", link: "/deploy/deploy-targets/local" },
                { text: "Windows IIS", link: "/deploy/deploy-targets/iis" },
                { text: "Kubernetes", link: "/deploy/deploy-targets/k8s" },
                { text: "Webhook", link: "/deploy/deploy-targets/webhook" },
              ],
            },
          ],
        },
      ],
      "/ops/": [
        {
          text: "运维指南",
          items: [
            { text: "数据备份与恢复", link: "/ops/backup" },
            { text: "安全建议", link: "/ops/security" },
            { text: "故障排查", link: "/ops/troubleshooting" },
          ],
        },
      ],
      "/api/": [
        {
          text: "API 参考",
          items: [
            { text: "概览与认证", link: "/api/overview" },
            { text: "证书", link: "/api/certificates" },
            { text: "渠道账户", link: "/api/accounts" },
            { text: "部署目标", link: "/api/deploy-targets" },
          ],
        },
      ],
      "/architecture/": [
        {
          text: "架构",
          items: [
            { text: "总体架构", link: "/architecture/overview" },
            { text: "目录结构", link: "/architecture/structure" },
            { text: "里程碑", link: "/architecture/milestones" },
          ],
        },
      ],
    },
    socialLinks: [{ icon: "github", link: "https://github.com/iamartinlong/Argus" }],
    footer: {
      message: "Argus - 永不眨眼的 SSL 证书守护者",
      copyright: "MIT License",
    },
  },
})
