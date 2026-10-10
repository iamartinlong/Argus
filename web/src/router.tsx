import { createBrowserRouter, Navigate } from "react-router-dom"
import { AppLayout } from "@/components/app-layout"
import { LoginPage } from "@/features/auth/login-page"
import { ChangePasswordPage } from "@/features/auth/change-password-page"
import { SetupPage } from "@/features/setup/setup-page"
import { DashboardPage } from "@/features/dashboard/dashboard-page"
import { CertificatesPage } from "@/features/certificates/certificates-page"
import { CertificateDetailPage } from "@/features/certificates/certificate-detail-page"
import { AcmeAccountsPage } from "@/features/acme-accounts/acme-accounts-page"
import { DnsAccountsPage } from "@/features/dns-accounts/dns-accounts-page"
import { NotificationRulesPage } from "@/features/notifications/notification-rules-page"
import { ChannelsPage } from "@/features/notifications/channels-page"
import { NotificationHistoryPage } from "@/features/notifications/history-page"
import { NotificationLayout } from "@/features/notifications/notification-layout"
import { TaskLogsPage } from "@/features/logs/task-logs-page"
import { AuditLogsPage } from "@/features/logs/audit-logs-page"
import { DeployTargetsPage } from "@/features/deploy/deploy-targets-page"
import { ApiTokensPage } from "@/features/system/api-tokens-page"
import { UsersPage } from "@/features/system/users-page"
import { ContactsPage } from "@/features/system/contacts-page"
import { AgentsPage } from "@/features/agents/agents-page"
import { AgentDetailPage } from "@/features/agents/agent-detail-page"
import { ApiDocsPage } from "@/features/common/api-docs-page"

export const router = createBrowserRouter([
  {
    path: "/login",
    element: <LoginPage />,
  },
  {
    path: "/setup",
    element: <SetupPage />,
  },
  {
    path: "/change-password",
    element: <ChangePasswordPage />,
  },
  {
    path: "/",
    element: <AppLayout />,
    children: [
      { index: true, element: <Navigate to="/dashboard" replace /> },
      { path: "dashboard", element: <DashboardPage /> },
      { path: "certificates", element: <CertificatesPage /> },
      { path: "certificates/:id", element: <CertificateDetailPage /> },
      { path: "acme-accounts", element: <AcmeAccountsPage /> },
      { path: "dns-accounts", element: <DnsAccountsPage /> },
      { path: "deploy-targets", element: <DeployTargetsPage /> },
      {
        path: "notification",
        element: <NotificationLayout />,
        children: [
          { index: true, element: <Navigate to="channels" replace /> },
          { path: "channels", element: <ChannelsPage /> },
          { path: "rules", element: <NotificationRulesPage /> },
          { path: "histories", element: <NotificationHistoryPage /> },
        ],
      },
      // 兼容旧路径
      { path: "notification-channels", element: <Navigate to="/notification/channels" replace /> },
      { path: "notification-rules", element: <Navigate to="/notification/rules" replace /> },
      { path: "notification-history", element: <Navigate to="/notification/histories" replace /> },
      { path: "task-logs", element: <TaskLogsPage /> },
      { path: "users", element: <UsersPage /> },
      { path: "contacts", element: <ContactsPage /> },
      { path: "api-tokens", element: <ApiTokensPage /> },
      { path: "agents", element: <AgentsPage /> },
      { path: "agents/:id", element: <AgentDetailPage /> },
      { path: "audit-logs", element: <AuditLogsPage /> },
      { path: "api-docs", element: <ApiDocsPage /> },
    ],
  },
  {
    path: "*",
    element: <Navigate to="/dashboard" replace />,
  },
])
