// vite build 的 emptyOutDir 会清空 webassets/dist，连同 go:embed 占位文件 .gitkeep 一起删除，
// 导致未构建前端时 go build/vet/test 报 "pattern all:dist: no matching files found"。
// 本脚本在前端构建后恢复占位文件（内容与 git 跟踪版本逐字节一致，避免状态漂移；
// 占位内容刻意保持 ASCII，杜绝跨平台编码问题）。
import { mkdirSync, writeFileSync } from "node:fs"
import { dirname, join } from "node:path"
import { fileURLToPath } from "node:url"

const PLACEHOLDER = "# placeholder for go:embed all:dist (restored by web/scripts/restore-embed-placeholder.mjs)\n"

const dist = join(dirname(fileURLToPath(import.meta.url)), "..", "..", "webassets", "dist")
mkdirSync(dist, { recursive: true })
writeFileSync(join(dist, ".gitkeep"), PLACEHOLDER)
