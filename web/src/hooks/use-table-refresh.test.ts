import { describe, it, expect, vi } from "vitest"
import { act, renderHook } from "@testing-library/react"

import { useTableRefresh } from "./use-table-refresh"

describe("useTableRefresh", () => {
  it("刷新期间 refreshing 为 true，请求结束后回落为 false", async () => {
    let resolveFetch: (v: unknown) => void = () => {}
    const refetch = vi.fn(
      () =>
        new Promise((resolve) => {
          resolveFetch = resolve
        }),
    )
    const { result } = renderHook(() => useTableRefresh(refetch))

    expect(result.current.refreshing).toBe(false)

    let pending: Promise<void>
    act(() => {
      pending = result.current.refresh()
    })
    expect(result.current.refreshing).toBe(true)
    expect(refetch).toHaveBeenCalledTimes(1)

    await act(async () => {
      resolveFetch({ data: [] })
      await pending
    })
    expect(result.current.refreshing).toBe(false)
  })

  it("请求失败时 refreshing 同样回落（不卡在遮罩态）", async () => {
    const refetch = vi.fn(() => Promise.reject(new Error("network")))
    const { result } = renderHook(() => useTableRefresh(refetch))

    await act(async () => {
      await result.current.refresh()
    })
    expect(result.current.refreshing).toBe(false)
  })

  it("请求瞬时返回时遮罩仍展示满最小时长（避免一闪而过）", async () => {
    const refetch = vi.fn(() => Promise.resolve({ data: [] }))
    const { result } = renderHook(() => useTableRefresh(refetch))

    let pending: Promise<void>
    act(() => {
      pending = result.current.refresh()
    })
    // 请求已经返回，但未满 400ms 最短展示时长，遮罩应仍在
    await act(async () => {
      await Promise.resolve()
      await Promise.resolve()
    })
    expect(result.current.refreshing).toBe(true)

    await act(async () => {
      await pending
    })
    expect(result.current.refreshing).toBe(false)
  })
})
