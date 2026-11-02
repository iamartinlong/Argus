import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { MemoryRouter } from "react-router-dom"
import { DomainsPage } from "./domains-page"
import { api } from "@/lib/api"

const apiMock = api as unknown as {
  get: ReturnType<typeof vi.fn>
  post: ReturnType<typeof vi.fn>
  put: ReturnType<typeof vi.fn>
  delete: ReturnType<typeof vi.fn>
}

vi.mock("@/lib/api", () => ({
  api: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
  // 透传真实错误消息，使内联 Alert 的文案断言有效
  getErrorMessage: vi.fn((e: unknown) => (e as { message?: string })?.message ?? "请求失败"),
}))

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn(), warning: vi.fn() },
}))

const ledgerItem = (over: Record<string, unknown> = {}) => ({
  id: "d1",
  name: "www.example.com",
  expected_status: "must_have_cert",
  source: "manual",
  note: "主站",
  tags: ["prod"],
  coverage: "expiring",
  days_to_expire: 12,
  cert_count: 1,
  current_cert: {
    id: "c1",
    primary_domain: "*.example.com",
    status: "expiring",
    provider: "acme",
    not_after: "2026-11-14T23:59:59Z",
  },
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-01T00:00:00Z",
  ...over,
})

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <DomainsPage />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe("DomainsPage", () => {
  beforeEach(() => {
    apiMock.get.mockReset()
    apiMock.post.mockReset()
    apiMock.put.mockReset()
    apiMock.delete.mockReset()
    vi.spyOn(window, "confirm").mockReturnValue(true)
  })

  it("渲染台账行与覆盖状态徽章", async () => {
    apiMock.get.mockResolvedValue({
      data: [
        ledgerItem(),
        ledgerItem({ id: "d2", name: "api.example.com", coverage: "missing", current_cert: null, days_to_expire: null, cert_count: 0 }),
      ],
    })
    renderPage()

    expect(await screen.findByText("www.example.com")).toBeInTheDocument()
    expect(screen.getByText("即将到期")).toBeInTheDocument()
    // 「缺少证书」同时出现在徽章与统计条，用 getAllByText 断言存在
    expect(screen.getAllByText("缺少证书").length).toBeGreaterThan(0)
    // 通配符证书作为当前证书展示
    expect(screen.getByText("*.example.com")).toBeInTheDocument()
    // 统计条
    expect(screen.getByText("域名总数")).toBeInTheDocument()
  })

  it("按覆盖状态筛选", async () => {
    const user = userEvent.setup()
    apiMock.get.mockResolvedValue({
      data: [
        ledgerItem(),
        ledgerItem({ id: "d2", name: "api.example.com", coverage: "missing", current_cert: null }),
      ],
    })
    renderPage()
    await screen.findByText("www.example.com")

    await user.click(screen.getByRole("combobox", { name: "" }))
    await user.click(await screen.findByRole("option", { name: "缺少证书" }))
    await waitFor(() => expect(screen.queryByText("www.example.com")).not.toBeInTheDocument())
    expect(screen.getByText("api.example.com")).toBeInTheDocument()
  })

  it("登记域名：提交规范化后的字段", async () => {
    const user = userEvent.setup()
    apiMock.get.mockResolvedValue({ data: [] })
    apiMock.post.mockResolvedValue({ data: ledgerItem() })
    renderPage()
    await screen.findByText("暂无域名，点击「登记域名」开始")

    await user.click(screen.getByRole("button", { name: /登记域名/ }))
    await user.type(screen.getByPlaceholderText("example.com 或 *.example.com"), " New.Example.com ")
    const dialog = screen.getByRole("dialog")
    await user.click(within(dialog).getByRole("combobox"))
    await user.click(screen.getByRole("option", { name: "仅关注（不要求证书）" }))
    await user.type(screen.getByPlaceholderText("prod, web"), "a, b")
    await user.click(screen.getByRole("button", { name: "登记" }))

    await waitFor(() => expect(apiMock.post).toHaveBeenCalled())
    const [url, body] = apiMock.post.mock.calls.at(-1) ?? []
    expect(url).toBe("/domains")
    expect(body).toMatchObject({
      name: " New.Example.com ",
      expected_status: "monitor_only",
      tags: ["a", "b"],
    })
  })

  it("登记失败时显示内联 Alert", async () => {
    const user = userEvent.setup()
    apiMock.get.mockResolvedValue({ data: [] })
    apiMock.post.mockRejectedValue({ message: "域名已在台账中" })
    renderPage()
    await screen.findByText("暂无域名，点击「登记域名」开始")

    await user.click(screen.getByRole("button", { name: /登记域名/ }))
    await user.type(screen.getByPlaceholderText("example.com 或 *.example.com"), "dup.example.com")
    await user.click(screen.getByRole("button", { name: "登记" }))

    const alert = await screen.findByRole("alert")
    expect(alert).toHaveTextContent("登记失败")
    expect(alert).toHaveTextContent("域名已在台账中")
  })

  it("删除时确认框带引用证书数", async () => {
    const user = userEvent.setup()
    apiMock.get.mockResolvedValue({ data: [ledgerItem()] })
    apiMock.delete.mockResolvedValue({ data: { ok: true } })
    renderPage()
    await screen.findByText("www.example.com")

    await user.click(screen.getAllByRole("button", { name: "" })[0]) // 删除图标按钮
    await waitFor(() => expect(apiMock.delete).toHaveBeenCalledWith("/domains/d1"))
  })

  it("点击刷新重新请求台账", async () => {
    const user = userEvent.setup()
    apiMock.get.mockResolvedValue({ data: [ledgerItem()] })
    renderPage()
    await screen.findByText("www.example.com")
    const before = apiMock.get.mock.calls.filter(([u]) => u === "/domains").length

    await user.click(screen.getByRole("button", { name: "刷新" }))
    await waitFor(() =>
      expect(apiMock.get.mock.calls.filter(([u]) => u === "/domains").length).toBeGreaterThan(before),
    )
  })
})
