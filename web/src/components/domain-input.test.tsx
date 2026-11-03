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
function renderControlled(initial = "", single = false) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const changes: string[] = []
  function Harness() {
    const [value, setValue] = React.useState(initial)
    return (
      <QueryClientProvider client={queryClient}>
        <DomainInput
          value={value}
          single={single}
          onChange={(v) => {
            changes.push(v)
            setValue(v)
          }}
        />
      </QueryClientProvider>
    )
  }
  const { container } = render(<Harness />)
  return { changes, container }
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

  // 回归：面板内输入过滤词后点击选项——过滤词不得覆盖选中项（曾因面板关闭时
  // 误把过滤词当域名提交，导致选中项丢失且插入垃圾 chip）
  it("面板内输入过滤词后选中：只录入选中项，过滤词不落 chip", async () => {
    const user = userEvent.setup()
    const { changes } = renderControlled()

    await user.click(screen.getByRole("button", { name: "从域名台账选择" }))
    await user.type(screen.getByRole("combobox"), "api")
    await user.click(await screen.findByText("api.example.com"))

    await waitFor(() => expect(changes.at(-1)).toBe("api.example.com"))
    // 过滤词 "api" 没有成为 chip
    expect(screen.queryByRole("button", { name: "移除 api" })).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "移除 api.example.com" })).toBeInTheDocument()
  })

  // 回归：Esc 关闭面板时过滤词同样被丢弃
  it("Esc 关闭面板丢弃过滤词", async () => {
    const user = userEvent.setup()
    const { changes } = renderControlled()

    await user.click(screen.getByRole("button", { name: "从域名台账选择" }))
    await user.type(screen.getByRole("combobox"), "api")
    await user.keyboard("{Escape}")

    await waitFor(() => expect(screen.queryByText("www.example.com")).not.toBeInTheDocument())
    expect(changes).toEqual([])
  })

  // 再次点击按钮可关闭面板（防止 trigger 与内部开关双触发导致面板重开）
  it("再次点击选择按钮关闭面板", async () => {
    const user = userEvent.setup()
    const { container } = renderControlled()

    await user.click(screen.getByRole("button", { name: "从域名台账选择" }))
    expect(await screen.findByText("www.example.com")).toBeInTheDocument()

    // 面板打开后触发器被 aria-hidden（base-ui 对可输入 combobox 的既定行为），
    // 经 DOM 查询点击验证"再点一次可关闭"
    const trigger = container.querySelector('button[aria-label="从域名台账选择"]')
    expect(trigger).not.toBeNull()
    await user.click(trigger as Element)
    await waitFor(() => expect(screen.queryByText("api.example.com")).not.toBeInTheDocument())
  })

  it("一键清空全部域名", async () => {
    const user = userEvent.setup()
    const { changes } = renderControlled("a.com, b.com")

    await user.click(screen.getByRole("button", { name: "清空全部域名" }))
    expect(changes.at(-1)).toBe("")
  })

  it("chip 可单个移除", async () => {    const user = userEvent.setup()
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

describe("DomainInput 单域名模式（IIS SNI 主机名）", () => {
  beforeEach(() => {
    apiMock.get.mockReset()
    apiMock.get.mockImplementation(async (url: string) => {
      if (url === "/domains") return { data: ledgerFixture }
      return { data: [] }
    })
  })

  it("输入框直接展示当前值，不渲染 chip", async () => {
    renderControlled("www.example.com", true)
    const input = screen.getByRole("combobox") as HTMLInputElement
    expect(input.value).toBe("www.example.com")
    expect(screen.queryByText("www.example.com")).not.toBeInTheDocument()
  })

  it("输入回车替换为单个域名（取第一个 token）", async () => {
    const user = userEvent.setup()
    const { changes } = renderControlled("", true)

    await user.type(screen.getByRole("combobox"), "SNI.Example.com, other.com")
    await user.keyboard("{Enter}")

    expect(changes.at(-1)).toBe("sni.example.com")
    expect((screen.getByRole("combobox") as HTMLInputElement).value).toBe("sni.example.com")
  })

  it("从面板选择即替换（而非累加）", async () => {
    const user = userEvent.setup()
    const { changes } = renderControlled("old.example.com", true)

    await user.click(screen.getByRole("button", { name: "从域名台账选择" }))
    await user.click(await screen.findByText("api.example.com"))

    await waitFor(() => expect(changes.at(-1)).toBe("api.example.com"))
    expect(changes.filter((c) => c.includes("old.example.com"))).toEqual([])
  })

  // 回归：单选后失焦，输入框文字曾消失（commitInput 清空了输入框，但值未变、
  // effect 不触发，导致只剩清除图标）
  it("选中后失焦：输入框仍显示该域名", async () => {
    const user = userEvent.setup()
    const { changes } = renderControlled("", true)

    await user.click(screen.getByRole("button", { name: "从域名台账选择" }))
    await user.click(await screen.findByText("www.example.com"))
    await waitFor(() => expect(changes.at(-1)).toBe("www.example.com"))

    // 失焦（点页面空白处）
    await user.click(document.body)
    await waitFor(() =>
      expect((screen.getByRole("combobox") as HTMLInputElement).value).toBe("www.example.com"),
    )
    // 失焦不应产生额外变更
    expect(changes).toEqual(["www.example.com"])
  })

  it("失焦时提交已输入但未回车的域名", async () => {
    const user = userEvent.setup()
    const { changes } = renderControlled("", true)

    await user.type(screen.getByRole("combobox"), "Blur.Example.com")
    await user.click(document.body)

    await waitFor(() => expect(changes.at(-1)).toBe("blur.example.com"))
    expect((screen.getByRole("combobox") as HTMLInputElement).value).toBe("blur.example.com")
  })

  it("失焦时清空输入框视为清空值", async () => {
    const user = userEvent.setup()
    const { changes } = renderControlled("keep.example.com", true)

    await user.clear(screen.getByRole("combobox"))
    await user.click(document.body)

    await waitFor(() => expect(changes.at(-1)).toBe(""))
    expect((screen.getByRole("combobox") as HTMLInputElement).value).toBe("")
  })

  it("面板内输入过滤词后选中：过滤词不顶掉已选主机名", async () => {
    const user = userEvent.setup()
    const { changes } = renderControlled("keep.example.com", true)

    await user.click(screen.getByRole("button", { name: "从域名台账选择" }))
    // 单域名模式输入框预填当前值，先清空再输过滤词
    await user.clear(screen.getByRole("combobox"))
    await user.type(screen.getByRole("combobox"), "api")
    await user.click(await screen.findByText("api.example.com"))

    await waitFor(() => expect(changes.at(-1)).toBe("api.example.com"))
    expect((screen.getByRole("combobox") as HTMLInputElement).value).toBe("api.example.com")
  })

  it("Esc 关闭面板后输入框恢复显示已选主机名", async () => {
    const user = userEvent.setup()
    const { changes } = renderControlled("keep.example.com", true)

    await user.click(screen.getByRole("button", { name: "从域名台账选择" }))
    await user.clear(screen.getByRole("combobox"))
    await user.type(screen.getByRole("combobox"), "api")
    await user.keyboard("{Escape}")

    await waitFor(() => expect(screen.queryByText("www.example.com")).not.toBeInTheDocument())
    expect((screen.getByRole("combobox") as HTMLInputElement).value).toBe("keep.example.com")
    expect(changes).toEqual([])
  })

  it("清空按钮与 Backspace 均可清空", async () => {
    const user = userEvent.setup()
    const { changes } = renderControlled("keep.example.com", true)

    await user.click(screen.getByRole("button", { name: "清空全部域名" }))
    expect(changes.at(-1)).toBe("")

    await user.click(screen.getByRole("combobox"))
    await user.keyboard("{Backspace}")
    expect(changes.at(-1)).toBe("")
  })
})
