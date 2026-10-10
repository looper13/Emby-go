# P1 工程与构建集成

日期：2026-10-10。继续使用根目录开发计划与本目录阶段记录，不重新生成第二套开发计划。

## 边界

- `internal/server/web/`、`internal/server/web_test/` 冻结，只读比对。
- `/admin`、`/`、`/web`、`/web/index.html` 保持旧版，Vue 只在 `/admin-vue`。
- 所有运行验证使用一次性 SQLite、专用 Redis 与临时媒体目录，不使用私人配置。
- 版本选择采用本机已经核实的 Node 24.14.1 / npm 11.11.0，并同步 CI；取代计划中建议的 Node 22.x，不修改用户全局工具链。

## 当前任务

- [x] VUE-01 本轮收尾：旧采集产物核验、请求计数、两轮桌面/手机复现与中断清理证据。
- [x] VUE-02 工程：锁定依赖、strict 类型检查、API client、当前页面 DTO、认证状态、Vitest 与 Playwright 配置。未迁移页面的 DTO 随页面补充。
- [x] VUE-03 实现：构建校验与资源发布、默认/嵌入 Go 资源、独立入口、本地构建脚本和 release workflow。
- [x] 集成验证：无 frontend/web_dist 的临时源码副本可直接 Go 构建；缺资源的 embedui 明确失败；真实 Windows/Linux amd64 产物可生成；直接访问嵌入二进制的浏览器检查通过。
- [x] VUE-17 基础设施：临时 SQLite/媒体、专用 Redis、动态端口、就绪检查、失败 trace 脱敏及清理；正常连续两轮和注入故障均通过。Go 监听地址仍为 `:port`，严格网络隔离需另由运行环境提供。
- [x] P2/VUE-04 接续验收：初始化浏览器通路及核心认证/历史场景；证据见 [P2/progress.md](../P2/progress.md)。
- [x] P2/VUE-05：Vue 媒体墙/详情最小读取通路及 300/250、迟到请求、直接打开与返回测试；证据见 P2。
- [ ] 本地 build.sh/build.bat 端到端执行、CI 实际运行、目标平台部署和正式切换。

## 已实现范围

`frontend/` 使用 Vue 3、TypeScript、Router、Pinia；全局 CSS、favicon、Artplayer 文件从冻结目录逐字节复制。原 CSS 未改类名或添加 scoped。Artplayer 只保留源码与许可证，尚未加载，P4 再接入。

当前可操作页面为初始化/登录和只读总览。API client 区分 204、JSON/非 JSON 错误、网络失败、显式取消及超时，FormData 不设置 boundary；写请求不套页面卸载取消或通用短超时。认证复用 `emby_token`，合并检查与并发 401，网络失败保留 token，迟到的旧 token 响应不清除新登录。内部返回地址受路由白名单限制；新增资源升级提示由用户确认刷新。

本阶段有意记录的 DOM/行为差异：导航暂时仅显示总览；未实现的管理/扫描动作通过普通链接进入旧控制台；Vue 总览暂只读。它们是分阶段实现边界，不是完整 P3 功能或全页面视觉等价声明。

Vite 只清理 `frontend/dist`。发布脚本校验固定目录、符号链接/目录联接、锁、manifest、HTML 引用与 SHA-256，再切换 `internal/server/web_dist`；失败不回退使用陈旧资源。Go 共用资源处理器，默认模式按需读本地产物，缺资源返回 503；`embedui` 嵌入真实产物，资源缺失时编译失败。只有 manifest 证明的内容 hash 文件使用 immutable；API、旧入口和未知协议路径未被 SPA fallback 接管。

## 验证结果

