import { useEffect, useState } from "react"
import { Navigate, useNavigate } from "react-router-dom"
import { fetchSetupStatus } from "@/features/setup/setup-api"
import { useMutation } from "@tanstack/react-query"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { ShieldCheck } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { api, getErrorMessage } from "@/lib/api"
import { useAuthStore } from "@/stores/auth-store"

const REMEMBER_KEY = "argus-remembered-username"

const loginSchema = z.object({
  username: z.string().min(1, "请输入用户名"),
  password: z.string().min(1, "请输入密码"),
})

type LoginForm = z.infer<typeof loginSchema>

export function LoginPage() {
  const navigate = useNavigate()
  const setAuth = useAuthStore((s) => s.setAuth)
  const accessToken = useAuthStore((s) => s.accessToken)
  const mustChangePassword = useAuthStore((s) => s.mustChangePassword)
  const [error, setError] = useState<string | null>(null)
  const [rememberUsername, setRememberUsername] = useState(
    () => localStorage.getItem(REMEMBER_KEY) !== null,
  )

  // 首次使用（未初始化）直接进安装向导
  useEffect(() => {
    fetchSetupStatus()
      .then((s) => {
        if (!s.initialized) navigate("/setup", { replace: true })
      })
      .catch(() => {})
  }, [navigate])

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<LoginForm>({
    resolver: zodResolver(loginSchema),
    defaultValues: {
      username: localStorage.getItem(REMEMBER_KEY) ?? "",
    },
  })

  const login = useMutation({
    mutationFn: async (values: LoginForm) => {
      const res = await api.post<{
        access_token: string
        must_change_password?: boolean
        user: { id: string; username: string; role: string }
      }>("/auth/login", values)
      return res.data
    },
    onSuccess: (data) => {
      setAuth(data.access_token, data.user, data.must_change_password)
      navigate(data.must_change_password ? "/change-password" : "/dashboard", { replace: true })
    },
    onError: (err) => {
      // 保留登录场景的兜底文案，同时透出后端 429/403 等具体提示
      setError(getErrorMessage(err, "用户名或密码错误"))
    },
  })

  // 已登录用户再访登录页：直接跳转，避免重复登录
  if (accessToken) {
    return <Navigate to={mustChangePassword ? "/change-password" : "/dashboard"} replace />
  }

  return (
    <div className="flex min-h-svh items-center justify-center bg-muted/40 p-4">
      <Card className="w-full max-w-sm">
        <CardHeader className="space-y-1 text-center">
          <div className="mx-auto flex size-12 items-center justify-center rounded-lg bg-primary text-primary-foreground">
            <ShieldCheck className="size-6" />
          </div>
          <CardTitle className="text-xl">Argus</CardTitle>
          <CardDescription>永不眨眼的 SSL 证书守护者</CardDescription>
        </CardHeader>
        <CardContent>
          <form
            className="space-y-4"
            onSubmit={handleSubmit((values) => {
              if (rememberUsername) {
                localStorage.setItem(REMEMBER_KEY, values.username)
              } else {
                localStorage.removeItem(REMEMBER_KEY)
              }
              login.mutate(values)
            })}
          >
            <div className="space-y-2">
              <Label htmlFor="username">用户名</Label>
              <Input
                id="username"
                placeholder="admin"
                autoComplete="username"
                {...register("username")}
              />
              {errors.username && (
                <p className="text-sm text-destructive">{errors.username.message}</p>
              )}
            </div>
            <div className="space-y-2">
              <Label htmlFor="password">密码</Label>
              <Input
                id="password"
                type="password"
                autoComplete="current-password"
                {...register("password")}
              />
              {errors.password && (
                <p className="text-sm text-destructive">{errors.password.message}</p>
              )}
            </div>
            <label className="flex items-center gap-2 text-sm">
              <Checkbox
                checked={rememberUsername}
                onCheckedChange={(checked) => setRememberUsername(checked)}
              />
              记住用户名
            </label>
            {error && <p className="text-sm text-destructive">{error}</p>}
            <Button type="submit" className="w-full" disabled={login.isPending}>
              {login.isPending ? "登录中..." : "登录"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
