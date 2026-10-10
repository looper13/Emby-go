# P6 全量回归与浏览器验证（已完成）

2026-10-10，接续 P5，完成 VUE-14。Vue 入口仍为 `/admin-vue`，正式 `/admin` 仍是旧版；未提交、未部署、未切换入口。旧 `internal/server/web/`、`web_test/` 与 `legacy.css` 继续冻结。

- [x] VUE-14：既有 17 场景落点核对、逐页功能对照、旧地址、401、网络异常、畸形/空响应与任务生命周期，并在真实 Go 服务下跑桌面/手机主链路。
- [x] 输出用例结果、截图、关键请求计数、Go 集成结果与待解决项；mock 通过与真实服务通过分别记录。

覆盖对照全文见 [coverage-matrix.md](coverage-matrix.md)（17 项旧回归 → 新落点、GL-01..GL-35 全量、§9.2 新场景、20 项补测清单、C1–C4 有意变更、未覆盖项）。

## 审计结论

| 清单 | 结论 |
|---|---|
| 既有 17 项回归 | 17/17 有新的单元/组件或真实浏览器落点；不再读取旧 `app.js` |
| GL-01..GL-35 | 全部有自动断言；GL-02/GL-09 另有代码锚点（见开放项） |
| 计划 §9.2 新场景 | 除 P7 项（升级 chunk、回滚二进制）外全部覆盖 |
| test-mapping §3 补测清单 | 20/20 有落点 |
| 有意变更 C1–C4 | 网络失败≠401、并发 401 合并、轮询异常≠完成均有用例 |

## 本轮补齐（8 项新单测 + 1 项 E2E 断言）

| 证据 | 断言 |
|---|---|
| `U tests/overlays.test.ts`（新文件，4 项） | 播放器 Escape 关闭并阻断页面级 back 监听；刮削浮层在下层播放器之上时不消费 Escape；播放器与刮削浮层都只由遮罩自身点击关闭，内容区点击不关闭且不写盘 |
| `U tests/media-interactions.test.ts`「Escape returns from detail unless the user is typing in a control」 | 详情 Escape 调用 `backFromItem`；焦点在输入控件内时一次也不触发 |
| `U tests/media-interactions.test.ts`「recommends within a 5 second budget and hides the section when it times out」 | 推荐适配器确实传 `timeoutMs: 5000`；超时只隐藏推荐分区，详情主体仍在 |
| `U tests/auth.test.ts`「router fallbacks」（2 项） | 未知路径经真实 Router + 守卫落到 `/overview` 且渲染控制台外壳；`/item/0` 被 `beforeEnter` 挡回 `/items` 并发出媒体墙请求 |
| `E core-path.spec.ts` 300/250 | 新增 `document.title` 保持 `Emby-go · 控制台`（GL-22：只改 h1） |

审计中同时修正测试夹具：`auth.test.ts` 的 `openApp` 未按 `main.ts` 的装配注入 view-history，媒体墙页面因此拿到 `undefined` 并在 setup 抛错。补齐 `provide` 后 App 级测试可以渲染媒体墙。这是夹具与生产装配不一致，**不是产品缺陷**，页面代码未改动。

## 验证结果

| 验证 | 结果 |
|---|---|
| TypeScript 严格检查 | 通过 |
| Vitest 单元/组件 | 189/189（13 文件；P6 新增 1 文件、8 项） |
| 真实 embedui Playwright | 58/58（desktop 29 + mobile 29），5.4 分钟；18 个结果目录、30 张桌面/手机截图、无失败 trace |
| Vite/Go 发布资源 | 通过；产物与 P5 完全相同（`index-QSi2F-iQ.js` 231.48 kB / gzip 82.14，Artplayer chunk 133.43 kB，CSS 29.12 kB），证明本轮只改测试 |
| Node 构建脚本 | 11/11 |
| Go vet | 默认与 embedui 均通过 |
| Go 定向 | `-tags embedui ./internal/server -run 'Test(WebUI|CachePolicyHeaders)'` 通过；CT-01/02/03 契约（含写契约 8 子例、preview/root 取消）通过 |
| 干净源码与平台构建 | 默认不依赖 frontend/web_dist；缺资源 embedui 仍被拒绝；Windows/Linux amd64 交叉构建通过（Linux 未运行） |
| 安全 Go 全包 | 14 包通过、1 跳过、1 失败：仅既有 `TestMediaProbe`（ffprobe `<reframes>` 差异）。P5 出现的 `TestProbeCircuitBreaker` 负载抖动本轮未复现 |
| 冻结基线 | `web/`、`web_test/` 在 P6 无写入；`style.css`/`legacy.css` SHA256 仍为 `2F8FB7F756C0BC96BBB856E4CB76CF19E06CDDA762F4F48F5CD5D597F0A73DF3` |

