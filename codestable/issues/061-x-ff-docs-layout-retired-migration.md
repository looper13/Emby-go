---
kind: issue
title: "项目文档归入 docs 并移除旧迁移目录依赖"
type: ff
status: closed
created: 2026-10-29
---

# 项目文档归入 docs 并移除旧迁移目录依赖

用户确认 `vue-migration/` 已主动删除且不再使用；保留该删除，将仍被现行测试使用的少量文件归入当前前端测试目录，不恢复迁移工作区或阶段进度。项目级 Markdown 统一归入 `docs/`，根目录保留 `AGENTS.md`。

- 文档：移动根目录 12 份 Markdown，修正相对链接与路径引用；`docs/README.md` 保持项目主说明，命令默认从项目根目录执行。旧迁移计划标为历史参考，不把删除工作区当作 Linux 验收通过。部署/第三方专用 README 和固定位置 `codestable/` 保留原位置。
- 依赖：16 个仍被导入的 JSON 夹具放在 `frontend/tests/fixtures/`；隔离环境、媒体、脱敏和浏览器工具放在 `frontend/test-support/live-env/`。前端单测和 Go 共享契约仅修改引用；测试脚本报告转到既有忽略目录 `frontend/test-results/`，源码导出跳过工作区已删除的文件。
- 规则：`AGENTS.md` 明确文档、测试支持、临时报告位置以及旧迁移目录退役；`.gitignore` 只放行 `docs/` 根级 Markdown，内部报告继续忽略，不扩大凭证或临时证据的提交范围。
- 验证：全前端类型检查、37 文件 / 372 单测、4 个隔离工具测试、11 个资源构建测试、Vue production 构建通过；Go 共享实体契约连续 3 轮通过。现有浏览器登录/刷新冒烟在桌面和 390px 手机项目各通过一次，仅使用临时 Go/Redis/媒体环境，非生产服务验收；完整 E2E 和五平台 release 检查未运行。
- 文档/数据检查：54 个相对 Markdown 链接有效，内部报告 SHA256 不变，16 个夹具和 6 个保留模块与原 Git 内容逐字节一致；调用方单测没有删减或放宽断言。`vue-migration/` 未重新创建；原有 `output/` 删除保持原样。
- codestable：只更新已有事项中的文档引用，不改历史业务结论或状态；无现有 Project Spec/Notes。本记录是本轮归档痕迹，不修改既有 open 事项，也不自动提交、推送或部署。