| 检查 | 本轮结果 |
|---|---|
| strict 类型检查 | 通过 |
| Vitest | 75/75（API/认证/URL 72 项，冻结资产字节一致性 3 项） |
| Node 构建脚本测试 | 11/11 |
| 旧 JS 回归 + 隔离工具测试 | 17/17 + 4/4 |
| Go `TestWebUI*`、`TestCachePolicyHeaders` | 默认与实际 embedui 均通过；Windows 符号链接逃逸用例因权限不足跳过 |
| Go vet | 默认与 embedui 均通过 |
| Go 构建隔离检查 | 无 Node/前端产物的默认构建通过；真实 Windows/Linux amd64 编译通过，Linux 未运行 |
| 真实 embedui Playwright | 6/6；桌面 1280x800、手机模拟 390x844；入口、资源 GET/HEAD/MIME/缓存、登录/刷新/失效 token、错误输入保留、无横向溢出/JS 错误 |
| P0 真实旧版 300/250 | 两轮、每轮桌面/手机；0 重复分页，0 返回分页；证据见 P0 notes |
| 依赖安装审计 | 锁定 Vitest 4.1.11 等版本后，安装报告 0 vulnerabilities；不代表未来持续保证 |

最终前端构建发布 4 个校验过的文件：入口 0.53 kB，CSS 29.12 kB（gzip 6.69 kB），JS 117.81 kB（gzip 46.15 kB），另有 favicon。未生成 sourcemap，未打包 Artplayer；这些是当前最小页面的数值，不是完整迁移预算。

### Go 回归未全绿

本机既有 `internal/cache/cache_test.go` 的 `TestRedisBehavior` 会连接 `127.0.0.1:6379` DB15 并清理数据，因此本轮明确排除，未将其记为通过。安全入口为 `npm --prefix frontend run test:go-regression`；完整记录见 [go-regression.json](../artifacts/P1/go-regression.json)。

- `TestMediaProbe`：ffprobe 8.1.2 缺少测试期望的 reframes；P0 已记录相同失败。
- `TestSaveStreamDetailsReplacesExisting`、`TestSaveFileInfoSize`：全包运行出现 Windows rename Access denied；分别重跑 3 次通过。
- `TestPollingMonitorDebouncesRetriesAndTracksMoves`：全包运行出现目录占用；单独 3 次重跑仍有一次目录事件断言失败。

后三项处于本次未改动的后端模块，但不能仅凭此判定无关，仍需后续定位。本轮未修订其断言、未修改后端业务以掩盖失败。未运行完整 embedui 全包测试；只有上表的针对性测试与 vet 可记为通过。

## 可重复命令

在仓库根目录 PowerShell 执行；npm/Node 需匹配已锁定版本，Go、Redis、ffprobe 需在 PATH，浏览器需预先准备。

```powershell
npm --prefix frontend ci
npm --prefix frontend run typecheck
npm --prefix frontend run test:unit -- --run
npm --prefix frontend run test:build
npm --prefix frontend run build
node --test internal/server/web_test/app.test.cjs vue-migration/tools/live-env/live-env.test.mjs
npm --prefix frontend run test:go-build
go test ./internal/server -run 'Test(WebUI|CachePolicyHeaders)' -count=1
go test -tags embedui ./internal/server -run 'Test(WebUI|CachePolicyHeaders)' -count=1
go vet ./...
go vet -tags embedui ./...
$env:PW_CHANNEL = 'msedge'
npm --prefix frontend run test:e2e
npm --prefix frontend run test:go-regression
```

末条回归命令目前返回非零，原因如上。P0 连续两轮与故障清理复现命令见 [live-env-notes.md](../P0/live-env-notes.md)。Windows 当前使用已安装 Edge，不把浏览器未安装或 sandbox EPERM 算作通过。

`npm --prefix frontend run dev:isolated` 启动临时后端与 Vite，输出当次 URL 和一次性登录凭据；Ctrl+C 清理自有子进程和临时目录。优先使用此入口，不把测试代理指向用户的 18080/6379。刷新/跨端口不会共享 localStorage。运行日志、浏览器报告与产物均已忽略，不提交临时凭据。

## 下一步

本节为 P1 完成时的接续点；P2 已于 2026-10-10 实现并验证，最新状态见 [P2/progress.md](../P2/progress.md)，下一步 P3。全页面动态态、GL/CT 所有分支、播放/扫描/任务、升级中表单与扫描状态、真机和发布部署均未完成。P3-P7 不标为完成，不切换正式入口，不删除旧版。
