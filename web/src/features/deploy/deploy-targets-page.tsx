import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useState } from "react"
import { useForm, type UseFormReturn } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { Plus, Trash2, PlayCircle, Server, Eye } from "lucide-react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { api, getErrorMessage } from "@/lib/api"

interface DeployTarget {
  id: string
  name: string
  type: string
  enabled: boolean
  last_status: string
  last_error: string
  created_at: string
  config?: Record<string, unknown>
}

const typeNames: Record<string, string> = {
  local: "本机",
  ssh: "SSH",
  agent: "Agent",
  k8s: "Kubernetes",
  webhook: "Webhook",
}

// scene 应用场景（目标环境）：仅 本机/SSH/Agent 通道可选，K8s/Webhook 无场景
const sceneNames: Record<string, string> = {
  generic: "通用",
  nginx: "Nginx",
  apache: "Apache",
  tomcat: "Tomcat",
  iis: "Windows IIS",
}

// 有场景的通道
const sceneChannels = ["local", "ssh", "agent"]

const targetSchema = z
  .object({
    name: z.string().min(1, "请输入目标名称"),
    type: z.enum(["local", "ssh", "agent", "k8s", "webhook"]),
    scene: z.enum(["generic", "nginx", "apache", "tomcat", "iis"]).optional(),
    cert_path: z.string().optional(),
    key_path: z.string().optional(),
    post_command: z.string().optional(),
    host: z.string().optional(),
    port: z.string().optional(),
    user: z.string().optional(),
    private_key: z.string().optional(),
    password: z.string().optional(),
    hook: z.string().optional(),
    known_hosts: z.string().optional(),
    iis_bindings: z.string().optional(),
    agent_id: z.string().optional(),
    pfx_path: z.string().optional(),
    site_name: z.string().optional(),
    hostname: z.string().optional(),
    server_names: z.string().optional(),
    namespace: z.string().optional(),
    secret_name: z.string().optional(),
    url: z.string().optional(),
  })
  .superRefine((v, ctx) => {
    // 有场景通道缺省用通用
    const scene = sceneChannels.includes(v.type) ? (v.scene || "generic") : undefined
    const add = (field: string, msg: string) =>
      ctx.addIssue({ code: "custom", path: [field], message: msg })

    switch (v.type) {
      case "local":
        if (scene === "tomcat" || scene === "iis") {
          if (!v.pfx_path) add("pfx_path", "请输入 PFX 路径")
          if (scene === "iis" && !v.iis_bindings) add("iis_bindings", "请输入 HTTPS 绑定端口")
        } else if (!v.cert_path) {
          add("cert_path", "请输入证书路径")
        }
        break
      case "ssh":
        if (!v.host) add("host", "请输入主机")
        if (!v.user) add("user", "请输入用户")
        if (!v.private_key && !v.password) add("private_key", "请配置 SSH 私钥或密码")
        if (scene === "tomcat" || scene === "iis") {
          if (!v.pfx_path) add("pfx_path", "请输入 PFX 路径")
          if (scene === "iis" && !v.iis_bindings) add("iis_bindings", "请输入 HTTPS 绑定端口")
        } else if (!v.cert_path) {
          add("cert_path", "请输入证书路径")
        }
        if (v.port && !/^\d+$/.test(v.port)) add("port", "端口需为数字")
        break
      case "agent":
        if (!v.agent_id) add("agent_id", "请选择 Agent")
        if (scene === "tomcat" || scene === "iis") {
          if (!v.pfx_path) add("pfx_path", "请输入 PFX 路径")
          if (scene === "iis" && !v.iis_bindings) add("iis_bindings", "请输入 HTTPS 绑定端口")
        } else if (!v.cert_path) {
          add("cert_path", "请输入证书路径")
        }
        break
      case "k8s":
        if (!v.secret_name) add("secret_name", "请输入 Secret 名称")
        break
      case "webhook":
        if (!v.url) add("url", "请输入 Webhook URL")
        else if (!/^https:\/\//.test(v.url)) add("url", "URL 必须为 https://")
        break
    }
  })

type TargetForm = z.infer<typeof targetSchema>

// buildConfig 根据表单值构建目标 config（创建/编辑共用）
function buildConfig(values: TargetForm): Record<string, unknown> {
  const scene = sceneChannels.includes(values.type) ? (values.scene || "generic") : undefined
  const port = (n: string | undefined, d: number) => (n ? Number(n) : d)
  const bindings = (v: string | undefined) =>
    v ? v.split(/[\s,]+/).map(Number).filter((n) => n > 0) : undefined

  switch (values.type) {
    case "local":
      if (scene === "tomcat" || scene === "iis") {
        return { scene, pfx_path: values.pfx_path, bindings: bindings(values.iis_bindings), site_name: values.site_name, hostname: values.hostname || undefined, post_command: values.post_command }
      }
      if (scene === "nginx") {
        return { scene, cert_path: values.cert_path, key_path: values.key_path, server_names: values.server_names || undefined, post_command: values.post_command }
      }
      return { scene, cert_path: values.cert_path, key_path: values.key_path, post_command: values.post_command }
    case "ssh":
      if (scene === "tomcat" || scene === "iis") {
        return {
          scene, host: values.host, port: port(values.port, 22), user: values.user,
          private_key: values.private_key, password: values.password, known_hosts: values.known_hosts,
          pfx_path: values.pfx_path, bindings: bindings(values.iis_bindings), site_name: values.site_name, hostname: values.hostname || undefined,
          hook: values.hook,
        }
      }
      if (scene === "nginx") {
        return {
          scene, host: values.host, port: port(values.port, 22), user: values.user,
          private_key: values.private_key, password: values.password, known_hosts: values.known_hosts,
          cert_path: values.cert_path, key_path: values.key_path, server_names: values.server_names || undefined, hook: values.hook,
        }
      }
      return {
        scene, host: values.host, port: port(values.port, 22), user: values.user,
        private_key: values.private_key, password: values.password,
        cert_path: values.cert_path, key_path: values.key_path, hook: values.hook,
        known_hosts: values.known_hosts,
      }
    case "agent":
      if (scene === "tomcat" || scene === "iis") {
        return {
          scene, agent_id: values.agent_id, pfx_path: values.pfx_path,
          site_name: values.site_name, hostname: values.hostname || undefined, bindings: bindings(values.iis_bindings),
          hook: values.hook, is_windows: scene === "iis",
        }
      }
      if (scene === "nginx") {
        return {
          scene, agent_id: values.agent_id, cert_path: values.cert_path, key_path: values.key_path,
          server_names: values.server_names || undefined, hook: values.hook,
        }
      }
      return {
        scene, agent_id: values.agent_id, cert_path: values.cert_path,
        key_path: values.key_path, hook: values.hook,
      }
    case "k8s":
      return { namespace: values.namespace, secret_name: values.secret_name }
    case "webhook":
      return { url: values.url }
  }
}

// configToForm 将服务端 config 映射回表单值（编辑回显）；旧目标无 scene 时按 config 推断
function configToForm(type: string, cfg: Record<string, unknown>): Partial<TargetForm> {
  const c = cfg ?? {}
  const out: Record<string, unknown> = { type }
  const hasScene = sceneChannels.includes(type)

  // 推断场景：config 含 scene 用之；否则有 pfx_path/is_windows → iis，其余 generic
  let scene = c.scene as string | undefined
  if (hasScene && !scene) {
    if (c.pfx_path || c.is_windows) scene = "iis"
    else scene = "generic"
  }
  if (scene) out.scene = scene
  const isPFX = scene === "tomcat" || scene === "iis"

    switch (type) {
    case "local":
      if (isPFX) {
        out.pfx_path = c.pfx_path
        out.site_name = c.site_name
        out.hostname = c.hostname
        out.iis_bindings = Array.isArray(c.bindings) ? (c.bindings as number[]).join(",") : ""
        out.post_command = c.post_command
      } else {
        out.cert_path = c.cert_path
        out.key_path = c.key_path
        out.server_names = c.server_names
        out.post_command = c.post_command
      }
      break
    case "ssh":
      out.host = c.host
      out.port = c.port != null ? String(c.port) : "22"
      out.user = c.user
      out.private_key = c.private_key
      out.password = c.password
      out.known_hosts = c.known_hosts
      out.hook = c.hook
      if (isPFX) {
        out.pfx_path = c.pfx_path
        out.site_name = c.site_name
        out.hostname = c.hostname
        out.iis_bindings = Array.isArray(c.bindings) ? (c.bindings as number[]).join(",") : ""
      } else {
        out.cert_path = c.cert_path
        out.key_path = c.key_path
        out.server_names = c.server_names
      }
      break
    case "agent":
      out.agent_id = c.agent_id
      out.hook = c.hook
      if (isPFX) {
        out.pfx_path = c.pfx_path
        out.site_name = c.site_name
        out.hostname = c.hostname
        out.iis_bindings = Array.isArray(c.bindings) ? (c.bindings as number[]).join(",") : ""
      } else {
        out.cert_path = c.cert_path
        out.key_path = c.key_path
        out.server_names = c.server_names
      }
      break
    case "k8s":
      out.namespace = c.namespace
      out.secret_name = c.secret_name
      break
    case "webhook":
      out.url = c.url
      break
  }
  return out as Partial<TargetForm>
}

interface TargetFieldsProps {
  type: string
  scene: string
  form: UseFormReturn<TargetForm>
  agents?: { id: string; name: string; status: string }[]
}

// Field 简易错误提示
function FieldError({ form, name }: { form: UseFormReturn<TargetForm>; name: keyof TargetForm }) {
  const err = form.formState.errors[name]
  if (!err) return null
  return <p className="text-sm text-destructive">{err.message}</p>
}

// TargetFields 按「通道 type × 场景 scene」联合渲染配置字段（创建/编辑共用）
function TargetFields({ type, scene, form, agents }: TargetFieldsProps) {
  const { register, watch, setValue } = form
  const isPFX = scene === "tomcat" || scene === "iis"

  return (
    <>
      {type === "local" && (
        <>
          {isPFX ? (
            <>
              <div className="space-y-2">
                <Label>PFX 路径</Label>
                <Input placeholder="C:\certs\example.pfx" {...register("pfx_path")} />
                <FieldError form={form} name="pfx_path" />
              </div>
              {scene === "iis" && (
                <>
                  <div className="grid grid-cols-2 gap-4">
                    <div className="space-y-2">
                      <Label>HTTPS 绑定端口（逗号分隔，如 443）</Label>
                      <Input placeholder="443" {...register("iis_bindings")} />
                      <FieldError form={form} name="iis_bindings" />
                    </div>
                    <div className="space-y-2">
                      <Label>IIS 站点名（可选）</Label>
                      <Input placeholder="Default Web Site" {...register("site_name")} />
                    </div>
                  </div>
                  <div className="space-y-2">
                    <Label>主机名（SNI，多站点同端口必填，如 www.example.com）</Label>
                    <Input placeholder="www.example.com（留空则通配，单站点可用）" {...register("hostname")} />
                    <p className="text-xs text-muted-foreground">同 IP 同端口多站点需不同主机名并启用 SNI，否则第二站点绑定会失败或主机名为空</p>
                  </div>
                </>
              )}
            </>
          ) : (
            <>
              <div className="space-y-2">
                <Label>证书路径{scene === "nginx" ? "（支持 {domain} 占位）" : ""}</Label>
                <Input placeholder={scene === "nginx" ? "/etc/nginx/certs/{domain}/fullchain.pem" : "/etc/ssl/certs/x.pem"} {...register("cert_path")} />
                <FieldError form={form} name="cert_path" />
                {scene === "nginx" && <p className="text-xs text-muted-foreground">同端口多域名：每域 1 目标，路径含 {"{domain}"} 自动按证书主域替换；SNI 由多 server_name 自动复用</p>}
              </div>
              <div className="space-y-2">
                <Label>私钥路径（可选）</Label>
                <Input placeholder={scene === "nginx" ? "/etc/nginx/certs/{domain}/privkey.pem" : "/etc/ssl/private/x.key"} {...register("key_path")} />
              </div>
              {scene === "nginx" && (
                <div className="space-y-2">
                  <Label>ServerName(s)（SNI，多域逗号分隔）</Label>
                  <Input placeholder="example.com www.example.com" {...register("server_names")} />
                  <p className="text-xs text-muted-foreground">空则取证书主域；多 server 块需每域独立目标</p>
                </div>
              )}
            </>
          )}
          {scene !== "iis" && (
            <div className="space-y-2">
              <Label>部署后命令（可选）</Label>
              <Input placeholder={scene === "nginx" ? "nginx -t && nginx -s reload" : "systemctl reload nginx"} {...register("post_command")} />
              {scene === "nginx" && <p className="text-xs text-muted-foreground">空则默认 nginx -t && nginx -s reload</p>}
            </div>
          )}
          {scene === "iis" && (
            <p className="text-xs text-muted-foreground">
              本机通道 IIS 场景需服务端为 Windows（合成 PFX + PowerShell 导入 + netsh/hostnameport + SNI 绑定）。
            </p>
          )}
        </>
      )}

      {type === "ssh" && (
        <>
          <div className="grid grid-cols-3 gap-4">
            <div className="space-y-2 col-span-2">
              <Label>主机</Label>
              <Input placeholder="10.0.0.5" {...register("host")} />
              <FieldError form={form} name="host" />
            </div>
            <div className="space-y-2">
              <Label>端口</Label>
              <Input placeholder="22" {...register("port")} />
              <FieldError form={form} name="port" />
            </div>
          </div>
          <div className="space-y-2">
            <Label>用户</Label>
            <Input placeholder="root" {...register("user")} />
            <FieldError form={form} name="user" />
          </div>
          <div className="space-y-2">
            <Label>SSH 私钥（PEM，推荐）</Label>
            <Input type="password" placeholder="-----BEGIN OPENSSH PRIVATE KEY-----" {...register("private_key")} />
            <FieldError form={form} name="private_key" />
          </div>
          <div className="space-y-2">
            <Label>或密码</Label>
            <Input type="password" {...register("password")} />
          </div>
          <div className="space-y-2">
            <Label>known_hosts（建议配置，防止中间人）</Label>
            <Input type="password" placeholder="host ssh-ed25519 AAAAC3..." {...register("known_hosts")} />
          </div>
          {isPFX ? (
            <>
              <div className="space-y-2">
                <Label>PFX 路径</Label>
                <Input placeholder="C:\certs\example.pfx" {...register("pfx_path")} />
                <FieldError form={form} name="pfx_path" />
              </div>
              {scene === "iis" && (
                <>
                  <div className="grid grid-cols-2 gap-4">
                    <div className="space-y-2">
                      <Label>HTTPS 绑定端口（逗号分隔，如 443）</Label>
                      <Input placeholder="443" {...register("iis_bindings")} />
                      <FieldError form={form} name="iis_bindings" />
                    </div>
                    <div className="space-y-2">
                      <Label>IIS 站点名（可选）</Label>
                      <Input placeholder="Default Web Site" {...register("site_name")} />
                    </div>
                  </div>
                  <div className="space-y-2">
                    <Label>主机名（SNI，多站点同端口必填）</Label>
                    <Input placeholder="www.example.com（留空通配）" {...register("hostname")} />
                    <p className="text-xs text-muted-foreground">同 IP 同端口多站点需不同主机名并启用 SNI</p>
                  </div>
                </>
              )}
              {scene === "tomcat" && (
                <div className="space-y-2">
                  <Label>部署后命令（可选，支持 {`{pfx_path} {pfx_password} {domain}`}）</Label>
                  <Input placeholder="替换 Tomcat keystore 并重启" {...register("hook")} />
                </div>
              )}
            </>
          ) : (
            <>
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label>证书路径{scene === "nginx" ? "（支持 {domain} 占位）" : ""}</Label>
                  <Input placeholder={scene === "nginx" ? "/etc/nginx/certs/{domain}/fullchain.pem" : "/etc/ssl/certs/x.pem"} {...register("cert_path")} />
                  <FieldError form={form} name="cert_path" />
                </div>
                <div className="space-y-2">
                  <Label>私钥路径（可选）</Label>
                  <Input placeholder={scene === "nginx" ? "/etc/nginx/certs/{domain}/privkey.pem" : "/etc/ssl/private/x.key"} {...register("key_path")} />
                </div>
              </div>
              {scene === "nginx" && (
                <div className="space-y-2">
                  <Label>ServerName(s)（SNI，多域逗号分隔）</Label>
                  <Input placeholder="example.com www.example.com" {...register("server_names")} />
                  <p className="text-xs text-muted-foreground">空则取证书主域；多 server 块需每域独立目标</p>
                </div>
              )}
              <div className="space-y-2">
                <Label>部署后命令（可选，支持 {`{cert_path} {key_path} {domain}`}）</Label>
                <Input placeholder={scene === "nginx" ? "nginx -t && nginx -s reload" : "nginx -s reload"} {...register("hook")} />
              </div>
            </>
          )}
        </>
      )}

      {type === "agent" && (
        <>
          <div className="space-y-2">
            <Label>Agent</Label>
            <Select value={watch("agent_id") || ""} onValueChange={(v) => setValue("agent_id", v ?? "")}>
              <SelectTrigger>
                <SelectValue>
                  {(v) =>
                    (v && agents?.find((a) => a.id === v)?.name) ??
                    "选择 Agent"
                  }
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {agents?.map((a) => (
                  <SelectItem key={a.id} value={a.id}>
                    {a.name}（{a.status === "online" ? "在线" : "离线"}）
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FieldError form={form} name="agent_id" />
          </div>
          {isPFX ? (
            <>
              <div className="space-y-2">
                <Label>PFX 路径</Label>
                <Input placeholder="C:\certs\example.pfx" {...register("pfx_path")} />
                <FieldError form={form} name="pfx_path" />
              </div>
              {scene === "iis" && (
                <>
                  <div className="grid grid-cols-2 gap-4">
                    <div className="space-y-2">
                      <Label>HTTPS 端口（逗号分隔，如 443）</Label>
                      <Input placeholder="443" {...register("iis_bindings")} />
                      <FieldError form={form} name="iis_bindings" />
                    </div>
                    <div className="space-y-2">
                      <Label>IIS 站点名（可选）</Label>
                      <Input placeholder="Default Web Site" {...register("site_name")} />
                    </div>
                  </div>
                  <div className="space-y-2">
                    <Label>主机名（SNI，多站点同端口必填）</Label>
                    <Input placeholder="www.example.com（留空通配）" {...register("hostname")} />
                    <p className="text-xs text-muted-foreground">同 IP 同端口多站点需不同主机名并启用 SNI</p>
                  </div>
                </>
              )}
              {scene === "tomcat" && (
                <div className="space-y-2">
                  <Label>部署后命令（可选）</Label>
                  <Input placeholder="替换 Tomcat keystore 并重启" {...register("hook")} />
                </div>
              )}
            </>
          ) : (
            <>
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label>证书路径{scene === "nginx" ? "（支持 {domain} 占位）" : ""}</Label>
                  <Input placeholder={scene === "nginx" ? "/etc/nginx/certs/{domain}/fullchain.pem" : "/etc/ssl/certs/x.pem"} {...register("cert_path")} />
                  <FieldError form={form} name="cert_path" />
                </div>
                <div className="space-y-2">
                  <Label>私钥路径</Label>
                  <Input placeholder={scene === "nginx" ? "/etc/nginx/certs/{domain}/privkey.pem" : "/etc/ssl/private/x.key"} {...register("key_path")} />
                </div>
              </div>
              {scene === "nginx" && (
                <div className="space-y-2">
                  <Label>ServerName(s)（SNI，多域逗号分隔）</Label>
                  <Input placeholder="example.com www.example.com" {...register("server_names")} />
                  <p className="text-xs text-muted-foreground">空则取证书主域；多 server 块需每域独立目标</p>
                </div>
              )}
              <div className="space-y-2">
                <Label>部署后命令（可选）</Label>
                <Input placeholder={scene === "nginx" ? "nginx -t && nginx -s reload" : "systemctl reload nginx"} {...register("hook")} />
              </div>
            </>
          )}
          <p className="text-xs text-muted-foreground">
            Agent 由业务机出站轮询拉取任务并执行，支持 NAT/无入站端口环境。
          </p>
        </>
      )}

      {type === "k8s" && (
        <>
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-2">
              <Label>命名空间</Label>
              <Input placeholder="default" {...register("namespace")} />
            </div>
            <div className="space-y-2">
              <Label>Secret 名称</Label>
              <Input placeholder="example-tls" {...register("secret_name")} />
              <FieldError form={form} name="secret_name" />
            </div>
          </div>
        </>
      )}

      {type === "webhook" && (
        <div className="space-y-2">
          <Label>Webhook URL（必须 https://）</Label>
          <Input placeholder="https://example.com/hook/cert" {...register("url")} />
          <FieldError form={form} name="url" />
        </div>
      )}
    </>
  )
}

export function DeployTargetsPage() {
  const queryClient = useQueryClient()
  const [dialogOpen, setDialogOpen] = useState(false)
  const CREATE_DEFAULT: TargetForm = {
    name: "",
    type: "ssh",
    scene: "generic",
    port: "22",
    host: "",
    user: "",
    private_key: "",
    password: "",
    known_hosts: "",
    cert_path: "",
    key_path: "",
    pfx_path: "",
    iis_bindings: "",
    site_name: "",
    hostname: "",
    server_names: "",
    post_command: "",
    hook: "",
    agent_id: "",
    namespace: "",
    secret_name: "",
    url: "",
  }

  const { data: targets, isLoading } = useQuery({
    queryKey: ["deploy-targets"],
    queryFn: async () => {
      const res = await api.get<DeployTarget[]>("/deploy-targets")
      return res.data
    },
  })

  const { data: agents } = useQuery({
    queryKey: ["agents"],
    queryFn: async () => {
      const res = await api.get<{ id: string; name: string; status: string }[]>("/agents")
      return res.data
    },
  })

  const createForm = useForm<TargetForm>({
    resolver: zodResolver(targetSchema),
    defaultValues: { type: "ssh", scene: "generic", port: "22" },
  })
  const { register, handleSubmit, watch, setValue, reset: resetCreate } = createForm
  const errors = createForm.formState.errors

  const type = watch("type")
  const scene = sceneChannels.includes(type) ? (watch("scene") || "generic") : ""
  const openCreateDialog = () => {
    resetCreate(CREATE_DEFAULT)
    setDialogOpen(true)
  }

  // 场景切换预填默认路径/命令（仅当对应字段为空时预填，避免覆盖用户已输入）
  const applyScenePreset = (targetScene: string, form: UseFormReturn<TargetForm>) => {
    const v = form.getValues()
    const set = (key: keyof TargetForm, val: string) => {
      if (!v[key]) form.setValue(key, val)
    }
    if (targetScene === "nginx") {
      set("cert_path", "/etc/nginx/certs/{domain}/fullchain.pem")
      set("key_path", "/etc/nginx/certs/{domain}/privkey.pem")
      set("server_names", "")
      if (form === createForm) set("hook", "nginx -t && nginx -s reload")
    } else if (targetScene === "apache") {
      set("cert_path", "/etc/apache2/ssl/cert.pem")
      set("key_path", "/etc/apache2/ssl/privkey.pem")
      if (form === createForm) set("hook", "systemctl reload apache2")
    } else if (targetScene === "generic") {
      set("cert_path", "/data/certs/fullchain.pem")
      set("key_path", "/data/certs/privkey.pem")
      if (form === createForm) set("hook", "")
    } else if (targetScene === "tomcat") {
      set("pfx_path", "/opt/tomcat/conf/cert.pfx")
    } else if (targetScene === "iis") {
      set("pfx_path", "C:\\certs\\cert.pfx")
      set("iis_bindings", "443")
      set("site_name", "Default Web Site")
    }
  }

  const create = useMutation({
    mutationFn: async (values: TargetForm) => {
      await api.post("/deploy-targets", {
        name: values.name,
        type: values.type,
        config: buildConfig(values),
      })
    },
    onSuccess: () => {
      toast.success("目标创建成功")
      queryClient.invalidateQueries({ queryKey: ["deploy-targets"] })
      resetCreate(CREATE_DEFAULT)
      setDialogOpen(false)
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  const [editTarget, setEditTarget] = useState<DeployTarget | null>(null)
  const [editOpen, setEditOpen] = useState(false)
  const [editEnabled, setEditEnabled] = useState(true)

  const editForm = useForm<TargetForm>({
    resolver: zodResolver(targetSchema),
    defaultValues: { type: "ssh", port: "22" },
  })

  const openEdit = async (t: DeployTarget) => {
    try {
      const res = await api.get<DeployTarget>(`/deploy-targets/${t.id}`)
      const full = res.data
      setEditTarget(full)
      setEditEnabled(full.enabled)
      const mapped = configToForm(full.type, full.config ?? {})
      editForm.reset({ name: full.name, type: full.type as TargetForm["type"], ...mapped })
      setEditOpen(true)
    } catch (err) {
      toast.error(getErrorMessage(err))
    }
  }

  const update = useMutation({
    mutationFn: async (values: TargetForm) => {
      await api.put(`/deploy-targets/${editTarget!.id}`, {
        name: values.name,
        type: values.type,
        config: buildConfig(values),
        enabled: editEnabled,
      })
    },
    onSuccess: () => {
      toast.success("目标已更新")
      queryClient.invalidateQueries({ queryKey: ["deploy-targets"] })
      setEditOpen(false)
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  const test = useMutation({
    mutationFn: async (id: string) => {
      await api.post(`/deploy-targets/${id}/test`)
    },
    onSuccess: () => {
      toast.success("连接测试通过")
      queryClient.invalidateQueries({ queryKey: ["deploy-targets"] })
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })

  const remove = useMutation({
    mutationFn: async (id: string) => {
      await api.delete(`/deploy-targets/${id}`)
    },
    onSuccess: () => {
      toast.success("已删除")
      queryClient.invalidateQueries({ queryKey: ["deploy-targets"] })
      setDeleteOpen(false)
      setDeleteTarget(null)
    },
    onError: (err) => toast.error(getErrorMessage(err)),
  })
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<DeployTarget | null>(null)

  return (
    <Card className="flex-1">
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle>部署目标</CardTitle>
        <Button onClick={openCreateDialog}>
          <Plus className="mr-2 size-4" />
          新建目标
        </Button>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>名称</TableHead>
              <TableHead>类型</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>创建时间</TableHead>
              <TableHead className="text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading && (
              <TableRow>
                <TableCell colSpan={5} className="text-center text-muted-foreground">加载中...</TableCell>
              </TableRow>
            )}
            {!isLoading && (!targets || targets.length === 0) && (
              <TableRow>
                <TableCell colSpan={5} className="text-center text-muted-foreground py-8">
                  <Server className="mx-auto mb-2 size-8 opacity-50" />
                  暂无部署目标，创建后证书签发/续期可自动推送
                </TableCell>
              </TableRow>
            )}
            {targets?.map((t) => (
              <TableRow key={t.id}>
                <TableCell className="font-medium">{t.name}</TableCell>
                <TableCell>{typeNames[t.type] ?? t.type}</TableCell>
                <TableCell>
                  {t.last_status === "success" ? (
                    <Badge variant="outline" className="bg-green-500/15 text-green-600 dark:text-green-400">正常</Badge>
                  ) : t.last_status === "error" ? (
                    <Badge variant="outline" className="bg-red-500/15 text-red-600 dark:text-red-400" title={t.last_error}>错误</Badge>
                  ) : (
                    <span className="text-muted-foreground">未测试</span>
                  )}
                </TableCell>
                <TableCell>{new Date(t.created_at).toLocaleDateString()}</TableCell>
                <TableCell className="text-right space-x-1">
                  <Button variant="outline" size="sm" onClick={() => openEdit(t)}>
                    <Eye className="mr-1 size-4" />
                    编辑
                  </Button>
                  <Button variant="outline" size="sm" onClick={() => test.mutate(t.id)} disabled={test.isPending}>
                    <PlayCircle className="mr-1 size-4" />
                    测试
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="text-destructive"
                    onClick={() => {
                      setDeleteTarget(t)
                      setDeleteOpen(true)
                    }}
                  >
                    <Trash2 className="size-4" />
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>

      <Dialog open={dialogOpen} onOpenChange={(open) => { setDialogOpen(open); if (open) resetCreate(CREATE_DEFAULT); else resetCreate(CREATE_DEFAULT) }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>新建部署目标</DialogTitle>
            <DialogDescription>配置证书推送通道（SSH / Agent / K8s / Webhook / 本机）</DialogDescription>
          </DialogHeader>
          <form className="space-y-4" onSubmit={handleSubmit((v) => create.mutate(v))}>
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label>目标名称</Label>
                <Input placeholder="生产 nginx-01" {...register("name")} />
                {errors.name && <p className="text-sm text-destructive">{errors.name.message}</p>}
              </div>
              <div className="space-y-2">
                <Label>类型</Label>
                <Select value={type} onValueChange={(v) => setValue("type", v as TargetForm["type"])}>
                  <SelectTrigger>
                    <SelectValue>{(v) => typeNames[v as string] ?? v}</SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="ssh">SSH</SelectItem>
                    <SelectItem value="agent">Agent</SelectItem>
                    <SelectItem value="local">本机</SelectItem>
                    <SelectItem value="k8s">Kubernetes Secret</SelectItem>
                    <SelectItem value="webhook">Webhook</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>

            {sceneChannels.includes(type) && (
              <div className="space-y-2">
                <Label>应用场景</Label>
                <Select
                  value={watch("scene") || "generic"}
                  onValueChange={(v) => {
                    setValue("scene", (v || "generic") as TargetForm["scene"])
                    applyScenePreset(v || "generic", createForm)
                  }}
                >
                  <SelectTrigger>
                    <SelectValue>{(v) => sceneNames[(v as string) || "generic"]}</SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="generic">通用</SelectItem>
                    <SelectItem value="nginx">Nginx</SelectItem>
                    <SelectItem value="apache">Apache</SelectItem>
                    <SelectItem value="tomcat">Tomcat</SelectItem>
                    <SelectItem value="iis">Windows IIS</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            )}

            <TargetFields type={type} scene={scene} form={createForm} agents={agents} />

            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => { resetCreate(CREATE_DEFAULT); setDialogOpen(false) }}>
                取消
              </Button>
              <Button type="submit" disabled={create.isPending}>
                {create.isPending ? "创建中..." : "创建"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* 查看/编辑目标 */}
      <Dialog open={editOpen} onOpenChange={(open) => { setEditOpen(open); if (!open) { setEditTarget(null); editForm.reset({ type: "ssh", port: "22" }) } }}>
        <DialogContent className="max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>编辑部署目标：{editTarget?.name}</DialogTitle>
            <DialogDescription>修改名称与推送配置，保存后立即生效</DialogDescription>
          </DialogHeader>
          <form className="space-y-4" onSubmit={editForm.handleSubmit((v) => update.mutate(v))}>
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label>目标名称</Label>
                <Input placeholder="生产 nginx-01" {...editForm.register("name")} />
                {editForm.formState.errors.name && (
                  <p className="text-sm text-destructive">{editForm.formState.errors.name.message}</p>
                )}
              </div>
              <div className="space-y-2">
                <Label>类型（不可修改）</Label>
                <Input readOnly value={typeNames[editForm.watch("type") as string] ?? editForm.watch("type")} />
              </div>
            </div>

            {sceneChannels.includes(editForm.watch("type")) && (
              <div className="space-y-2">
                <Label>应用场景</Label>
                <Select
                  value={editForm.watch("scene") || "generic"}
                  onValueChange={(v) => {
                    editForm.setValue("scene", (v || "generic") as TargetForm["scene"])
                    applyScenePreset(v || "generic", editForm)
                  }}
                >
                  <SelectTrigger>
                    <SelectValue>{(v) => sceneNames[(v as string) || "generic"]}</SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="generic">通用</SelectItem>
                    <SelectItem value="nginx">Nginx</SelectItem>
                    <SelectItem value="apache">Apache</SelectItem>
                    <SelectItem value="tomcat">Tomcat</SelectItem>
                    <SelectItem value="iis">Windows IIS</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            )}

            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                className="size-4 accent-primary"
                checked={editEnabled}
                onChange={(e) => setEditEnabled(e.target.checked)}
              />
              启用目标（关闭后停止推送）
            </label>

            <TargetFields
              type={editForm.watch("type")}
              scene={sceneChannels.includes(editForm.watch("type")) ? (editForm.watch("scene") || "generic") : ""}
              form={editForm}
              agents={agents}
            />

            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setEditOpen(false)}>
                取消
              </Button>
              <Button type="submit" disabled={update.isPending}>
                {update.isPending ? "保存中..." : "保存"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>确认删除目标？</DialogTitle>
            <DialogDescription>删除目标 {deleteTarget?.name} 后，已绑定证书将解除关联且无法自动部署。</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteOpen(false)}>取消</Button>
            <Button
              variant="destructive"
              disabled={remove.isPending}
              onClick={() => deleteTarget && remove.mutate(deleteTarget.id)}
            >
              {remove.isPending ? "删除中..." : "确认删除"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  )
}
