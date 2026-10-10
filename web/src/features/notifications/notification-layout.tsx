import { NavLink, Outlet, useLocation } from "react-router-dom"
import { cn } from "@/lib/utils"

const tabs = [
  { label: "渠道", to: "/notification/channels" },
  { label: "规则", to: "/notification/rules" },
  { label: "历史", to: "/notification/histories" },
]

export function NotificationLayout() {
  const location = useLocation()
  return (
    <div className="flex flex-col gap-4">
      <div className="inline-flex h-9 items-center justify-start rounded-lg bg-muted p-1 text-muted-foreground w-fit">
        {tabs.map((t) => {
          const active = location.pathname.startsWith(t.to)
          return (
            <NavLink
              key={t.to}
              to={t.to}
              className={cn(
                "inline-flex items-center justify-center whitespace-nowrap rounded-md px-3 py-1 text-sm font-medium ring-offset-background transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
                active && "bg-background text-foreground shadow",
              )}
            >
              {t.label}
            </NavLink>
          )
        })}
      </div>
      <Outlet />
    </div>
  )
}