mock 与真实服务分别记录：单测使用夹具与 mock 传输；58 项浏览器用例全部运行真实 `-tags embedui` Go 二进制 + 临时 SQLite + 独立 Redis + 本地媒体源与刮削桩服务，未连接开发者的 `config.yaml`、真实媒体库或在线刮削服务。

## 关键请求计数与行为证据（真实服务）

| 场景 | 证据 |
|---|---|
| 300/250 历史与滚动 | 初始 3 次分页请求（100/200/300 条）；进入详情、`#item-back` 返回、`goForward` 后请求数与集合不变（0 次重复/返回分页）；第 250 张卡焦点与滚动位置差 <3px；`history.scrollRestoration === 'manual'` |
| 筛选与防抖 | 离散筛选 push 使 `history.length` +1；搜索 replace 不增长；离开页面后再等 500ms 无迟到 replace |
| 卡片动作边界 | 嵌套按钮 Enter 不导航（仍在 `#/items`）；真实单条探测写入 `<streamdetails>`；删索引保留源文件并回到 0 卡 |
| 刮削写入 | CT-03 双击确认只发出 1 次写入请求；取消预览 0 次写入请求且磁盘不变；头像任务只写自有媒体并回填 `<thumb>` |
| 任务生命周期 | 扫描/重建期间跨路由按钮保持禁用，完成后任务表 +1 行；批量探测/刮削 reload 后恢复到同一 `started_at`；服务重启后任务定义保留但后台任务不续跑 |
| 入口与协议边界 | 嵌入资源 MIME/Cache-Control/HEAD 一致，缺失 chunk 与 build-info 返回 404，`/api` 未授权返回 JSON 401，旧入口仍加载 `/web/app.js` |

## 开放项与未运行项

1. **浏览器刷新取消手动扫描**：只有 Go 级证据 `TestCancelledScanTaskStatus`（请求 context 取消 → 499 + `cancelled` + 释放运行态）。未加浏览器层用例：需要人造慢扫描，时间敏感；P7 可在真实服务器手工确认一次。
2. **回滚二进制可重新登录浏览、Linux systemd 冒烟、五平台发布产物**：属 P7/VUE-16。
3. **资源泄漏计数**（切页 50 次、播放器开关 20 次、监听器/定时器回到基线）：属 P7 §9.4 必做检查；当前只有单测断言定时器归零。
4. **GL-09 滚动监听清理**：代码锚点 `MediaWallPage.vue:105-106`，无独立断言，归 P7 泄漏检查。
5. **真机与 Linux 运行**：手机为 390×844 视口模拟；Linux 仅交叉编译，未在目标平台运行。

## 可重复命令与接续点

```powershell
npm --prefix frontend run typecheck
npm --prefix frontend run test:unit -- --run
npm --prefix frontend run test:build
npm --prefix frontend run build
$env:PW_CHANNEL = 'msedge'
npm --prefix frontend run test:e2e -- --output=../vue-migration/artifacts/P6/full-suite
go vet ./... ; go vet -tags embedui ./...
go test -tags embedui ./internal/server -run 'Test(WebUI|CachePolicyHeaders)' -count=1
go test ./internal/scraper ./internal/server -run '^TestVueMigration(Number|Entity|Overwrite|PreviewAndRootCancellation)Contract$' -count=1
npm --prefix frontend run test:go-build
$env:EMBY_MIGRATION_PHASE = 'P6'
npm --prefix frontend run test:go-regression
Remove-Item Env:EMBY_MIGRATION_PHASE
```

不要直接执行 `go test ./...`：既有 `TestRedisBehavior` 会清理本机 6379 的 DB15。浏览器与回归命令必须在 PowerShell 中执行并显式设置环境变量。

生成证据不入库：浏览器结果与截图 `artifacts/P6/full-suite/`、安全 Go 全包报告 `artifacts/P6/go-regression.json`；机器可读汇总 `artifacts/P6/verification.json`。

下一步为 P7/VUE-15/16：必做性能与泄漏检查（请求计数、返回恢复、图片尺寸、明显卡顿、切页泄漏、升级后 chunk 失败）、正式入口切换、五平台 embedui 构建、Linux 冒烟与回滚演练、文档收尾与旧 UI 移除。