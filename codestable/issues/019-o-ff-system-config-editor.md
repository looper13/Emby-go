---
kind: issue
title: "实例配置页面与 YAML 在线保存"
type: ff
status: open
created: 2026-10-12
---

# 实例配置页面与 YAML 在线保存

管理员在设置页编辑启动 YAML 并显式保存。服务名、备用域名、监控设置和探测参数无需重启；端口、调试、数据库、演员目录与 Redis 保存后标记待重启。恢复、草稿保留、密码不回传/明确清除、并发版本冲突、原子文件替换已实现。页面结构与具体生效时机见 [配置说明](../../SYSTEM-CONFIG.md)。

- 改动：`internal/config/edit.go` 负责 YAML 注释/未知字段与别名保留、字段校验、版本和原子写入；`internal/server/system_config.go` 提供鉴权的 GET/PUT 与运行配置快照；相关读取使用互斥快照，监控变更重建监听并返回异常，媒体库增删与配置重建串行。Vue 设置表单分组编辑、恢复、保存失败保留、离开确认与移动布局；运行状态独立保留。systemd 示例开放配置目录写权限，部署说明覆盖已有安装升级。
- 验证（2026-10-12）：`go test ./internal/config ./internal/server -skip 'TestMediaProbe$|TestProbeCircuitBreaker$' -count=1` 通过（server 70.458 秒）。这两个用例沿用前一阶段记录的媒体探测基线限制，本次未重跑，不代表全量回归通过。最后增补 YAML 别名保护、监控异常状态保留及一致快照后，配置/API/核心 API/域名/禁用监控针对性测试通过；`go vet ./internal/config ./internal/server`、Go 主程序构建、`npm run typecheck`、`git diff --check` 通过。
- 前端补验（2026-10-12，权限解除后）：正式 `npm run build` 通过，5 个校验产物同步至 `internal/server/web_dist`；Vitest `system-config.test.ts` / `admin.test.ts` 共 15 项通过；Edge Playwright `system-config.spec.ts` 桌面与移动端 2 项通过，覆盖显式保存、恢复、服务名称在线生效、重启字段保持旧运行值、刷新持久性、密码不回传与无横向溢出。截图检查后统一双列与区块间距，重新正式构建并跑过同样 2 项（22.6 秒）。此前 `EPERM` 及审批 503 是环境阻塞，现已解除。
- 剩余边界：systemd Linux 实机权限行为尚未验收；本次仅补设置页的前端验证，不代表此前其他功能的所有未验收项已通过。
- 推送前复验：前端全套 197 项单测、11 项构建流程测试及类型检查通过；干净源文件默认 Go 构建、缺失 UI 拒绝、Windows/Linux amd64 embedui 交叉构建通过。演员与实例配置 server 定向测试通过。修复测试来源状态泄漏后，相邻演员/P5 桌面/移动端 18 项浏览器验收通过，记录在 `actor-assets-plan.md`。
- codestable：无既有配置页面规格需要回写。页面说明已同步验证状态；保留 open 记录。用户已明确授权验证后提交并推送；未部署或发布版本。
