# P4 完整媒体交互（已完成）

2026-10-10，接续 P3，完成 VUE-08/09/10。Vue 入口为 `/admin-vue`，正式 `/admin` 仍是旧版。未提交、未部署、未切换入口。旧 `internal/server/web/`、`web_test/` 与全局 CSS 继续冻结。

- [x] VUE-08：全部筛选、卡片动作/用户数据、分页与历史恢复。
- [x] VUE-09：完整详情分区、图片、实体跳转、推荐、重读/单条探测、刷新保留展开与位置。
- [x] VUE-10：真实 Artplayer、一次代理回退、迟到回调隔离、手机静音回退、弹层焦点和销毁。
- [x] CT-01/02 同组夹具由实际 Go 实现与 Vue 分别核对；真实 embedui 桌面/手机异常验证及阶段基线。Go 全包的失败单独记录，不作为通过项。

## 实现与状态归属

| 范围 | 行为与归属 |
|---|---|
| 媒体墙筛选 | `WallFilters` 展示库（≤8 为按钮，>8 为 select）、状态、刮削状态、搜索、排序、实体筛选及清除。`MediaWallPage` 持有搜索草稿与 400ms 防抖，离散筛选 push、搜索 replace；实体跳转仅保留 library_id。 |
| 分页与恢复 | `media-wall` store 保持 100 条补页、60s 快照、dirty 失效、代次与请求取消。`WallLoadState` 展示加载、明确重试及结束；沿用 P2 300/250 历史条目数量/位置/焦点恢复，本轮完整回归继续通过。 |
| 卡片 | `MediaCard`/`MediaGrid` 发出 open、quickplay、probe、reread、remove；按钮阻止点击冒泡，Enter/Space 仅卡片本身导航。海报内动作保持卡片尺寸，focus-within 可操作；渲染已看/收藏/多段、观看进度和状态徽标，避免标题重复番号。 |
| 短写 | `useMediaActions` 按动作/ID 加锁；写请求不绑页面 GET signal。完成时只刷新仍在同一 fullPath 的拥有页面；失败保留旧数据和可重试按钮。删除原生确认明确“仅删除数据库索引，不删除源文件”。 |
| 详情 | `ItemDetailPage` 持有主体、plot/files 展开、推荐与 loaded 标记；子组件顺序为 hero → 简介 → 预告片 → 剧照 → 演员 → 推荐 → 媒体信息 → 元数据 → 文件与时间。详情更新不替换整个页面；重读/探测保留展开、阅读位置与已加载推荐，失败继续可操作。 |
| 图片与实体 | 缓存 tag 在 maxWidth 前，剧照保留真实 ImageIndex，缩略图带宽度、原图链接不带宽度。演员使用 UTF-8/base64url 实体 ID；真实代表图接口已验证 200。`MediaImage` 一次回退后固定占位。元数据数组用显式空格分隔，避免模板压缩成连写。 |
| 推荐 | `/Items/:id/Similar?Limit=12` 与主体并行，5s 超时、父 signal 联动；代次保护迟到结果。推荐跳详情保留 query，返回走应用历史；超时只隐藏推荐，不阻塞主体。 |
| 播放器 | 应用层 `player` store 持有 options/触发焦点，`PlayerDialog` 拥有单个 Artplayer 实例。vendor 按需动态导入，使用 Vite default export，兼容原 UMD global。直连失败仅一次切 `/proxy`，异步回退静音并捕获 play 拒绝；再次失败仅一次 toast。预告片无代理。关闭、切页、认证失效/卸载销毁；延迟 import/error 不复活旧实例。 |
| 键盘 | 对话框带 role/aria-modal、Tab/Shift+Tab 限制焦点；Escape 优先关闭播放器，详情不同时返回；关闭恢复仍挂载的触发控件。详情输入控件聚焦时不误触 Escape。 |

实际组件边界回写 [P0/component-split.md](../P0/component-split.md)，DTO 字段按真实后端大小写解析，缺省/nil 数组归一为 []，已消费的畸形可选字段明确报错。没有修改后端生产逻辑。

P5 的单条刮削预览/确认与批量探测尚未迁移；对应入口明确标“刮削（旧版）”及“探测媒体信息（旧版）”，跳回旧控制台。CT-03、刮削轮询/预览 Escape 优先级、计划任务属于 P5。

## 对照与修正

- CT-01/02 共用 `fixtures/ct` 的 19+19 例：Go 测试直接调用 `scraper.joinNumberTitle` 与 `server.entityId`，验证并输出实际结果；Vue 消费同组输入。输出见 `artifacts/P4/ct-go-output.txt`。259LUXU 前后端已知差异继续保留，不归一化番号；实体 kind 转小写、名称不 trim，页面只传小写 kind。
- 首轮浏览器暴露 Artplayer UMD 经 Vite 构建后没有 global constructor，以及推荐横向列表固有宽度撑破详情。已改 default export 加 global fallback；详情根网格与 item-sections 局部设 `grid-template-columns:minmax(0,1fr)`，原 CSS 不变。桌面/手机均检查 document.scrollWidth ≤ innerWidth。
- 卡片中央现为快捷播放，P2 的导航用例改点标题；尺寸比较等面板动画结束；演员头像夹具改为有代表图的临时专用演员。这些是测试触发/夹具修正。
- 最终截图检查补上元数据数组空格，增加组件回归。全页截图用临时 screenshot style 隐藏路径文字，避免手机 fullPage 遮罩位置偏移；不改变页面生产行为。卡片截图保留动作覆盖层，并隐藏路径文字。

