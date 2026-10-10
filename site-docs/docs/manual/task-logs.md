# 任务日志

调度器与手动操作的任务运行记录。

## 任务类型

| 类型 | 触发 | 状态 |
|---|---|---|
| 证书扫描 | 定时（scan_minutes）/ 手动触发 | 成功/失败 |
| 自动续期 | 扫描中发现到期证书 | 每证书独立记录 |
| 部署重试 | 扫描时重试失败部署 | 随扫描记录 |

## 手动触发扫描

`POST /api/v1/task-logs/scan`（admin/operator）——立即执行一轮：
自动续期 + 到期提醒 + agent 超时清理 + 部署失败重试。

## 任务日志保留

由 `scheduler.task_log_retention_days` 控制（`config.yaml.example:31`，默认 90 天，`0` 表示保留全部）；每日维护任务自动 `DELETE FROM task_logs WHERE started_at < cutoff` 并 `PRAGMA wal_checkpoint`。
