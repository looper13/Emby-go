---
kind: issue
title: 任务日志显示局部刷新失败路径与原因
type: ff
status: closed
created: 2026-10-26
---

局部刷新有条目失败时，任务日志可展开查看具体目录、文件、处理阶段和原始错误。页面最多列出 20 条，完整失败项逐条写入运行日志；失败条目仍保留原索引，监听器仍等待文件变化或下次扫描。

- 改动：`internal/scanner/failure.go` 汇总有界失败明细，扫描器记录 NFO、文件状态及媒体元数据错误；`internal/server/library_watch.go` 将明细传入任务历史；`frontend/src/components/tasks/TaskError.vue` 和任务页提供展开查看及手机纵向布局。
- 验证：扫描器与服务端定向回归、前端类型检查及任务页 21 项测试、`git diff --check` 通过；浏览器在 1280px 和 390px 检查展开态，页面无横向溢出。此前全量前端 333 项测试及排除既有外部依赖用例后的 Go 回归通过。
- codestable：无 spec/epic 需同步；关联既有 `012-x-ff-watch-partial-failure-retry.md` 的失败后重试边界。本地夹具和浏览器验证，尚未在实际媒体库或部署环境验收。
