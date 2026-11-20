---
kind: issue
title: "退役 Vue 前端与开发工具链"
type: refactor
status: open
created: 2026-11-19
---

# 退役 Vue 前端与开发工具链

## 做成以后是什么样

正式 React 页面和回归证据达到 Epic 关闭条件后，移除仅供 Vue 入口和 Vue 组件测试使用的源码、插件、依赖和旧测试；从干净安装到 `/web/ui/` 构建不再需要 Vue。

**范围：** Vue SFC、Vue-only stores/composables、仅 Vue 的组件测试与 setup、Vue/Vue Router/Pinia/Vue Test Utils/插件/类型检查依赖、开发文档和构建引用。

**归属：** `codestable/epics/001-o-react-frontend-migration/spec.md`

## 安全顺序

Phase 3 功能对照与 Phase 4 回归/发行验证完成前不得批量删除 Vue 代码。先用 `rg` 区分共享 API/DTO/纯逻辑与 Vue-only 模块；只删除 React 已覆盖且不再被其他目标导入的文件。共享行为迁移到 React 后，删除对应 Vue 测试必须同时保留等价的可观察断言。

## 需要同步

- 更新 `docs/README.md`、`docs/dev-plan.md`、命令示例和开发/发布说明。
- `frontend/package.json`、锁文件、Vite 与 TypeScript 配置、测试 setup 不再安装/编译 Vue-only 代码。
- 全仓 `rg` 复核活跃代码/构建/测试中的 Vue/Pinia 引用；文档中的历史实现说明可保留并明确标记。
- 保留 `docs/frontend-vue-migration-plan.md` 历史方案；`/admin-vue` 旧书签按现有重定向契约保留并继续测试。
- 不恢复 `vue-migration/`，不移除共享纯逻辑或 API/DTO，不强制加入临时报告与凭据。

## 验证

用干净依赖安装执行 typecheck、全量测试、React build、安全 Go 回归、Go 静态资源测试及 Windows/Linux 嵌入构建；执行旧 URL 和未知 API 隔离回归。所有入口使用实际 React 产物，无 Vue 运行时挂载。

## 执行记录

2026-11-20：Vue-only SFC、入口/路由、stores、composables、组件测试及 Vue/Pinia 构建依赖已从当前前端移除；共享 API/DTO 和纯逻辑保留。`package.json`、锁文件、Vite、TypeScript、构建/预览脚本和测试 setup 均使用 React 工具链。主说明与开发文档以 React 为当前入口，`/admin-vue` 查询/fragment 兼容重定向及未知 API 行为有测试。干净副本 `npm ci`、typecheck、36 个文件/235 项单测、React build、Go UI 资源测试、Windows/Linux/macOS amd64/arm64 五平台嵌入构建和安全 Go 全回归通过；安全回归排除会清理本机 Redis DB15 的 `TestRedisBehavior`。活跃源码、测试和配置中的残余 `vue`/`pinia` 搜索结果仅为 `/admin-vue` 兼容测试及服务端路由；历史迁移方案保留在 `docs/`。Phase 5 实施项完成，Issue 状态仍为 open。原 Vue 行为清单的 React 覆盖审计已由 Issue `002` 完成。
