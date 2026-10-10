import { useState } from "react"
import { Sun, Moon, Monitor } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { getTheme, setTheme, type Theme } from "@/lib/theme"

const themeNames: Record<Theme, string> = {
  light: "亮色",
  dark: "暗色",
  system: "跟随系统",
}

export function ThemeToggle() {
  const [theme, setCurrent] = useState<Theme>(getTheme())

  const change = (t: Theme) => {
    setCurrent(t)
    setTheme(t)
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button variant="ghost" size="icon" className="size-8">
            {theme === "dark" ? (
              <Moon className="size-4" />
            ) : theme === "light" ? (
              <Sun className="size-4" />
            ) : (
              <Monitor className="size-4" />
            )}
          </Button>
        }
      />
      <DropdownMenuContent align="end" className="w-40">
        <DropdownMenuGroup>
          <DropdownMenuLabel>主题</DropdownMenuLabel>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        {(["light", "dark", "system"] as Theme[]).map((t) => (
          <DropdownMenuItem key={t} onClick={() => change(t)}>
            {t === "light" ? (
              <Sun className="mr-2 size-4" />
            ) : t === "dark" ? (
              <Moon className="mr-2 size-4" />
            ) : (
              <Monitor className="mr-2 size-4" />
            )}
            {themeNames[t]}
            {theme === t && <span className="ml-auto text-muted-foreground">✓</span>}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
