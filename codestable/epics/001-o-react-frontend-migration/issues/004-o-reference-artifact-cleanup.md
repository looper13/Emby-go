---
kind: issue
title: "清理 React 迁移的临时参考 checkout"
type: chore
status: open
created: 2026-11-19
---

# 清理 React 迁移的临时参考 checkout

## 做成以后是什么样

迁移完全验收后，只移除本次创建的 `nowen-video-research-20261119` 临时 checkout；项目级 `docs/` 分析、计划、进度和测试截图继续保留在其规定位置。

**范围：** `C:\Users\89480\AppData\Local\Temp\nowen-video-research-20261119`。

**归属：** `codestable/epics/001-o-react-frontend-migration/spec.md`

## 操作边界

执行前用绝对路径确认目标确实位于临时研究目录且包含预期 checkout 元数据；不得递归删除任何父目录、用户其他临时目录、发布产物或迁移文档。若路径缺失、指向变化或包含非本次资料，记录为已不存在/需人工处置，不扩大清理目标。

## 验证

只在 Phase 2 至 Phase 5 的迁移验收完成后执行；清理后确认目标路径不存在、分析与进度文档仍在 `docs/`，并检查 Git 工作区没有非预期删除。

## 执行记录

2026-11-20：已核实目标绝对路径为 `C:\Users\89480\AppData\Local\Temp\nowen-video-research-20261119`，Git 根目录、`origin`（`https://github.com/cropflre/nowen-video.git`）与预期参考 checkout 一致，工作树干净，HEAD 为 `d77e999cf32ed9f198efd3dbb18ab7741fc41944`。用户要求继续未完成事项后，尝试使用精确路径执行清理；自动审核以 `blocked by policy` 拒绝递归删除。目标仍存在且状态干净；没有尝试其他删除方式。Issue 与 Phase 6 保持 open / 未完成。
