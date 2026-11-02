import * as React from "react"

// 遮罩最短展示时长：本地/内网 API 常在几毫秒内返回，若无下限，
// 遮罩会一闪而过、用户感知不到刷新动作。至少展示 400ms 给出明确反馈，
// 慢请求不受影响（展示到请求结束为止）。
const MIN_VISIBLE_MS = 400

/**
 * 列表页"手动刷新"状态机：点击后置 refreshing，请求结束（含失败）后清除。
 *
 * 为什么需要它：TanStack Query 的 isFetching 在带 refetchInterval 的页面
 * （任务日志 / Agent 每 5s 轮询）会周期性为真，若直接用它驱动整表遮罩，
 * 表格会每 5s 闪一次。本 hook 只包裹"用户主动点击"这一次刷新，
 * 遮罩只在实际点击后出现，反馈明确且不打扰自动轮询。
 *
 * @param refresh useQuery 返回的 refetch（或任意返回 Promise 的刷新函数，
 *                多查询可传 async () => { await Promise.all([...]) }）
 */
export function useTableRefresh(refresh: () => unknown) {
  const [refreshing, setRefreshing] = React.useState(false)
  const run = React.useCallback(async () => {
    const startedAt = Date.now()
    setRefreshing(true)
    try {
      await refresh()
    } catch {
      // 刷新失败的呈现由查询自身 error 状态与页面既有提示负责，
      // 此处只保证遮罩一定收起，且不向上抛出（onClick 场景会是未处理的 rejection）
    } finally {
      const elapsed = Date.now() - startedAt
      if (elapsed < MIN_VISIBLE_MS) {
        await new Promise((r) => setTimeout(r, MIN_VISIBLE_MS - elapsed))
      }
      setRefreshing(false)
    }
  }, [refresh])
  return { refreshing, refresh: run }
}
