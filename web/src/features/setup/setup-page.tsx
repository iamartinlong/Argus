import { useEffect, useState } from "react"
import { useNavigate } from "react-router-dom"
import { useMutation } from "@tanstack/react-query"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { CheckCircle2, Loader2, ShieldCheck } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import {
  fetchSetupStatus,
  generateSetupKeys,
  getSetupErrorMessage,
  publicSetupRestart,
  submitSetupInit,
  type SetupInitResult,
  type SetupStatus,
} from "./setup-api"
import { api } from "@/lib/api"
import { useAuthStore } from "@/stores/auth-store"

const setupSchema = z
  .object({
    jwtSecret: z.string().optional().or(z.literal("")),
    masterKey: z.string().optional().or(z.literal("")),
    minPasswordLen: z.number().min(8, "需在 8~64 之间").max(64, "需在 8~64 之间"),
    maxLoginFailures: z.number().min(1, "需在 1~100 之间").max(100, "需在 1~100 之间"),
    lockoutMinutes: z.number().min(1, "需在 1~1440 之间").max(1440, "需在 1~1440 之间"),
    adminUsername: z.string().min(1, "请输入管理员用户名").max(64, "不超过 64 字符"),
    adminPassword: z.string().min(1, "请输入管理员密码"),
    confirmPassword: z.string(),
  })
  .refine((v) => v.adminPassword === v.confirmPassword, {
    message: "两次输入的密码不一致",
    path: ["confirmPassword"],
  })

type SetupForm = z.infer<typeof setupSchema>

