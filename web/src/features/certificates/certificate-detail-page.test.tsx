import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { CertificateDetailPage } from "./certificate-detail-page"
import { api } from "@/lib/api"
import type { Certificate } from "@/lib/types"

const apiMock = api as unknown as {
  get: ReturnType<typeof vi.fn>
  post: ReturnType<typeof vi.fn>
  put: ReturnType<typeof vi.fn>
}

vi.mock("@/lib/api", () => ({
  api: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
  getErrorMessage: vi.fn(() => "请求失败"),
  getErrorMessageAsync: vi.fn(async () => "请求失败"),
}))

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn(), warning: vi.fn() },
}))

const certFactory = (overrides: Partial<Certificate> = {}): Certificate => ({
  id: "cert-1",
  domains: ["detail.example.com"],
  primary_domain: "detail.example.com",
  provider: "acme",
  status: "valid",
  issuer_ca: "",
  not_before: "2026-01-01T00:00:00Z",
  not_after: "2027-01-01T00:00:00Z",
  verification: "dns01",
  deploy_target_ids: ["t1"],
  notify_events: ["cert_expiring", "renew_success"],
  last_error: "",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  days_to_expire: 20,
  ...overrides,
})

function mockCert(cert: Certificate) {
  apiMock.get.mockImplementation(async (url: string) => {
    if (url === "/certificates/cert-1") return { data: cert }
    if (url === "/certificates/cert-1/orders") return { data: [] }
    if (url === "/certificates/cert-1/deploy-records") return { data: [] }
    if (url === "/deploy-targets") return { data: [{ id: "t1", name: "目标1" }] }
    return { data: [] }
  })
}

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={["/certificates/cert-1"]}>
        <Routes>
          <Route path="/certificates/:id" element={<CertificateDetailPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

async function waitLoaded() {
  // 域名同时出现在标题与 SAN 区，用 findAllByText 等待加载完成
  await screen.findAllByText("detail.example.com")
}

describe("CertificateDetailPage 操作按钮守卫", () => {
  beforeEach(() => {
    apiMock.get.mockReset()
    apiMock.post.mockReset()
    apiMock.put.mockReset()
    apiMock.post.mockResolvedValue({ data: { code: 0 } })
    apiMock.put.mockResolvedValue({ data: { code: 0 } })
  })

  it("导入证书：续期按钮禁用并提示原因", async () => {
    mockCert(certFactory({ provider: "import" }))
    renderPage()
    await waitLoaded()

    const renewBtn = screen.getByRole("button", { name: /续期/ })
    expect(renewBtn).toBeDisabled()
    expect(renewBtn).toHaveAttribute("title", "导入的证书无法续期")
  })

  it("导入证书：吊销按钮禁用并提示原因", async () => {
    mockCert(certFactory({ provider: "import" }))
    renderPage()
    await waitLoaded()

    const revokeBtn = screen.getByRole("button", { name: /吊销/ })
    expect(revokeBtn).toBeDisabled()
    expect(revokeBtn).toHaveAttribute("title", "导入的证书无法吊销")
  })

  it("未绑定部署目标：部署按钮禁用并提示原因", async () => {
    mockCert(certFactory({ deploy_target_ids: [] }))
    renderPage()
    await waitLoaded()

    const deployBtn = screen.getByRole("button", { name: /部署/ })
    expect(deployBtn).toBeDisabled()
    expect(deployBtn).toHaveAttribute("title", "该证书未绑定部署目标")
  })

  it("云厂商证书：吊销对话框显示云厂商不支持吊销的提示", async () => {
    const user = userEvent.setup()
    mockCert(certFactory({ provider: "aliyun" }))
    renderPage()
    await waitLoaded()

    await user.click(screen.getByRole("button", { name: /吊销/ }))
    await screen.findByText("确认吊销证书？")
    expect(screen.getByText(/云厂商渠道不支持吊销/)).toBeInTheDocument()
    expect(screen.getByText(/云控制台/)).toBeInTheDocument()
  })

  it("ACME 证书：吊销对话框不显示云厂商提示", async () => {
    const user = userEvent.setup()
    mockCert(certFactory({ provider: "acme" }))
    renderPage()
    await waitLoaded()

    await user.click(screen.getByRole("button", { name: /吊销/ }))
    await screen.findByText("确认吊销证书？")
    expect(screen.queryByText(/云厂商渠道不支持吊销/)).not.toBeInTheDocument()
  })

  it("剩余有效期大于 30 天：续期确认对话框显示提前续期警告，确认后调用续期接口", async () => {
    const user = userEvent.setup()
    mockCert(certFactory({ days_to_expire: 60 }))
    renderPage()
    await waitLoaded()

    await user.click(screen.getByRole("button", { name: /续期/ }))
    await screen.findByText("确认续期证书？")
    expect(screen.getByText(/尚未临近到期/)).toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "确认续期" }))
    await waitFor(() => expect(apiMock.post).toHaveBeenCalledWith("/certificates/cert-1/renew"))
  })

  it("部署确认流程：确认后调用部署接口", async () => {
    const user = userEvent.setup()
    mockCert(certFactory())
    renderPage()
    await waitLoaded()

    await user.click(screen.getByRole("button", { name: /部署/ }))
    await screen.findByText("确认部署证书？")

    await user.click(screen.getByRole("button", { name: "确认部署" }))
    await waitFor(() => expect(apiMock.post).toHaveBeenCalledWith("/certificates/cert-1/deploy"))
  })

  it("通知事件：显示勾选事件徽标，编辑保存后调用 notify-events 接口", async () => {
    const user = userEvent.setup()
    mockCert(certFactory())
    renderPage()
    await waitLoaded()

    expect(screen.getByText("到期提醒")).toBeInTheDocument()
    expect(screen.getByText("续期成功")).toBeInTheDocument()
    expect(screen.queryByText("部署失败")).not.toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "编辑通知事件" }))
    await screen.findByText("编辑通知事件")

    await user.click(screen.getByRole("checkbox", { name: /部署失败/ }))
    await user.click(screen.getByRole("checkbox", { name: /到期提醒/ }))
    await user.click(screen.getByRole("button", { name: "保存" }))

    await waitFor(() =>
      expect(apiMock.put).toHaveBeenCalledWith("/certificates/cert-1/notify-events", {
        notify_events: ["renew_success", "deploy_failed"],
      }),
    )
  })
})