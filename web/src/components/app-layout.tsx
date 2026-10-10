import * as React from "react"
import { useEffect, useMemo, useState } from "react"
import { Link, Outlet, Navigate, useLocation } from "react-router-dom"
import { AppSidebar } from "@/components/app-sidebar"
import { ThemeToggle } from "@/components/theme-toggle"
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb"
import { Separator } from "@/components/ui/separator"
import {
  SidebarInset,
  SidebarProvider,
  SidebarTrigger,
} from "@/components/ui/sidebar"
import { Toaster } from "@/components/ui/sonner"
import { useAuthStore } from "@/stores/auth-store"
import { restoreSession } from "@/lib/api"
import { fetchSetupStatus } from "@/features/setup/setup-api"
import { nav } from "@/lib/nav"

function useBreadcrumbs(pathname: string): string[] {
  // 匹配 nav 中最长前缀，返回 [group.label, item.title, ...extra]
  let matched: string[] | null = null
  let bestLen = -1
  for (const group of nav) {
    for (const item of group.items) {
      if (pathname === item.url || pathname.startsWith(item.url + "/")) {
        if (item.url.length > bestLen) {
          bestLen = item.url.length
          matched = [group.label, item.title]
        }
      }
    }
  }
  if (!matched) {
    if (pathname.startsWith("/certificates/")) return ["证书", "证书管理", "详情"]
    if (pathname.startsWith("/notification/")) {
      const tab = pathname.split("/")[2]
      const tabName = tab === "rules" ? "规则" : tab === "histories" ? "历史" : "渠道"
      return ["自动化", "通知", tabName]
    }
    return ["概览", "仪表盘"]
  }
  // 通知子路由补充第三级
  if (matched[1] === "通知" && pathname.startsWith("/notification/")) {
    const tab = pathname.split("/")[2]
    const tabName = tab === "rules" ? "规则" : tab === "histories" ? "历史" : "渠道"
    return [...matched, tabName]
  }
  if (matched[1] === "证书管理" && pathname.startsWith("/certificates/") && pathname !== "/certificates") {
    return [...matched, "详情"]
  }
  return matched
}

export function AppLayout() {
  const location = useLocation()
  const crumbs = useMemo(() => useBreadcrumbs(location.pathname), [location.pathname])
  const accessToken = useAuthStore((s) => s.accessToken)
  const mustChangePassword = useAuthStore((s) => s.mustChangePassword)
  // 初始为 true：先尝试恢复会话 + 检查初始化状态再决定跳转
  const [restoring, setRestoring] = useState(true)
  const [initialized, setInitialized] = useState<boolean | null>(null)

  // 页面刷新后内存 token 丢失：通过刷新 cookie 静默恢复会话；
  // 同时查询安装状态：未初始化（首次使用）强制进 /setup
  useEffect(() => {
    let cancelled = false
    Promise.all([
      restoreSession(),
      fetchSetupStatus()
        .then((s) => !cancelled && setInitialized(s.initialized))
        .catch(() => !cancelled && setInitialized(true)),
    ]).finally(() => !cancelled && setRestoring(false))
    return () => {
      cancelled = true
    }
  }, [])

  if (restoring || initialized === null) {
    return (
      <div className="flex min-h-svh items-center justify-center text-muted-foreground">
        正在恢复会话...
      </div>
    )
  }

  if (!initialized) {
    return <Navigate to="/setup" replace />
  }

  if (!accessToken) {
    return <Navigate to="/login" replace />
  }

  if (mustChangePassword) {
    return <Navigate to="/change-password" replace />
  }

  return (
    <SidebarProvider>
      <AppSidebar />
      <SidebarInset>
        <header className="flex h-16 shrink-0 items-center gap-2 border-b px-4">
          <SidebarTrigger className="-ml-1" />
          <Separator
            orientation="vertical"
            className="mr-2 data-[orientation=vertical]:h-4 data-[orientation=vertical]:self-center"
          />
          <Breadcrumb>
            <BreadcrumbList>
              <BreadcrumbItem>
                <BreadcrumbLink render={<Link to="/dashboard" />}>Argus</BreadcrumbLink>
              </BreadcrumbItem>
              {crumbs.map((c, i) => (
                <React.Fragment key={`${c}-${i}`}>
                  <BreadcrumbSeparator />
                  <BreadcrumbItem>
                    {i === crumbs.length - 1 ? (
                      <BreadcrumbPage>{c}</BreadcrumbPage>
                    ) : (
                      <span className="text-muted-foreground">{c}</span>
                    )}
                  </BreadcrumbItem>
                </React.Fragment>
              ))}
            </BreadcrumbList>
          </Breadcrumb>
          <div className="ml-auto flex items-center gap-2">
            <ThemeToggle />
          </div>
        </header>
        <div className="flex flex-1 flex-col gap-4 p-4">
          <Outlet />
        </div>
      </SidebarInset>
      <Toaster />
    </SidebarProvider>
  )
}