export function SetupPage() {
  const navigate = useNavigate()
  const [status, setStatus] = useState<SetupStatus | null>(null)
  const [checking, setChecking] = useState(true)
  const [step, setStep] = useState(0)
  const [error, setError] = useState<string | null>(null)
  const [result, setResult] = useState<SetupInitResult | null>(null)
  const [restarting, setRestarting] = useState(false)
  const [restartInfo, setRestartInfo] = useState<string | null>(null)
  const [authed, setAuthed] = useState(false)
  const [finishing, setFinishing] = useState(false)
  const setAuth = useAuthStore((s) => s.setAuth)
  const setupOnly = status?.setup_only ?? false

  useEffect(() => {
    fetchSetupStatus()
      .then((s) => {
        if (s.initialized) {
          navigate("/login", { replace: true })
          return
        }
        setStatus(s)
      })
      .catch(() => setError("无法连接服务端，请确认 Argus 已启动"))
      .finally(() => setChecking(false))
  }, [navigate])

  const {
    register,
    handleSubmit,
    setValue,
    watch,
    formState: { errors },
  } = useForm<SetupForm>({
    resolver: zodResolver(setupSchema),
    defaultValues: {
      jwtSecret: "",
      masterKey: "",
      minPasswordLen: 12,
      maxLoginFailures: 5,
      lockoutMinutes: 15,
      adminUsername: "admin",
      adminPassword: "",
      confirmPassword: "",
    },
  })

  useEffect(() => {
    if (status) setValue("minPasswordLen", status.min_password_len || 12)
  }, [status, setValue])

  const minLen = watch("minPasswordLen") || 12

  const genKeys = useMutation({
    mutationFn: generateSetupKeys,
    onSuccess: (keys) => {
      setValue("jwtSecret", keys.jwt_secret)
      setValue("masterKey", keys.master_key)
      setError(null)
    },
    onError: (err) => setError(getSetupErrorMessage(err)),
  })

  const submit = useMutation({
    mutationFn: async (values: SetupForm) => {
      // 密码复杂度与后端一致：长度 + 大小写 + 数字
      if (values.adminPassword.length < minLen) {
        throw new Error(`密码长度至少 ${minLen} 位`)
      }
      if (!/[a-z]/.test(values.adminPassword) || !/[A-Z]/.test(values.adminPassword) || !/[0-9]/.test(values.adminPassword)) {
        throw new Error("密码必须包含大写字母、小写字母和数字")
      }
      return submitSetupInit({
        jwt_secret: values.jwtSecret || undefined,
        master_key: values.masterKey || undefined,
        min_password_len: values.minPasswordLen,
        max_login_failures: values.maxLoginFailures,
        lockout_minutes: values.lockoutMinutes,
        admin_username: values.adminUsername.trim(),
        admin_password: values.adminPassword,
      })
    },
    onSuccess: (data, values) => {
      setResult(data)
      setError(null)
      // 完整模式：用刚设置的凭据自动登录；需重启则顺手触发认证重启
      if (!setupOnly) {
        void autoLoginAndRestart(data, values.adminUsername.trim(), values.adminPassword)
      }
    },
    onError: (err) => setError(getSetupErrorMessage(err)),
  })

  // 完整模式收尾：自动登录（免二次输入），需重启则带鉴权重起服务
  const autoLoginAndRestart = async (data: SetupInitResult, username: string, password: string) => {
    setFinishing(true)
    try {
      const login = await api.post<{
        access_token: string
        must_change_password?: boolean
        user: { id: string; username: string; role: string }
      }>("/auth/login", { username, password })
      setAuth(login.data.access_token, login.data.user, login.data.must_change_password)
      setAuthed(true)
      if (!data.needs_restart) return
      try {
        const token = login.data.access_token
        const res = await api.post<{
          restarting?: boolean
          needs_manual_restart?: boolean
          message?: string
        }>("/setup/restart", {}, { headers: { Authorization: `Bearer ${token}` } })
        if (res.data.restarting) {
          setRestartInfo("新配置已生效并正在重启，约 5 秒后进入系统…")
        } else {
          setRestartInfo(res.data.message ?? "请手动重启服务使新配置生效")
        }
      } catch {
        setRestartInfo("自动重启失败：请进入系统后手动重启服务（Windows 服务：Restart-Service Argus）。")
      }
    } catch {
      setRestartInfo("自动登录失败：请点击“去登录”手动登录。")
    } finally {
      setFinishing(false)
    }
  }

  // 安装模式：凭初始化返回的令牌重启，重启后才能登录
  const doRestart = async () => {
    setRestarting(true)
    try {
      const d = await publicSetupRestart(result?.restart_token)
      if (d.restarting) {
        setRestartInfo("正在重启服务，约 5 秒后请点击“去登录”…")
      } else {
        setRestartInfo(d.message ?? "请手动重启服务使配置生效")
      }
    } catch (err) {
      setRestartInfo(getSetupErrorMessage(err))
    } finally {
      setRestarting(false)
    }
  }

  if (checking) {
    return (
      <div className="flex min-h-svh items-center justify-center text-muted-foreground">
        <Loader2 className="mr-2 size-4 animate-spin" /> 正在检查系统状态...
      </div>
    )
  }

  if (!status) {
    return (
      <div className="flex min-h-svh items-center justify-center bg-muted/40 p-4">
        <Card className="w-full max-w-sm">
          <CardHeader className="text-center">
            <CardTitle className="text-xl">无法连接服务端</CardTitle>
            <CardDescription>{error ?? "请确认 Argus 已启动"}</CardDescription>
          </CardHeader>
        </Card>
      </div>
    )
  }

  // 安装成功页
  if (result) {
    return (
      <div className="flex min-h-svh items-center justify-center bg-muted/40 p-4">
        <Card className="w-full max-w-md">
          <CardHeader className="space-y-1 text-center">
            <div className="mx-auto flex size-12 items-center justify-center rounded-lg bg-primary text-primary-foreground">
              <CheckCircle2 className="size-6" />
            </div>
            <CardTitle className="text-xl">安装完成</CardTitle>
            <CardDescription>
              管理员 <span className="font-medium text-foreground">{result.admin_username}</span> 已创建
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {setupOnly ? (
              <p className="rounded-md bg-muted p-3 text-sm text-muted-foreground">
                当前为安装模式，须重启进入完整模式后才能登录。
                {restartInfo && <span className="mt-1 block">{restartInfo}</span>}
              </p>
            ) : finishing ? (
              <p className="rounded-md bg-muted p-3 text-sm text-muted-foreground">正在自动登录…</p>
            ) : (
              restartInfo && (
                <p className="rounded-md bg-muted p-3 text-sm text-muted-foreground">{restartInfo}</p>
              )
            )}
            {error && <p className="text-sm text-destructive">{error}</p>}
            <div className="flex gap-2">
              {setupOnly && (
                <Button className="flex-1" onClick={doRestart} disabled={restarting}>
                  {restarting ? "重启中..." : "立即重启"}
                </Button>
              )}
              {setupOnly ? (
                <Button variant="outline" className="flex-1" onClick={() => navigate("/login", { replace: true })}>
                  去登录
                </Button>
              ) : authed ? (
                <Button className="flex-1" onClick={() => navigate("/dashboard", { replace: true })}>
                  进入系统
                </Button>
              ) : (
                <Button className="flex-1" onClick={() => navigate("/login", { replace: true })}>
                  去登录
                </Button>
              )}
            </div>
          </CardContent>
        </Card>
      </div>
    )
  }

  const steps = ["欢迎", "安全配置", "管理员"]
  const jwtFromEnv = status.jwt_from_env
  const masterFromEnv = status.master_from_env

  return (
    <div className="flex min-h-svh items-center justify-center bg-muted/40 p-4">
      <Card className="w-full max-w-lg">
        <CardHeader className="space-y-1 text-center">
          <div className="mx-auto flex size-12 items-center justify-center rounded-lg bg-primary text-primary-foreground">
            <ShieldCheck className="size-6" />
          </div>
          <CardTitle className="text-xl">欢迎使用 Argus</CardTitle>
          <CardDescription>
            永不眨眼的 SSL 证书守护者 · 首次使用请完成初始配置（{status.version}）
          </CardDescription>
          <div className="flex items-center justify-center gap-2 pt-2 text-xs text-muted-foreground">
            {steps.map((s, i) => (
              <span key={s} className={i === step ? "font-medium text-foreground" : ""}>
                {i + 1}. {s}{i < steps.length - 1 && " → "}
              </span>
            ))}
          </div>
        </CardHeader>
        <CardContent>
          {step === 0 && (
            <div className="space-y-3 text-sm">
              <p className="text-muted-foreground">
                向导将引导你完成安全密钥与初始管理员的配置，无需手动修改 config.yaml。
              </p>
              <ul className="list-disc space-y-1 pl-5 text-muted-foreground">
                <li>配置文件：{status.config_file ? "已检测到（将自动备份 .bak 后写回）" : "未检测到（将使用默认配置）"}</li>
                {status.running_as_service && <li>当前以 Windows 服务运行，改密级配置生效需 Restart-Service</li>}
                {jwtFromEnv && <li>JWT 密钥已由环境变量提供，无需填写</li>}
                {masterFromEnv && <li>主密钥已由环境变量提供，无需填写</li>}
              </ul>
              <Button className="w-full" onClick={() => setStep(1)}>开始配置</Button>
            </div>
          )}

          {step > 0 && (
            <form className="space-y-4" onSubmit={handleSubmit((v) => submit.mutate(v))}>
              {step === 1 && (
                <>
                  <div className="space-y-2">
                    <div className="flex items-center justify-between">
                      <Label>安全密钥</Label>
                      <Button type="button" variant="outline" size="sm" onClick={() => genKeys.mutate()} disabled={genKeys.isPending || (jwtFromEnv && masterFromEnv)}>
                        {genKeys.isPending ? "生成中..." : "一键生成"}
                      </Button>
                    </div>
                    {!jwtFromEnv ? (
                      <div className="space-y-2">
                        <Label htmlFor="jwtSecret">JWT 密钥（≥32 字符）</Label>
                        <Textarea id="jwtSecret" rows={2} placeholder="点“一键生成”或粘贴已有密钥" {...register("jwtSecret")} />
                      </div>
                    ) : (
                      <p className="text-xs text-muted-foreground">JWT 密钥：已由环境变量提供。</p>
                    )}
                    {!masterFromEnv ? (
                      <div className="space-y-2">
                        <Label htmlFor="masterKey">主密钥（≥32 字节，加密存储凭据）</Label>
                        <Textarea id="masterKey" rows={2} placeholder="点“一键生成”或粘贴已有密钥" {...register("masterKey")} />
                      </div>
                    ) : (
                      <p className="text-xs text-muted-foreground">主密钥：已由环境变量提供。</p>
                    )}
                    <p className="text-xs text-muted-foreground">密钥仅显示一次，请妥善保管；更换密钥后需重启服务。</p>
                  </div>
                  <div className="grid grid-cols-3 gap-2">
                    <div className="space-y-2">
                      <Label htmlFor="minPasswordLen">最小密码长度</Label>
                      <Input id="minPasswordLen" type="number" {...register("minPasswordLen", { valueAsNumber: true })} />
                      {errors.minPasswordLen && <p className="text-xs text-destructive">{errors.minPasswordLen.message}</p>}
                    </div>
                    <div className="space-y-2">
                      <Label htmlFor="maxLoginFailures">失败锁定阈值</Label>
                      <Input id="maxLoginFailures" type="number" {...register("maxLoginFailures", { valueAsNumber: true })} />
                      {errors.maxLoginFailures && <p className="text-xs text-destructive">{errors.maxLoginFailures.message}</p>}
                    </div>
                    <div className="space-y-2">
                      <Label htmlFor="lockoutMinutes">锁定分钟数</Label>
                      <Input id="lockoutMinutes" type="number" {...register("lockoutMinutes", { valueAsNumber: true })} />
                      {errors.lockoutMinutes && <p className="text-xs text-destructive">{errors.lockoutMinutes.message}</p>}
                    </div>
                  </div>
                </>
              )}

              {step === 2 && (
                <>
                  <div className="space-y-2">
                    <Label htmlFor="adminUsername">管理员用户名</Label>
                    <Input id="adminUsername" autoComplete="username" {...register("adminUsername")} />
                    {errors.adminUsername && <p className="text-sm text-destructive">{errors.adminUsername.message}</p>}
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="adminPassword">管理员密码</Label>
                    <Input id="adminPassword" type="password" autoComplete="new-password" placeholder={`至少 ${minLen} 位，含大小写字母与数字`} {...register("adminPassword")} />
                    {errors.adminPassword && <p className="text-sm text-destructive">{errors.adminPassword.message}</p>}
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="confirmPassword">确认密码</Label>
                    <Input id="confirmPassword" type="password" autoComplete="new-password" {...register("confirmPassword")} />
                    {errors.confirmPassword && <p className="text-sm text-destructive">{errors.confirmPassword.message}</p>}
                  </div>
                </>
              )}

              {error && <p className="text-sm text-destructive">{error}</p>}
              <div className="flex gap-2">
                <Button type="button" variant="outline" className="flex-1" onClick={() => { setError(null); setStep(step - 1) }}>
                  上一步
                </Button>
                {step === 1 && (
                  <Button type="button" className="flex-1" onClick={() => setStep(2)}>下一步</Button>
                )}
                {step === 2 && (
                  <Button type="submit" className="flex-1" disabled={submit.isPending}>
                    {submit.isPending ? "提交中..." : "完成安装"}
                  </Button>
                )}
              </div>
            </form>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
