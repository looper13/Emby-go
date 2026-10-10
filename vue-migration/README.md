# Vue 迁移工作区

本目录存放 [frontend-vue-migration-plan.md](../frontend-vue-migration-plan.md) 实施过程中的盘点基线、夹具与证据。

硬性约束：

- `internal/server/web/` 与 `internal/server/web_test/` 是冻结的比对基线，迁移验证完成前只读，禁止修改、删除、移动其中任何文件。
- P0 期间不修改仓库内任何现有文件；所有盘点产物只写入本目录。
- 测试数据一律放系统临时目录，禁止对用户真实媒体库做删除、写 NFO 或全量重建。

## P0（VUE-01）产物索引

| 文件 | 内容 | 状态 |
|---|---|---|
| [P0/api-inventory.md](P0/api-inventory.md) | 逐接口 DTO、字段、状态码、副作用；UI 已用/未用矩阵 | ✅ 完成 |
| [P0/ct-fixtures.md](P0/ct-fixtures.md) | CT-01/02/03 前后端镜像规则对照夹具说明 | ✅ 完成 |
| [P0/behavior-inventory.md](P0/behavior-inventory.md) | 逐页功能对照表（点哪个按钮发生什么） | ✅ 完成 |
| [P0/component-split.md](P0/component-split.md) | 页面 → 子组件 → 状态/事件/测试 拆分清单 | ✅ 完成 |
| [P0/global-behaviors.md](P0/global-behaviors.md) | GL-01..35 全局行为编号清单（含代码锚点） | ✅ 完成 |
| [P0/test-mapping.md](P0/test-mapping.md) | 17 项回归与新场景的测试落点映射 | ✅ 完成 |
| [P0/baseline-env.md](P0/baseline-env.md) | 环境版本、基线命令结果、既有失败（F1 稳定/F2 偶发） | ✅ 完成 |
| [fixtures/ct/](fixtures/ct/) | CT-01/02/03 JSON 夹具 | CT-01/02 已由真实 Go 实现与 Vue 共用验证；CT-03 已由 Go 写契约测试与真实 embedui 浏览器验证（P5） |
| [fixtures/json/](fixtures/json/) | 真实服务 JSON 响应夹具 | 29 份已核验；11 份只做临时路径脱敏 |
| [screenshots/](screenshots/) | 旧 UI 桌面/手机截图 | 保留原 16 张；补采两轮 300/250 主链路，不代表全部动态态已覆盖 |
| [P0/live-env-notes.md](P0/live-env-notes.md) | 临时环境配方、请求计数、复现脚本 | 本轮完成；两轮正常运行及故障清理通过 |

## 当前进展（2026-10-10）

- **最新接续点：P7/VUE-15 已完成，VUE-16 本地实现与验证完成，Linux 等项待验。** 已补 8000 条大库、50 次切页、20 次播放器开关和真实 chunk 404 检查，并修复 Artplayer 全屏监听泄漏。源码正式入口已切换 Vue，189 单测、68 项完整浏览器回归、6 项独立性能检查、五平台 embedui 构建、Windows 回滚与恢复 Vue、本地双构建脚本均通过；证据见 [P7/progress.md](P7/progress.md)。用户确认没有 Linux 环境，systemd/回滚保留待验；提交后真实干净检出及旧 UI/harness 最终移除仍待完成，整体迁移未标完成。

- P6/VUE-14 已完成全量回归与浏览器验证。17 项旧场景、GL-01..35、§9.2 新场景与 20 项补测清单逐条对照（[P6/coverage-matrix.md](P6/coverage-matrix.md)），189 项单元/组件及 58 项桌面/手机真实服务回归通过，详见 [P6/progress.md](P6/progress.md)。

- P5/VUE-11/12/13 已完成任务与刮削。应用持有探测/刮削/头像任务，批量与单条预览、CT-03 真实写入、计划任务 CRUD 与 cron 校验已接通；181 项单元/组件及 58 项桌面/手机浏览器回归通过，详见 [P5/progress.md](P5/progress.md)。

- P4/VUE-08/09/10 已完成媒体交互。全部筛选、卡片动作/用户数据、完整详情分区、推荐与实体跳转、重读/单条探测、Artplayer 及一次代理回退已接通。153 项单元/组件及 42 项桌面/手机浏览器回归通过；最终资源另做 P4 10 项补验，详见 [P4/progress.md](P4/progress.md)。Go 全包当时三项失败，目录监听/轮询单独复查通过，未标为全绿。

- P3 管理基础页面已完成；总览、媒体库、只读设置、API 密钥、手动补录、任务历史和接口探针接真实 API，扫描/重建切页继续。历史验收见 [P3/progress.md](P3/progress.md)。

- P2 核心通路已完成；真实 Vue 登录 → 媒体墙 → 详情 → 返回，桌面/手机 300/250 均通过，无重复/返回分页；历史阶段记录见 [P2/progress.md](P2/progress.md)。

- 中断的环境采集已接续完成，使用 PowerShell/Node，无需等待 Bash。旧版媒体墙两轮桌面/手机均恢复 300 条和第 250 条的焦点/阅读位置，重复分页与返回分页均为 0。
- P1 工程与 Go 嵌入链路已实现；Vue 最小登录/总览可运行。阶段证据、构建命令及剩余缺口见 [P1/progress.md](P1/progress.md)。
- P1-E 隔离环境已接入真实 embedui 浏览器测试；P2 至 P5 持续复用，完整媒体操作、真实播放器、刮削写入与计划任务已纳入桌面/手机回归。
- `internal/server/web/`、`internal/server/web_test/` 继续冻结；本轮按用户授权提交到本地 Git，未 push、未部署。P7 已在源码切换正式入口，迁移验证未全部完成。
- 本机不要直接运行 `go test ./...`：既有 `TestRedisBehavior` 会清理本机 6379 的 DB15。使用记录排除项的 `npm --prefix frontend run test:go-regression`，详见 [基线安全补充](P0/baseline-env.md#4-每次阶段验证复用的基线命令组)。P7 Go 全包有 `TestMediaProbe` 既有差异及 `TestProbeCircuitBreaker` 负载超时（后者单独复查通过），不标记为全绿。
- 浏览器与 Go 回归命令必须在 PowerShell 中执行：WSL 不向 Win32 进程传递 `PW_CHANNEL`、`EMBY_MIGRATION_PHASE` 等环境变量，误从 WSL 运行会在启动浏览器阶段整体失败。
