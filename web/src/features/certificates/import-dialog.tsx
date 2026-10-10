import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { toast } from "sonner"
import { useEffect, useState } from "react"
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
import { Textarea } from "@/components/ui/textarea"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { api, getErrorMessage } from "@/lib/api"
import { readFileAsBase64, readFileAsText } from "@/lib/file"
import { NOTIFICATION_EVENTS } from "@/lib/types"

// 导入证书不触发签发事件，仅保留后续可触发的事件
const IMPORT_NOTIFY_EVENTS = NOTIFICATION_EVENTS.filter(
  (e) => e.value !== "issue_success" && e.value !== "issue_failed",
)

const pemSchema = z.object({
  domains: z.string().min(1, "至少输入一个域名"),
  certificate: z.string().min(1, "证书内容不能为空"),
  private_key: z.string().min(1, "私钥内容不能为空"),
})

const pfxSchema = z.object({
  pfx: z.string().min(1, "请选择 PFX/P12 文件"),
  pfx_password: z.string(),
})

type PemForm = z.infer<typeof pemSchema>
type PfxForm = z.infer<typeof pfxSchema>

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function ImportDialog({ open, onOpenChange }: Props) {
  const queryClient = useQueryClient()
  const [mode, setMode] = useState<"pem" | "pfx">("pem")
  const [deployTargetIds, setDeployTargetIds] = useState<string[]>([])
  const [notifyEvents, setNotifyEvents] = useState<string[]>(IMPORT_NOTIFY_EVENTS.map((e) => e.value))

  const pem = useForm<PemForm>({ resolver: zodResolver(pemSchema) })
  const pfx = useForm<PfxForm>({ resolver: zodResolver(pfxSchema) })

  // 打开对话框时重置所有状态，避免上次（含私钥/PFX 内容）残留泄漏
  useEffect(() => {
    if (open) {
      setMode("pem")
      setDeployTargetIds([])
      setNotifyEvents(IMPORT_NOTIFY_EVENTS.map((e) => e.value))
      pem.reset()
      pfx.reset()
    }
  }, [open, pem, pfx])

  const { data: deployTargets } = useQuery({
    queryKey: ["deploy-targets"],
    queryFn: async () => {
      const res = await api.get<{ id: string; name: string; type: string }[]>("/deploy-targets")
      return res.data
    },
  })

  const importCert = useMutation({
    mutationFn: async (values: PemForm | PfxForm) => {
      const body: Record<string, unknown> = { deploy_target_ids: deployTargetIds, notify_events: notifyEvents }
      if (mode === "pfx") {
        const v = values as PfxForm
        body.pfx = v.pfx
        body.pfx_password = v.pfx_password
        body.verification = "dns01"
      } else {
        const v = values as PemForm
        body.domains = v.domains.split(/[\s,]+/).filter(Boolean).map((d) => d.trim())
        body.certificate = v.certificate
        body.private_key = v.private_key
        body.verification = "dns01"
      }
      await api.post("/certificates/import", body)
    },
    onSuccess: () => {
      toast.success("导入成功")
      queryClient.invalidateQueries({ queryKey: ["certificates"] })
      setDeployTargetIds([])
      onOpenChange(false)
    },
    onError: (err) => {
      toast.error(getErrorMessage(err))
    },
  })

  const handlePemFile = async (field: "certificate" | "private_key", file?: File) => {
    if (!file) return
    try {
      const text = await readFileAsText(file)
      pem.setValue(field, text)
    } catch {
      toast.error("读取文件失败")
    }
  }

  const handlePfxFile = async (file?: File) => {
    if (!file) return
    try {
      const b64 = await readFileAsBase64(file)
      pfx.setValue("pfx", b64)
    } catch {
      toast.error("读取文件失败")
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>导入证书</DialogTitle>
          <DialogDescription>导入已有证书（支持 PEM 文本或 PFX/P12 文件，如云厂商签发）</DialogDescription>
        </DialogHeader>
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault()
            if (mode === "pfx") {
              pfx.handleSubmit((v) => importCert.mutate(v))(e)
            } else {
              pem.handleSubmit((v) => importCert.mutate(v))(e)
            }
          }}
        >
          <div className="space-y-2">
            <Label>导入方式</Label>
            <Select value={mode} onValueChange={(v) => setMode(v as "pem" | "pfx")}>
              <SelectTrigger>
                <SelectValue>
                  {(v) => (v === "pfx" ? "PFX / P12 文件（Windows/IIS）" : "PEM 文本（证书 + 私钥）")}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="pem">PEM 文本（证书 + 私钥）</SelectItem>
                <SelectItem value="pfx">PFX / P12 文件（Windows/IIS）</SelectItem>
              </SelectContent>
            </Select>
          </div>

          {mode === "pem" ? (
            <>
              <div className="space-y-2">
                <Label>域名（多个用逗号分隔）</Label>
                <Input placeholder="example.com" {...pem.register("domains")} />
                {pem.formState.errors.domains && (
                  <p className="text-sm text-destructive">{pem.formState.errors.domains.message}</p>
                )}
              </div>
              <div className="space-y-2">
                <Label>证书 PEM（可含证书链）</Label>
                <Textarea
                  className="font-mono text-xs h-24"
                  placeholder="-----BEGIN CERTIFICATE-----"
                  {...pem.register("certificate")}
                />
                <Input
                  type="file"
                  accept=".pem,.crt,.cer,.cert,.pem,.crt"
                  onChange={(e) => handlePemFile("certificate", e.target.files?.[0])}
                />
                {pem.formState.errors.certificate && (
                  <p className="text-sm text-destructive">{pem.formState.errors.certificate.message}</p>
                )}
              </div>
              <div className="space-y-2">
                <Label>私钥 PEM</Label>
                <Textarea
                  className="font-mono text-xs h-24"
                  placeholder="-----BEGIN EC PRIVATE KEY-----"
                  {...pem.register("private_key")}
                />
                <Input
                  type="file"
                  accept=".key,.pem"
                  onChange={(e) => handlePemFile("private_key", e.target.files?.[0])}
                />
                {pem.formState.errors.private_key && (
                  <p className="text-sm text-destructive">{pem.formState.errors.private_key.message}</p>
                )}
              </div>
            </>
          ) : (
            <>
              <div className="space-y-2">
                <Label>PFX / P12 文件</Label>
                <Input type="file" accept=".pfx,.p12,.pem" onChange={(e) => handlePfxFile(e.target.files?.[0])} />
                <p className="text-xs text-muted-foreground">多域名将自动从证书 SAN 解析，无需填写域名</p>
                {pfx.formState.errors.pfx && (
                  <p className="text-sm text-destructive">{pfx.formState.errors.pfx.message}</p>
                )}
              </div>
              <div className="space-y-2">
                <Label>PFX 密码</Label>
                <Input type="password" placeholder="PFX 文件的密码" {...pfx.register("pfx_password")} />
              </div>
            </>
          )}

          {deployTargets && deployTargets.length > 0 && (
            <div className="space-y-2">
              <Label>部署目标（导入成功后自动部署）</Label>
              <div className="flex flex-wrap gap-2">
                {deployTargets.map((t) => {
                  const selected = deployTargetIds.includes(t.id)
                  return (
                    <Button
                      key={t.id}
                      type="button"
                      size="sm"
                      variant={selected ? "default" : "outline"}
                      onClick={() => {
                        setDeployTargetIds(
                          selected ? deployTargetIds.filter((x) => x !== t.id) : [...deployTargetIds, t.id],
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
              {IMPORT_NOTIFY_EVENTS.map((e) => {
                const selected = notifyEvents.includes(e.value)
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
                      onChange={() =>
                        setNotifyEvents(
                          selected
                            ? notifyEvents.filter((x) => x !== e.value)
                            : [...notifyEvents, e.value],
                        )
                      }
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
            <Button type="submit" disabled={importCert.isPending}>
              {importCert.isPending ? "导入中..." : "导入"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
