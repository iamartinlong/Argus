import { useQuery } from "@tanstack/react-query"
import { useNavigate } from "react-router-dom"
import {
  ResponsiveContainer,
  BarChart,
  Bar,
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  Cell,
} from "recharts"
import { ShieldCheckIcon, AlertTriangleIcon, RefreshCwIcon, GlobeIcon, BellRingIcon, RocketIcon } from "lucide-react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { api } from "@/lib/api"
import type { Stats } from "@/lib/types"

const COLORS = ["#22c55e", "#f59e0b", "#ef4444", "#a1a1aa"]

export function DashboardPage() {
  const navigate = useNavigate()
  const { data, isLoading } = useQuery({
    queryKey: ["stats"],
    queryFn: async () => {
      const res = await api.get<Stats>("/stats")
      return res.data
    },
  })

  const cards = [
    {
      title: "证书总数",
      value: data?.total_certificates ?? 0,
      icon: ShieldCheckIcon,
      to: "/certificates",
    },
    {
      title: "30 天内到期",
      value: data?.expiring_within_30d ?? 0,
      icon: AlertTriangleIcon,
      to: "/certificates",
    },
    {
      title: "已过期",
      value: data?.expired ?? 0,
      icon: AlertTriangleIcon,
      to: "/certificates",
    },
    {
      title: "自动续期成功率",
      value: data ? `${(data.auto_renew_success_rate * 100).toFixed(1)}%` : "-",
      icon: RefreshCwIcon,
      to: "/task-logs",
    },
  ]

  const chartData = data
    ? [
        {
          name: "有效",
          value: Math.max(
            0,
            data.total_certificates - data.expiring_within_30d - data.expired,
          ),
        },
        { name: "30 天内到期", value: data.expiring_within_30d },
        { name: "已过期", value: data.expired },
      ]
    : []

  const trendData = data?.renewal_trend ?? []
  const rateCards = [
    {
      title: "部署成功率",
      value: data && (data.deploy_success_rate > 0 || data.deploy_success_rate === 0 && data.total_certificates > 0)
        ? `${(data.deploy_success_rate * 100).toFixed(1)}%`
        : "-",
      icon: RocketIcon,
      to: "/deploy-targets",
    },
    {
      title: "通知成功率",
      value: data ? `${(data.notify_success_rate * 100).toFixed(1)}%` : "-",
      icon: BellRingIcon,
      to: "/notification-history",
    },
  ]

  return (
    <div className="flex flex-col gap-4">
      <div className="grid auto-rows-min gap-4 md:grid-cols-4">
        {cards.map((card) => (
          <Card
            key={card.title}
            className="cursor-pointer transition-colors hover:bg-muted/50"
            onClick={() => navigate(card.to)}
          >
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">{card.title}</CardTitle>
              <card.icon className="size-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">
                {isLoading ? "..." : card.value}
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
      <div className="grid auto-rows-min gap-4 md:grid-cols-2">
        {rateCards.map((card) => (
          <Card
            key={card.title}
            className="cursor-pointer transition-colors hover:bg-muted/50"
            onClick={() => navigate(card.to)}
          >
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">{card.title}</CardTitle>
              <card.icon className="size-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{isLoading ? "..." : card.value}</div>
            </CardContent>
          </Card>
        ))}
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">证书状态分布</CardTitle>
          </CardHeader>
          <CardContent className="h-64">
            {chartData.length > 0 ? (
              <ResponsiveContainer width="100%" height="100%">
                <BarChart data={chartData}>
                  <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
                  <XAxis dataKey="name" tick={{ fontSize: 12 }} />
                  <YAxis allowDecimals={false} tick={{ fontSize: 12 }} />
                  <Tooltip />
                  <Bar dataKey="value" name="证书数" radius={[4, 4, 0, 0]}>
                    {chartData.map((_, i) => (
                      <Cell key={i} fill={COLORS[i]} />
                    ))}
                  </Bar>
                </BarChart>
              </ResponsiveContainer>
            ) : (
              <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
                {isLoading ? "加载中..." : "暂无数据"}
              </div>
            )}
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className="text-base">续期趋势（近 12 个月）</CardTitle>
          </CardHeader>
          <CardContent className="h-64">
            {trendData.length > 0 && trendData.some((p) => p.total > 0) ? (
              <ResponsiveContainer width="100%" height="100%">
                <LineChart data={trendData}>
                  <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
                  <XAxis dataKey="month" tick={{ fontSize: 11 }} />
                  <YAxis allowDecimals={false} tick={{ fontSize: 12 }} />
                  <Tooltip />
                  <Legend />
                  <Line type="monotone" dataKey="total" name="续期总数" stroke="#6366f1" strokeWidth={2} dot={false} />
                  <Line type="monotone" dataKey="success" name="成功" stroke="#22c55e" strokeWidth={2} dot={false} />
                </LineChart>
              </ResponsiveContainer>
            ) : (
              <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
                {isLoading ? "加载中..." : "暂无续期记录"}
              </div>
            )}
          </CardContent>
        </Card>
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        <Card className="flex-1">
          <CardHeader className="flex flex-row items-center justify-between space-y-0">
            <CardTitle className="flex items-center gap-2 text-base">
              <GlobeIcon className="size-4 text-muted-foreground" />
              快速开始
            </CardTitle>
          </CardHeader>
          <CardContent className="text-sm text-muted-foreground space-y-2">
            <p>1. 在「SSL 渠道账户」中创建 ACME 账户并测试连接</p>
            <p>2. 在「证书管理」中签发证书（HTTP-01 或 DNS-01，支持 CSR 模式）</p>
            <p>3. 证书到期前系统将自动续期并通过通知规则推送</p>
            <p>4. 在「任务日志」中可查看调度运行记录（支持手动触发扫描）</p>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
