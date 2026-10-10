# 04 前端设计

## 4.1 技术选型

| 类别 | 选型 |
|---|---|
| 框架/构建 | React 18 + TypeScript + Vite |
| UI | shadcn/ui + Tailwind CSS（组件源码入项目，深色模式） |
| 数据请求 | TanStack Query + Axios（缓存/轮询/失效重取） |
| 表格 | TanStack Table（shadcn data-table：排序/筛选/分页） |
| 图表 | Recharts（shadcn charts） |
| 表单 | React Hook Form + Zod（校验规则与后端对齐） |
| 路由/状态 | React Router + Zustand |
| 实时更新 | 任务日志 SSE 或 5s 轮询 |

## 4.2 构建集成

- 开发期：Vite dev server 代理 `/api` → Go 服务，前后端独立开发
- 生产：`npm run build` → `web/dist` → Go `//go:embed web/dist/*` 挂载 + SPA 路由回退（非 `/api` 请求返回 index.html）
- 无 UI 构建（CLI-only 镜像）用 build tag 跳过 embed

## 4.3 页面结构

```
/ 登录 → /dashboard 仪表盘
├── 仪表盘：证书总数、30 天内到期、续期成功率、到期分布图（Recharts）、签发趋势、失败告警
├── 证书：列表（状态徽标/到期倒计时/筛选搜索）→ 详情（SAN、CA、有效期、订单时间线、
│         手动续期/吊销/重推/下载）
├── ACME 账户：列表/新建（目录 URL、EAB）
├── 部署目标：CRUD（local/ssh/k8s/webhook 表单）、连接测试
├── 通知规则：CRUD（事件/渠道/提前天数）、测试发送
├── 任务日志：列表 + 实时进度
├── 审计日志：查询
└── 系统：用户管理、API Token
```

## 4.4 交互细节

- 到期高亮（30 天内橙色、已过期红色）
- 危险操作（吊销/删除/私钥下载）二次确认
- 表单校验 Zod schema 与后端一致
- 任务进度轮询刷新，失败行可展开错误详情
