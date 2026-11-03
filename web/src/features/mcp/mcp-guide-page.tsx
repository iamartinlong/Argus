import { useState } from "react"
import { Link } from "react-router-dom"
import { Check, Copy, KeyRound } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Badge } from "@/components/ui/badge"

// 工具清单（与 internal/mcp/tools.go 保持一致）
const TOOLS: { name: string; scope: string; desc: string; risk?: boolean }[] = [
  { name: "list_certificates", scope: "certificates:read", desc: "证书列表（状态/临期/关键字过滤）" },
  { name: "get_certificate", scope: "certificates:read", desc: "证书详情（SAN/有效期/渠道）" },
  { name: "list_deploy_targets", scope: "deploy:read", desc: "部署目标列表" },
  { name: "list_accounts", scope: "accounts:read", desc: "渠道账户列表（脱敏）" },
  { name: "list_agents", scope: "admin:read", desc: "Agent 在线状态" },
  { name: "get_task_logs", scope: "certificates:read", desc: "最近任务日志" },
  { name: "issue_certificate", scope: "certificates:write", desc: "签发证书（受 CA 速率限制）" },
  { name: "renew_certificate", scope: "certificates:write", desc: "续期证书" },
  { name: "revoke_certificate", scope: "certificates:write", desc: "吊销证书", risk: true },
  { name: "import_certificate", scope: "certificates:write", desc: "导入既有证书（PEM）" },
  { name: "deploy_certificate", scope: "deploy:write", desc: "推送部署到目标" },
  { name: "test_deploy_target", scope: "deploy:write", desc: "测试目标连通性" },
  { name: "download_certificate", scope: "certificates:write", desc: "下载证书打包（含私钥）", risk: true },
]

/** 一键复制按钮 */
function CopyButton({ text, label }: { text: string; label?: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-sm"
      title="复制"
      aria-label="复制"
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(text)
          setCopied(true)
          setTimeout(() => setCopied(false), 1500)
        } catch {
          // 剪贴板不可用（非 HTTPS 等）：忽略
        }
      }}
    >
      {copied ? <Check className="size-3.5 text-green-600" /> : <Copy className="size-3.5" />}
      {label && <span>{label}</span>}
    </Button>
  )
}

function CommandBlock({ command }: { command: string }) {
  return (
    <div className="flex items-center gap-2 rounded-md border bg-muted/50 px-3 py-2">
      <code className="flex-1 overflow-x-auto font-mono text-xs whitespace-pre">{command}</code>
      <CopyButton text={command} />
    </div>
  )
}

/**
 * MCP 指南页（静态）：让用户知道 Argus 可作为 MCP Server 被 AI Agent
 * 连接，并给出连接命令与工具清单。零服务端交互——M1 的 MCP 没有
 * 服务端运行时状态可展示（stdio 为客户端拉起的短生命周期进程）。
 * 完整教程见文档站（唯一事实源）。
 */
export function McpGuidePage() {
  return (
    <div className="flex flex-col gap-4">
      <Card className="flex-1">
        <CardHeader>
          <CardTitle>MCP（AI Agent 接入）</CardTitle>
          <CardDescription>
            Argus 是一个 MCP（Model Context Protocol）Server。Claude Code、Cline、Zed、
            Claude Desktop 等 AI Agent 连接后，可用自然语言完成证书查询、签发、续期、吊销、部署。
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-6">
          <section className="space-y-2">
            <h3 className="text-sm font-medium">本地模式（Agent 与 Argus 同机）</h3>
            <p className="text-sm text-muted-foreground">
              直连本机数据库，无需端口与 token（进程权限即信任，与 CLI 同等模型）。
            </p>
            <CommandBlock command="argus mcp --config /etc/argus/config.yaml" />
            <p className="text-xs text-muted-foreground">
              Claude Code 快速添加：<code className="font-mono">claude mcp add argus -- argus mcp --config &lt;配置路径&gt;</code>
            </p>
          </section>

          <section className="space-y-2">
            <h3 className="text-sm font-medium">远程模式（Agent 在运维电脑）</h3>
            <p className="text-sm text-muted-foreground">
              经服务端 REST API 代理，每个调用由服务端强制校验 token 的 scope 与角色。
            </p>
            <CommandBlock command="argus mcp --server https://cert.example.com --token st_xxxxxxxx" />
            <p className="text-xs text-muted-foreground">
              建议用环境变量 <code className="font-mono">ARGUS_API_TOKEN</code> 传 token，避免出现在 shell 历史。
            </p>
          </section>

          <section className="space-y-2">
            <h3 className="text-sm font-medium">工具清单（13 个）</h3>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>工具</TableHead>
                  <TableHead>所需 scope</TableHead>
                  <TableHead>说明</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {TOOLS.map((t) => (
                  <TableRow key={t.name}>
                    <TableCell className="font-mono text-xs">{t.name}</TableCell>
                    <TableCell>
                      <Badge variant="outline">{t.scope}</Badge>
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {t.desc}
                      {t.risk && (
                        <span className="ml-1 text-amber-600 dark:text-amber-400">
                          {t.name === "revoke_certificate" ? "（不可逆）" : "（默认关闭）"}
                        </span>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            <p className="text-xs text-muted-foreground">
              <code className="font-mono">download_certificate</code> 含私钥，默认不注册：需在 config.yaml 配置
              <code className="font-mono"> mcp.allow_key_download: true</code>，且每次调用均审计。
            </p>
          </section>

          <section className="flex flex-wrap items-center gap-3">
            <Button variant="outline" render={<Link to="/api-tokens" />}>
              <KeyRound className="mr-2 size-4" />
              去创建 API Token（远程模式需要）
            </Button>
            <Button variant="link" render={<a href="https://iamartinlong.github.io/Argus/guide/mcp" target="_blank" rel="noreferrer" />}>
              查看完整文档（客户端配置 / 排障）
            </Button>
          </section>
        </CardContent>
      </Card>
    </div>
  )
}
