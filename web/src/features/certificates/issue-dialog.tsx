import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
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
import { api, getErrorMessage } from "@/lib/api"
import { NOTIFICATION_EVENTS, type AcmeAccount, type ContactPerson, type DnsProviderAccount } from "@/lib/types"

const issueSchema = z.object({
  domains: z.string().optional(),
  account_id: z.string().min(1, "请选择渠道账户"),
  contact_id: z.string().optional(),
  verification: z.enum(["http01", "dns01"]),
  csr_mode: z.boolean().optional(),
  csr_pem: z.string().optional(),
  deploy_target_ids: z.array(z.string()).optional(),
  notify_events: z.array(z.string()).optional(),
  dns_provider: z.string().optional(),
  dns_access_key: z.string().optional(),
  dns_secret: z.string().optional(),
  dns_email: z.string().optional(),
  dns_account_id: z.string().optional(),
}).superRefine((data, ctx) => {
  if (data.csr_mode && !data.csr_pem?.trim()) {
    ctx.addIssue({ code: z.ZodIssueCode.custom, path: ["csr_pem"], message: "请粘贴 CSR（PEM）" })
  }
  if (!data.csr_mode && !data.domains?.trim()) {
    ctx.addIssue({ code: z.ZodIssueCode.custom, path: ["domains"], message: "至少输入一个域名" })
  }
  // DNS-01（非云厂商）需凭据：优先使用 DNS 账户，否则需手动凭据
  if (!data.csr_mode && data.verification === "dns01") {
    const isCloud = data.account_id && (accountsLookup?.[data.account_id] ?? "") !== "acme"
    if (!isCloud && !data.dns_account_id?.trim() && !data.dns_access_key?.trim()) {
      ctx.addIssue({ code: z.ZodIssueCode.custom, path: ["dns_access_key"], message: "请选择 DNS 账户或手动输入凭据" })
    }
  }
})

// accountsLookup 由组件挂载时填充（避免 schema 定义处访问组件状态）
const accountsLookup: Record<string, string> = {}

