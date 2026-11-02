import { describe, it, expect, vi, beforeEach } from "vitest"
import { act, render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { MemoryRouter } from "react-router-dom"
import { CertificatesPage } from "./certificates-page"
import { api } from "@/lib/api"
import { toast } from "sonner"

const apiMock = api as unknown as {
  get: ReturnType<typeof vi.fn>
  post: ReturnType<typeof vi.fn>
}

const toastMock = toast as unknown as { error: ReturnType<typeof vi.fn> }

vi.mock("@/lib/api", () => ({
  api: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
  getErrorMessage: vi.fn(() => "请求失败"),
}))

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn(), warning: vi.fn() },
}))

const certFixture = (id: string, domain: string, provider: string) => ({
  id,
  primary_domain: domain,
  domains: [domain],
  provider,
  status: "valid",
  issuer_ca: "",
  not_before: "2026-01-01T00:00:00Z",
  not_after: "2027-01-01T00:00:00Z",
  verification: "dns01",
  deploy_target_ids: [],
  last_error: "",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  days_to_expire: 30,
})

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <CertificatesPage />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe("批量续期排除导入证书", () => {
  beforeEach(() => {
    apiMock.get.mockReset()
    apiMock.post.mockReset()
    toastMock.error.mockReset()
    vi.spyOn(window, "confirm").mockReturnValue(true)
  })

  it("混合选择时只提交非导入证书 ID", async () => {
    const user = userEvent.setup()
    apiMock.get.mockImplementation(async (url: string) => {
      if (url === "/certificates") {
        return { data: [certFixture("acme-1", "a.example.com", "acme"), certFixture("imp-1", "b.example.com", "import")] }
      }
      if (url === "/deploy-targets") return { data: [{ id: "t1", name: "T", type: "local" }] }
      return { data: [] }
    })
    apiMock.post.mockResolvedValue({ data: { success: 1, failed: [] } })

    renderPage()
    await screen.findByText("a.example.com")

    const boxes = screen.getAllByRole("checkbox")
    await user.click(boxes[1]) // acme 证书
    await user.click(boxes[2]) // import 证书

    await user.click(await screen.findByRole("button", { name: /批量续期/ }))
    // 批量续期走确认弹窗，实际提交发生在确认之后
    await user.click(await screen.findByRole("button", { name: /确认续期/ }))
    await waitFor(() =>
      expect(apiMock.post).toHaveBeenCalledWith("/certificates/batch-renew", { ids: ["acme-1"] }),
    )
  })

  it("只选中导入证书时提示错误且不调用批量续期接口", async () => {
    const user = userEvent.setup()
    apiMock.get.mockImplementation(async (url: string) => {
      if (url === "/certificates") {
        return { data: [certFixture("imp-1", "b.example.com", "import"), certFixture("imp-2", "c.example.com", "import")] }
      }
      if (url === "/deploy-targets") return { data: [] }
      return { data: [] }
    })

    renderPage()
    await screen.findByText("b.example.com")

    const boxes = screen.getAllByRole("checkbox")
    await user.click(boxes[1]) // 仅选中导入证书

    await user.click(await screen.findByRole("button", { name: /批量续期/ }))
    expect(toastMock.error).toHaveBeenCalledWith("所选证书均为导入证书，无法续期")
    expect(apiMock.post).not.toHaveBeenCalled()
  })

  it("点击刷新按钮重新请求证书列表", async () => {
    const user = userEvent.setup()
    apiMock.get.mockImplementation(async (url: string) => {
      if (url === "/certificates") return { data: [certFixture("c-1", "a.example.com", "acme")] }
      if (url === "/deploy-targets") return { data: [] }
      return { data: [] }
    })

    renderPage()
    await screen.findByText("a.example.com")
    const before = apiMock.get.mock.calls.filter(([u]) => u === "/certificates").length
    expect(before).toBeGreaterThan(0)

    await user.click(screen.getByRole("button", { name: "刷新" }))
    await waitFor(() =>
      expect(apiMock.get.mock.calls.filter(([u]) => u === "/certificates").length).toBeGreaterThan(before),
    )
  })

  it("手动刷新时表格显示「刷新中」遮罩，完成后消失", async () => {
    const user = userEvent.setup()
    let certCalls = 0
    let releaseSecond: () => void = () => {}
    apiMock.get.mockImplementation((url: string) => {
      if (url !== "/certificates") return Promise.resolve({ data: [] })
      certCalls += 1
      const payload = { data: [certFixture("c-1", "a.example.com", "acme")] }
      if (certCalls === 1) return Promise.resolve(payload)
      // 第二次（手动刷新）挂起，用于观察遮罩态
      return new Promise((resolve) => {
        releaseSecond = () => resolve(payload)
      })
    })

    renderPage()
    await screen.findByText("a.example.com")
    expect(screen.queryByRole("status")).not.toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "刷新" }))
    expect(await screen.findByRole("status")).toHaveTextContent("刷新中")

    await act(async () => {
      releaseSecond()
      await Promise.resolve()
    })
    await waitFor(() => expect(screen.queryByRole("status")).not.toBeInTheDocument())
  })
})
