import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import * as React from "react"
import { DomainInput, parseDomainTokens } from "./domain-input"
import { api } from "@/lib/api"

const apiMock = api as unknown as { get: ReturnType<typeof vi.fn> }

vi.mock("@/lib/api", () => ({
  api: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
  getErrorMessage: vi.fn(() => "请求失败"),
}))

const ledgerFixture = [
  { id: "d1", name: "www.example.com", expected_status: "must_have_cert", source: "manual", note: "", tags: [], coverage: "missing", days_to_expire: null, cert_count: 0, current_cert: null, created_at: "", updated_at: "" },
  { id: "d2", name: "api.example.com", expected_status: "must_have_cert", source: "manual", note: "", tags: [], coverage: "missing", days_to_expire: null, cert_count: 0, current_cert: null, created_at: "", updated_at: "" },
]

/** 有状态 harness：模拟父表单，验证 chip 渲染与回传 */
function renderControlled(initial = "") {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const changes: string[] = []
  function Harness() {
    const [value, setValue] = React.useState(initial)
    return (
      <QueryClientProvider client={queryClient}>
        <DomainInput
          value={value}
          onChange={(v) => {
            changes.push(v)
            setValue(v)
          }}
        />
      </QueryClientProvider>
    )
  }
  render(<Harness />)
  return { changes }
}

describe("parseDomainTokens", () => {
  it("按逗号/空格分隔并规范化去重", () => {
    expect(parseDomainTokens("A.com, b.com  A.COM c.com.")).toEqual(["a.com", "b.com", "c.com"])
    expect(parseDomainTokens("")).toEqual([])
    expect(parseDomainTokens("  ,  ")).toEqual([])
  })
})

describe("DomainInput", () => {
  beforeEach(() => {
    apiMock.get.mockReset()
    apiMock.get.mockImplementation(async (url: string) => {
      if (url === "/domains") return { data: ledgerFixture }
      return { data: [] }
    })
  })

  it("输入后回车落为 chip（规范化）", async () => {
    const user = userEvent.setup()
    const { changes } = renderControlled()

    await user.type(screen.getByPlaceholderText("example.com, www.example.com"), "WWW.Example.com")
    await user.keyboard("{Enter}")

    expect(await screen.findByText("www.example.com")).toBeInTheDocument()
    expect(changes.at(-1)).toBe("www.example.com")
  })

  it("逗号分隔一次输入多个，重复自动去重", async () => {
    const user = userEvent.setup()
    const { changes } = renderControlled()

    await user.type(screen.getByPlaceholderText("example.com, www.example.com"), "a.com, b.com, a.com")
    await user.keyboard("{Enter}")

    expect(changes.at(-1)).toBe("a.com, b.com")
    expect(await screen.findByText("a.com")).toBeInTheDocument()
    expect(screen.getByText("b.com")).toBeInTheDocument()
  })

  it("点右侧按钮弹出台账面板，勾选后合并为 chip", async () => {
    const user = userEvent.setup()
    const { changes } = renderControlled()

    await user.click(screen.getByRole("button", { name: "从域名台账选择" }))
    // 面板列出台账域名
    const option = await screen.findByText("api.example.com")
    await user.click(option)

    await waitFor(() => expect(changes.at(-1)).toBe("api.example.com"))
    // chip 已生成（以"移除"按钮为精确锚点，避免与弹层内同名项冲突）
    expect(await screen.findByRole("button", { name: "移除 api.example.com" })).toBeInTheDocument()
  })

  it("chip 可单个移除", async () => {
    const user = userEvent.setup()
    const { changes } = renderControlled("a.com, b.com")

    await user.click(screen.getByRole("button", { name: "移除 a.com" }))
    expect(changes.at(-1)).toBe("b.com")
    expect(screen.queryByText("a.com")).not.toBeInTheDocument()
  })

  it("空输入框按 Backspace 删除最后一个 chip", async () => {
    const user = userEvent.setup()
    const { changes } = renderControlled("a.com, b.com")

    await user.click(screen.getByRole("combobox"))
    await user.keyboard("{Backspace}")

    expect(changes.at(-1)).toBe("a.com")
  })
})