type IssueForm = z.infer<typeof issueSchema>

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function IssueDialog({ open, onOpenChange }: Props) {
  const queryClient = useQueryClient()
  const { data: accounts } = useQuery({
    queryKey: ["acme-accounts"],
    queryFn: async () => {
      const res = await api.get<AcmeAccount[]>("/acme-accounts")
      return res.data
    },
  })

  const { data: deployTargets } = useQuery({
    queryKey: ["deploy-targets"],
    queryFn: async () => {
      const res = await api.get<{ id: string; name: string; type: string }[]>("/deploy-targets")
      return res.data
    },
  })

  const { data: contacts } = useQuery({
    queryKey: ["contacts"],
    queryFn: async () => {
      const res = await api.get<ContactPerson[]>("/settings/contacts")
      return res.data
    },
  })

  const { data: dnsAccounts } = useQuery({
    queryKey: ["dns-accounts"],
    queryFn: async () => {
      const res = await api.get<DnsProviderAccount[]>("/dns-accounts")
      return res.data
    },
  })

  const {
    register,
    handleSubmit,
    watch,
    setValue,
    formState: { errors },
  } = useForm<IssueForm>({
    resolver: zodResolver(issueSchema),
    defaultValues: {
      verification: "http01",
      csr_mode: false,
      deploy_target_ids: [],
      notify_events: NOTIFICATION_EVENTS.map((e) => e.value),
      dns_provider: "alidns",
    },
  })

  // 填充 provider 查询表（供 schema 校验判断是否云厂商账户）
  accounts?.forEach((a) => {
    accountsLookup[a.id] = a.provider
  })

  const buildDNSConfig = (values: IssueForm): Record<string, string> | undefined => {
    if (values.verification !== "dns01" || values.csr_mode) return undefined
    if (values.dns_account_id) return undefined
    const p = values.dns_provider ?? "alidns"
    switch (p) {
      case "alidns":
        return {
          provider: "alidns",
          ALICLOUD_ACCESS_KEY: values.dns_access_key ?? "",
          ALICLOUD_SECRET_KEY: values.dns_secret ?? "",
        }
      case "cloudflare":
        return {
          provider: "cloudflare",
          CLOUDFLARE_DNS_API_TOKEN: values.dns_access_key ?? "",
          CLOUDFLARE_API_KEY: values.dns_secret ?? "",
          CLOUDFLARE_EMAIL: values.dns_email ?? "",
        }
      case "dnspod":
        return { provider: "dnspod", DNSPOD_API_TOKEN: values.dns_access_key ?? "" }
      case "godaddy":
        return { provider: "godaddy", GODADDY_API_KEY: values.dns_access_key ?? "", GODADDY_API_SECRET: values.dns_secret ?? "" }
      default:
        return undefined
    }
  }

  const issue = useMutation({
    mutationFn: async (values: IssueForm) => {
      const domains = values.domains
        ? values.domains.split(/[\s,]+/).filter(Boolean).map((d) => d.trim())
        : []
      await api.post("/certificates", {
        domains,
        account_id: values.account_id,
        contact_id: values.contact_id,
        verification: values.verification,
        deploy_target_ids: values.deploy_target_ids,
        notify_events: values.notify_events,
        csr_pem: values.csr_mode ? values.csr_pem : "",
        dns_config: buildDNSConfig(values),
        dns_provider_account_id: values.dns_account_id || undefined,
      })
    },
    onSuccess: () => {
      toast.success("签发成功")
      queryClient.invalidateQueries({ queryKey: ["certificates"] })
      onOpenChange(false)
    },
    onError: (err) => {
      toast.error(getErrorMessage(err))
    },
  })

  const selectedAccount = accounts?.find((a) => a.id === watch("account_id"))
  const isCloudProvider = selectedAccount && selectedAccount.provider !== "acme"
  const csrMode = watch("csr_mode") ?? false

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>签发证书</DialogTitle>
          <DialogDescription>通过所选渠道为域名签发 SSL 证书</DialogDescription>
        </DialogHeader>
        <form
          className="space-y-4"
          onSubmit={handleSubmit((v) => issue.mutate(v))}
        >
          <div className="space-y-2">
            <Label>渠道账户</Label>
            <Select
              value={watch("account_id")}
              onValueChange={(v) => {
                setValue("account_id", v ?? "")
                setValue("contact_id", "")
                setValue("csr_mode", false)
              }}
            >
              <SelectTrigger>
                <SelectValue>
                  {(v) =>
                    (v && accounts?.find((a) => a.id === v)?.name) ??
                    "选择渠道账户"
                  }
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {accounts?.map((a) => (
                  <SelectItem key={a.id} value={a.id}>
                    {a.name}（{a.provider}）
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {errors.account_id && (
              <p className="text-sm text-destructive">{errors.account_id.message}</p>
            )}
          </div>
          {!isCloudProvider && selectedAccount && (
            <label className="flex cursor-pointer items-center justify-between rounded-md border p-3">
              <span className="text-sm font-medium">CSR 模式（私钥保留在客户端）</span>
              <input
                type="checkbox"
                className="size-4 accent-primary"
                checked={csrMode}
                onChange={(e) => setValue("csr_mode", e.target.checked)}
              />
            </label>
          )}
          {csrMode ? (
            <div className="space-y-2">
              <Label htmlFor="csr_pem">CSR（PEM 格式）</Label>
              <textarea
                id="csr_pem"
                placeholder="-----BEGIN CERTIFICATE REQUEST-----..."
                className="min-h-32 w-full rounded-md border border-input bg-transparent px-3 py-2 font-mono text-xs shadow-xs placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                {...register("csr_pem")}
              />
              <p className="text-xs text-muted-foreground">
                域名从 CSR 自动解析；签发后证书可在界面下载，私钥由你自行保管
              </p>
              {errors.csr_pem && <p className="text-sm text-destructive">{errors.csr_pem.message}</p>}
            </div>
          ) : (
            <div className="space-y-2">
              <Label htmlFor="domains">域名（多个用逗号分隔）</Label>
              <Input
                id="domains"
                placeholder="example.com, www.example.com"
                {...register("domains")}
              />
              {errors.domains && (
                <p className="text-sm text-destructive">{errors.domains.message}</p>
              )}
            </div>
          )}
          {!isCloudProvider && (
            <div className="space-y-2">
              <Label>域名验证方式</Label>
              <Select
                value={watch("verification")}
                onValueChange={(v) => setValue("verification", v as "http01" | "dns01")}
              >
                <SelectTrigger>
                  <SelectValue>
                    {(v) =>
                      v === "dns01" ? "DNS-01（需 DNS API 凭据）" : "HTTP-01（需 80 端口可达）"
                    }
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="http01">HTTP-01（需 80 端口可达）</SelectItem>
                  <SelectItem value="dns01">DNS-01（需 DNS API 凭据）</SelectItem>
                </SelectContent>
              </Select>
            </div>
          )}
          {isCloudProvider && (
            <>
              <p className="text-xs text-muted-foreground">
                云厂商渠道将使用其平台域名验证（DNS 自动验证需域名托管在对应云 DNS）。
              </p>
              <div className="space-y-2">
                <Label>申请人</Label>
                <Select
                  value={watch("contact_id") ?? ""}
                  onValueChange={(v) => setValue("contact_id", v ?? "")}
                >
                  <SelectTrigger>
                    <SelectValue>
                      {(v) => {
                        if (!v) return "使用默认（第一个）申请人"
                        const c = contacts?.find((x) => x.id === v)
                        return c ? `${c.name}（${c.email}）` : "选择申请人"
                      }}
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    {contacts && contacts.length > 0 ? (
                      contacts.map((c) => (
                        <SelectItem key={c.id} value={c.id}>
                          {c.name}（{c.email}）
                        </SelectItem>
                      ))
                    ) : (
                      <SelectItem value="__none" disabled>
                        未配置申请人，请到 系统→申请人信息 添加
                      </SelectItem>
                    )}
                  </SelectContent>
                </Select>
                {(!contacts || contacts.length === 0) && (
                  <p className="text-xs text-destructive">
                    云渠道签发需要申请人信息，请先到 系统→申请人信息 配置
                  </p>
                )}
              </div>
            </>
          )}
          {!isCloudProvider && watch("verification") === "dns01" && (
            <div className="space-y-2 rounded-md border p-3">
              <Label>DNS-01 凭据（用于自动添加 TXT 验证记录）</Label>
              <div className="space-y-2">
                <Label className="text-xs text-muted-foreground">DNS 验证账户（推荐，可复用）</Label>
                <Select
                  value={watch("dns_account_id") ?? ""}
                  onValueChange={(v) => setValue("dns_account_id", v === "__manual" ? "" : (v ?? ""))}
                >
                  <SelectTrigger>
                    <SelectValue>
                      {(v) => {
                        if (!v || v === "__manual") return "手动输入凭据"
                        const a = dnsAccounts?.find((x) => x.id === v)
                        return a ? `${a.name}（${a.provider}）` : "选择 DNS 账户"
                      }}
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="__manual">手动输入凭据</SelectItem>
                    {dnsAccounts?.map((a) => (
                      <SelectItem key={a.id} value={a.id}>
                        {a.name}（{a.provider}）
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                {!watch("dns_account_id") && (
                  <>
                    <Select
                      value={watch("dns_provider")}
                      onValueChange={(v) => setValue("dns_provider", v ?? "alidns")}
                    >
                      <SelectTrigger>
                        <SelectValue>
                          {(v) => (v ? `DNS 提供商：${v}` : "选择 DNS 提供商")}
                        </SelectValue>
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="alidns">阿里云 DNS</SelectItem>
                        <SelectItem value="cloudflare">Cloudflare</SelectItem>
                        <SelectItem value="dnspod">DNSPod</SelectItem>
                        <SelectItem value="godaddy">GoDaddy</SelectItem>
                      </SelectContent>
                    </Select>
                    {watch("dns_provider") === "alidns" && (
                      <>
                        <Input placeholder="AccessKey ID" {...register("dns_access_key")} />
                        <Input
                          type="password"
                          placeholder="AccessKey Secret"
                          {...register("dns_secret")}
                        />
                        <p className="text-xs text-muted-foreground">
                          阿里云 RAM 子账号，需域名解析（DNS）管理权限；域名须托管在阿里云云解析
                        </p>
                      </>
                    )}
                    {watch("dns_provider") === "cloudflare" && (
                      <>
                        <Input
                          type="password"
                          placeholder="API Token（推荐）或 API Key"
                          {...register("dns_access_key")}
                        />
                        <Input
                          type="password"
                          placeholder="Global API Key（使用 Token 时留空）"
                          {...register("dns_secret")}
                        />
                        <Input placeholder="账户邮箱（使用 API Key 时必填）" {...register("dns_email")} />
                      </>
                    )}
                    {watch("dns_provider") === "dnspod" && (
                      <Input
                        type="password"
                        placeholder="DNSPod Token（ID,Token 格式）"
                        {...register("dns_access_key")}
                      />
                    )}
                    {watch("dns_provider") === "godaddy" && (
                      <>
                        <Input type="password" placeholder="API Key" {...register("dns_access_key")} />
                        <Input type="password" placeholder="API Secret" {...register("dns_secret")} />
                      </>
                    )}
                  </>
                )}
                {watch("dns_account_id") && (
                  <p className="text-xs text-muted-foreground">
                    已选择 DNS 账户，签发与续期将复用该账户凭据；如需更换请重新选择。
                  </p>
                )}
              </div>
            </div>
          )}
          {deployTargets && deployTargets.length > 0 && (
            <div className="space-y-2">
              <Label>部署目标（签发成功后自动部署）</Label>
              <div className="flex flex-wrap gap-2">
                {deployTargets.map((t) => {
                  const selected = watch("deploy_target_ids")?.includes(t.id) ?? false
                  return (
                    <Button
                      key={t.id}
                      type="button"
                      size="sm"
                      variant={selected ? "default" : "outline"}
                      onClick={() => {
                        const cur = watch("deploy_target_ids") ?? []
                        setValue(
                          "deploy_target_ids",
                          selected ? cur.filter((x) => x !== t.id) : [...cur, t.id],
                        )
                      }}
                    >
                      {t.name}
                    </Button>
                  )
                })}
              </div>
            </div>
          )}
          <div className="space-y-2">
            <Label>通知事件（勾选的事件触发该证书的通知；未配置时全部事件可通知）</Label>
            <div className="space-y-1 rounded-md border p-3">
              {NOTIFICATION_EVENTS.map((e) => {
                const selected = watch("notify_events")?.includes(e.value) ?? false
                return (
                  <label
                    key={e.value}
                    className="flex cursor-pointer items-center justify-between gap-2 text-sm"
                  >
                    <span>{e.label}</span>
                    <input
                      type="checkbox"
                      className="size-4 accent-primary"
                      checked={selected}
                      onChange={() => {
                        const cur = watch("notify_events") ?? []
                        setValue(
                          "notify_events",
                          selected ? cur.filter((x) => x !== e.value) : [...cur, e.value],
                        )
                      }}
                    />
                  </label>
                )
              })}
            </div>
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              取消
            </Button>
            <Button type="submit" disabled={issue.isPending}>
              {issue.isPending ? "签发中..." : "签发"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
