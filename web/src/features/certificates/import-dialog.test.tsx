import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { act, render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { useState } from "react"
import { ImportDialog } from "./import-dialog"
import { readFileAsBase64 } from "@/lib/file"
import { api } from "@/lib/api"

const apiMock = api as unknown as {
  get: ReturnType<typeof vi.fn>
  post: ReturnType<typeof vi.fn>
}

vi.mock("@/lib/api", () => ({
  api: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
  getErrorMessage: vi.fn(() => "请求失败"),
}))

class FakeFileReader {
  result: string | ArrayBuffer | null = null
  onload: (() => void) | null = null
  onerror: (() => void) | null = null
  readAsArrayBuffer(blob: Blob) {
    void blob.arrayBuffer().then((buf) => {
      this.result = buf
      this.onload?.()
    })
  }
  readAsText(blob: Blob) {
    void blob.text().then((text) => {
      this.result = text
      this.onload?.()
    })
  }
}

function renderDialog() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  let setOpen!: (v: boolean) => void
  function Harness() {
    const [open, setOpenState] = useState(false)
    setOpen = setOpenState
    return (
      <QueryClientProvider client={queryClient}>
        <ImportDialog open={open} onOpenChange={setOpenState} />
      </QueryClientProvider>
    )
  }
  render(<Harness />)
  return { open: (v: boolean) => act(() => setOpen(v)) }
}

describe("readFileAsBase64", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("正确编码小文件", async () => {
    vi.stubGlobal("FileReader", FakeFileReader)
    const file = new File([new Uint8Array([0x48, 0x65, 0x6c, 0x6c, 0x6f])], "test.pfx")
    await expect(readFileAsBase64(file)).resolves.toBe("SGVsbG8=")
  })

  it("超过单个分块（0x8000 字节）时结果与真实 base64 一致", async () => {
    vi.stubGlobal("FileReader", FakeFileReader)
    const big = new Uint8Array(0x10000).map((_, i) => i % 251)
    const file = new File([big], "big.pfx")
    const b64 = await readFileAsBase64(file)
    const restored = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0))
    expect(restored).toEqual(big)
  })
})

describe("ImportDialog 状态重置", () => {
  beforeEach(() => {
    apiMock.get.mockReset()
    apiMock.post.mockReset()
    apiMock.get.mockResolvedValue({ data: [{ id: "t1", name: "目标1", type: "local" }] })
    apiMock.post.mockResolvedValue({ data: { code: 0 } })
  })

  it("关闭再打开后，PEM 表单内容与部署目标选择被清空", async () => {
    const user = userEvent.setup()
    const { open } = renderDialog()

    open(true)
    await screen.findByText("导入证书")

    await user.type(screen.getByPlaceholderText("example.com"), "example.com")
    await user.type(screen.getByPlaceholderText("-----BEGIN CERTIFICATE-----"), "CERTDATA")
    await user.type(screen.getByPlaceholderText("-----BEGIN EC PRIVATE KEY-----"), "KEYDATA")
    await user.click(screen.getByRole("button", { name: "目标1" }))

    await user.click(screen.getByRole("button", { name: "取消" }))
    open(true)
    await screen.findByText("导入证书")

    expect(screen.getByPlaceholderText<HTMLInputElement>("example.com").value).toBe("")
    expect(screen.getByPlaceholderText<HTMLTextAreaElement>("-----BEGIN CERTIFICATE-----").value).toBe("")
    expect(screen.getByPlaceholderText<HTMLTextAreaElement>("-----BEGIN EC PRIVATE KEY-----").value).toBe("")

    // 重新填写并提交：部署目标选择应已被清空
    await user.type(screen.getByPlaceholderText("example.com"), "example.com")
    await user.type(screen.getByPlaceholderText("-----BEGIN CERTIFICATE-----"), "CERTDATA")
    await user.type(screen.getByPlaceholderText("-----BEGIN EC PRIVATE KEY-----"), "KEYDATA")
    await user.click(screen.getByRole("button", { name: "导入" }))

    await waitFor(() => expect(apiMock.post).toHaveBeenCalled())
    const body = apiMock.post.mock.calls.at(-1)?.[1] as {
      deploy_target_ids?: string[]
      notify_events?: string[]
    }
    expect(body.deploy_target_ids).toEqual([])
    expect(body.notify_events).toEqual([
      "cert_expiring",
      "renew_success",
      "renew_failed",
      "deploy_success",
      "deploy_failed",
    ])
  })

  it("关闭再打开后，导入方式重置回 PEM", async () => {
    const user = userEvent.setup()
    const { open } = renderDialog()

    open(true)
    await screen.findByText("导入证书")

    await user.click(screen.getByRole("combobox"))
    await user.click(await screen.findByRole("option", { name: /PFX/ }))
    const pwd = screen.getByPlaceholderText<HTMLInputElement>("PFX 文件的密码")
    await user.type(pwd, "secret123")

    await user.click(screen.getByRole("button", { name: "取消" }))
    open(true)
    await screen.findByText("导入证书")

    expect(screen.getByPlaceholderText("example.com")).toBeInTheDocument()
    expect(screen.queryByPlaceholderText("PFX 文件的密码")).not.toBeInTheDocument()
  })
})