## 验证结果

| 验证 | 结果 |
|---|---|
| TypeScript 严格检查 | 通过 |
| Vitest 单元/组件 | 最终 153/153（9 文件，含 P4 新增 49 项） |
| 真实 embedui Playwright | 完整桌面/手机 42/42；元数据空格修正后的最终构建另复查 P4 桌面/手机 10/10；截图脱敏另补验 4 项 |
| Vite/Go 发布资源 | 通过；5 个 UI 文件，主 JS 189.59 kB（gzip 69.26），Artplayer 动态 chunk 133.43 kB（gzip 36.27），CSS 29.12 kB（gzip 6.69） |
| Node 构建脚本 | 11/11 |
| 冻结旧 JS / 隔离环境工具 | 17/17 + 4/4 |
| Go CT-01/02 | 两组实际实现各 19 例通过 |
| Go 静态资源/缓存/迁移/详情测试 | 默认与 embedui 均通过 |
| Go vet | 默认与 embedui 均通过 |
| 干净源码与平台构建 | 默认不依赖 frontend/web_dist；缺资源 embedui 正确失败；Windows/Linux amd64 交叉构建通过，Linux 未运行 |
| 安全 Go 全包 | 未全绿，三项失败见下；排除危险 TestRedisBehavior，并未记为通过 |
| 冻结基线 | web/、web_test/ 无差异；legacy.css 与原 style.css SHA256 同为 `2F8FB7F756C0BC96BBB856E4CB76CF19E06CDDA762F4F48F5CD5D597F0A73DF3` |

真实浏览器只操作拥有的临时 SQLite、独立 Redis 和本地媒体源。P4 场景涵盖：完整分区/非零剧照索引/缩略与原图链接；展开后重读、失败刷新保留 callbacks、推荐只请求一次；实体清除/库保留/刮削状态 toggle/推荐返回；卡片键盘与快捷播放；真实单条 ffprobe 写 NFO；删除索引保留源文件后重扫恢复；真实 Artplayer 直连与一次静音代理回退、焦点圈定/关闭恢复；图片 404 占位、推荐 5s 超时、双播放失败只报一次。

GL-03/05/09/14/15/23/25/26/28/30 沿用并复验 P2 的历史/分页/筛选；GL-06/07/08/10/12/17/18/24/27/29/32/33 由媒体组件、请求域、短写、图片和播放器单测/E2E 覆盖。GL-07/08 的刮削浮层部分及 GL-11/16/19/20/31 的探测/刮削任务部分仍待 P5，不标记全量 GL 验收完成。

安全 Go 全包失败：

1. `TestMediaProbe`（probe_test.go:135）：本机 ffprobe 输出缺 `<reframes>1</reframes>`，与 P0/P3 已记录差异相同。
2. `TestLibraryWatchEndToEnd`（library_watch_test.go:206）：删除媒体库返回 500，测试输出没有具体错误正文。
3. `TestLibraryPollingEndToEnd`（library_watch_test.go:321）：删除轮询库返回 500，`database is locked (5) (SQLITE_BUSY)`。

后两项组合单独 `-count=1` 复查通过（46.651s）；不能覆盖原全包失败。本轮 nfo、librarywatch 包及 ProbeCircuitBreaker 通过。未放宽断言、未修改后端来掩盖失败。完整失败输出保留 `artifacts/P4/go-regression.json`，单独复查 `go-watch-isolated.txt`。

生成证据不入库：完整报告 `artifacts/P4/playwright-report/`、结果 `browser-results/`，最终构建补验及截图 `focused-results/`，脱敏截图 `screenshot-results/`，首轮失败报告 `first-run-report/`，阶段汇总 `verification.json`。保留 P0/P2/P3 证据。手机验证为 390×844 浏览器视口模拟，未做真机或 Linux 运行验证。

## 可重复命令与接续点

```powershell
npm --prefix frontend run typecheck
npm --prefix frontend run test:unit -- --run
npm --prefix frontend run test:build
npm --prefix frontend run build
$env:PW_CHANNEL = 'msedge'
npm --prefix frontend run test:e2e
# 仅 P4 补验，可保留完整报告
npm --prefix frontend run test:e2e -- media-interactions.spec.ts --reporter=list --output=../vue-migration/artifacts/P4/focused-results
node --test internal/server/web_test/app.test.cjs vue-migration/tools/live-env/live-env.test.mjs
go test ./internal/scraper ./internal/server -run '^TestVueMigration(Number|Entity)Contract$' -count=1 -v
go test ./internal/server -run 'Test(WebUI|CachePolicyHeaders|VueMigration|AdminItemDetail)' -count=1
go test -tags embedui ./internal/server -run 'Test(WebUI|CachePolicyHeaders|VueMigration|AdminItemDetail)' -count=1
go vet ./...
go vet -tags embedui ./...
npm --prefix frontend run test:go-build
$env:EMBY_MIGRATION_PHASE = 'P4'
npm --prefix frontend run test:go-regression
Remove-Item Env:EMBY_MIGRATION_PHASE
go test ./internal/server -run '^TestLibrary(Watch|Polling)EndToEnd$' -count=1
```

不要直接执行 `go test ./...`：既有 TestRedisBehavior 会清理本机 6379 的 DB15。下一步为 P5/VUE-11/12/13：应用级探测/刮削任务、单条刮削预览确认及 CT-03、计划任务，继续保留冻结基线和隔离环境。
