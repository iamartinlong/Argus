import { useState } from "react"
import { useNavigate } from "react-router-dom"
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
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { api, getErrorMessage } from "@/lib/api"
import { useAuthStore } from "@/stores/auth-store"

const passwordSchema = z
  .object({
    oldPassword: z.string().min(1, "请输入当前密码"),
    newPassword: z
      .string()
      .min(12, "密码长度至少 12 位")
      .regex(/[a-z]/, "需包含小写字母")
      .regex(/[A-Z]/, "需包含大写字母")
      .regex(/[0-9]/, "需包含数字"),
    confirm: z.string(),
  })
  .refine((v) => v.newPassword === v.confirm, {
    message: "两次输入的密码不一致",
    path: ["confirm"],
  })

type PasswordForm = z.infer<typeof passwordSchema>

export function ChangePasswordPage() {
  const navigate = useNavigate()
  const clearAuth = useAuthStore((s) => s.clearAuth)
  const setMustChangePassword = useAuthStore((s) => s.setMustChangePassword)
  const [error, setError] = useState<string | null>(null)

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<PasswordForm>({
    resolver: zodResolver(passwordSchema),
  })

  const change = useMutation({
    mutationFn: async (values: PasswordForm) => {
      await api.put("/users/me/password", {
        old_password: values.oldPassword,
        new_password: values.newPassword,
      })
    },
    onSuccess: () => {
      setMustChangePassword(false)
      navigate("/dashboard", { replace: true })
    },
    onError: (err) => {
      setError(getErrorMessage(err))
    },
  })

  return (
    <div className="flex min-h-svh items-center justify-center bg-muted/40 p-4">
      <Card className="w-full max-w-sm">
        <CardHeader className="space-y-1 text-center">
          <div className="mx-auto flex size-12 items-center justify-center rounded-lg bg-primary text-primary-foreground">
            <ShieldCheck className="size-6" />
          </div>
          <CardTitle className="text-xl">修改初始密码</CardTitle>
          <CardDescription>出于安全考虑，首次登录需修改初始密码</CardDescription>
        </CardHeader>
        <CardContent>
          <form
            className="space-y-4"
            onSubmit={handleSubmit((values) => change.mutate(values))}
          >
            <div className="space-y-2">
              <Label htmlFor="oldPassword">当前密码</Label>
              <Input
                id="oldPassword"
                type="password"
                autoComplete="current-password"
                {...register("oldPassword")}
              />
              {errors.oldPassword && (
                <p className="text-sm text-destructive">{errors.oldPassword.message}</p>
              )}
            </div>
            <div className="space-y-2">
              <Label htmlFor="newPassword">新密码</Label>
              <Input
                id="newPassword"
                type="password"
                autoComplete="new-password"
                placeholder="至少 12 位，含大小写字母与数字"
                {...register("newPassword")}
              />
              {errors.newPassword && (
                <p className="text-sm text-destructive">{errors.newPassword.message}</p>
              )}
            </div>
            <div className="space-y-2">
              <Label htmlFor="confirm">确认新密码</Label>
              <Input
                id="confirm"
                type="password"
                autoComplete="new-password"
                {...register("confirm")}
              />
              {errors.confirm && (
                <p className="text-sm text-destructive">{errors.confirm.message}</p>
              )}
            </div>
            {error && <p className="text-sm text-destructive">{error}</p>}
            <Button type="submit" className="w-full" disabled={change.isPending}>
              {change.isPending ? "提交中..." : "确认修改"}
            </Button>
            <Button
              type="button"
              variant="outline"
              className="w-full"
              onClick={() => {
                clearAuth()
                navigate("/login", { replace: true })
              }}
            >
              退出登录
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
