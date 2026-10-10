import { describe, it, expect, vi, beforeEach } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { IssueDialog } from "./issue-dialog"
import { api } from "@/lib/api"

const apiMock = api as unknown as {
  get: ReturnType<typeof vi.fn>
  post: ReturnType<typeof vi.fn>
}

vi.mock("@/lib/api", () => ({
  api: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
  getErrorMessage: vi.fn(() => "请求失败"),
}))

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn(), warning: vi.fn() },
}))

const accountsFixture = [
  {
    id: "acme-1",
    provider: "acme",
    name: "Let's Encrypt",
    directory_url: "https://acme-v02.api.letsencrypt.org/directory",
    contact: "admin@example.com",
    has_eab: false,
    has_credentials: true,
    status: "valid",
    last_error: "",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  },
  {
    id: "ali-1",
    provider: "aliyun",
    name: "阿里云",
    directory_url: "",
    contact: "",
    has_eab: false,
    has_credentials: true,
    status: "valid",
    last_error: "",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  },
]

function mockData() {
  apiMock.get.mockImplementation(async (url: string) => {
    if (url === "/acme-accounts") return { data: accountsFixture }
    if (url === "/deploy-targets") return { data: [{ id: "t1", name: "目标1", type: "local" }] }
    if (url === "/settings/contacts") return { data: [] }
    return { data: [] }
  })
}

function renderDialog() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <IssueDialog open onOpenChange={() => {}} />
    </QueryClientProvider>,
  )
}

async function selectAccount(user: ReturnType<typeof userEvent.setup>, name: RegExp) {
  await user.click(screen.getAllByRole("combobox")[0])
  await user.click(await screen.findByRole("option", { name }))
}

describe("IssueDialog CSR 模式与表单逻辑", () => {
  beforeEach(() => {
    apiMock.get.mockReset()
    apiMock.post.mockReset()
    mockData()
  })

  it("未选择账户时不显示 CSR 开关", async () => {
    renderDialog()
    await screen.findByText("签发证书")
    expect(screen.queryByText("CSR 模式（私钥保留在客户端）")).not.toBeInTheDocument()
  })

  it("选择 ACME 账户后显示 CSR 开关，选择云厂商账户则隐藏并显示申请人区", async () => {
    const user = userEvent.setup()
    renderDialog()
    await screen.findByText("签发证书")

    await selectAccount(user, /Let's Encrypt/)
    expect(screen.getByText("CSR 模式（私钥保留在客户端）")).toBeInTheDocument()

    await selectAccount(user, /阿里云/)
    expect(screen.queryByText("CSR 模式（私钥保留在客户端）")).not.toBeInTheDocument()
    expect(screen.getByText("申请人")).toBeInTheDocument()
  })

  it("切换账户后 CSR 模式被重置为关闭", async () => {
    const user = userEvent.setup()
    renderDialog()
    await screen.findByText("签发证书")

    await selectAccount(user, /Let's Encrypt/)
    const csr = screen.getByRole("checkbox", { name: /CSR 模式/ })
    await user.click(csr)
    expect((csr as HTMLInputElement).checked).toBe(true)

    await selectAccount(user, /阿里云/)
    await selectAccount(user, /Let's Encrypt/)
    const csr2 = screen.getByRole("checkbox", { name: /CSR 模式/ })
    expect((csr2 as HTMLInputElement).checked).toBe(false)
  })

  it("非 CSR 模式且未填域名：提交提示至少输入一个域名且不调用接口", async () => {
    const user = userEvent.setup()
    renderDialog()
    await screen.findByText("签发证书")

    await selectAccount(user, /Let's Encrypt/)
    await user.click(screen.getByRole("button", { name: "签发" }))

    expect(await screen.findByText("至少输入一个域名")).toBeInTheDocument()
    expect(apiMock.post).not.toHaveBeenCalled()
  })

  it("CSR 模式且未粘贴 CSR：提交提示请粘贴 CSR 且不调用接口", async () => {
    const user = userEvent.setup()
    renderDialog()
    await screen.findByText("签发证书")

    await selectAccount(user, /Let's Encrypt/)
    await user.click(screen.getByRole("checkbox", { name: /CSR 模式/ }))
    await user.click(screen.getByRole("button", { name: "签发" }))

    expect(await screen.findByText("请粘贴 CSR（PEM）")).toBeInTheDocument()
    expect(apiMock.post).not.toHaveBeenCalled()
  })

  it("ACME + HTTP-01 + 域名 + 部署目标：提交体正确（csr_pem 为空、无 dns_config）", async () => {
    const user = userEvent.setup()
    renderDialog()
    await screen.findByText("签发证书")

    await selectAccount(user, /Let's Encrypt/)
    await user.type(
      screen.getByPlaceholderText("example.com, www.example.com"),
      "example.com, www.example.com",
    )
    await user.click(screen.getByRole("button", { name: "目标1" }))
    await user.click(screen.getByRole("button", { name: "签发" }))

    await waitFor(() => expect(apiMock.post).toHaveBeenCalled())
    const [url, body] = apiMock.post.mock.calls.at(-1) ?? []
    expect(url).toBe("/certificates")
    expect(body).toMatchObject({
      domains: ["example.com", "www.example.com"],
      account_id: "acme-1",
      verification: "http01",
      csr_pem: "",
      deploy_target_ids: ["t1"],
      notify_events: [
        "cert_expiring",
        "renew_success",
        "renew_failed",
        "issue_success",
        "issue_failed",
        "deploy_success",
        "deploy_failed",
      ],
    })
    expect(body.dns_config).toBeUndefined()
  })

  it("取消勾选某个通知事件后，提交体不包含该事件", async () => {
    const user = userEvent.setup()
    renderDialog()
    await screen.findByText("签发证书")

    await selectAccount(user, /Let's Encrypt/)
    await user.type(
      screen.getByPlaceholderText("example.com, www.example.com"),
      "example.com",
    )
    await user.click(screen.getByRole("checkbox", { name: /签发成功/ }))
    await user.click(screen.getByRole("button", { name: "签发" }))

    await waitFor(() => expect(apiMock.post).toHaveBeenCalled())
    const [, body] = apiMock.post.mock.calls.at(-1) ?? []
    expect(body.notify_events).not.toContain("issue_success")
    expect(body.notify_events).toContain("issue_failed")
  })
})