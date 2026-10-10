export type Theme = "light" | "dark" | "system"

const STORAGE_KEY = "argus-theme"

export function getTheme(): Theme {
  const saved = localStorage.getItem(STORAGE_KEY)
  if (saved === "light" || saved === "dark" || saved === "system") {
    return saved
  }
  return "system"
}

export function setTheme(theme: Theme) {
  localStorage.setItem(STORAGE_KEY, theme)
  applyTheme(theme)
}

export function applyTheme(theme: Theme) {
  const dark =
    theme === "dark" ||
    (theme === "system" &&
      window.matchMedia("(prefers-color-scheme: dark)").matches)
  document.documentElement.classList.toggle("dark", dark)
}

export function initTheme() {
  applyTheme(getTheme())
  // 跟随系统时监听变化
  window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", (e) => {
    if (getTheme() === "system") {
      document.documentElement.classList.toggle("dark", e.matches)
    }
  })
}
