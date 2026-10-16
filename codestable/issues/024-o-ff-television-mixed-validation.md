---
kind: issue
title: 剧集与混合媒体库验证收尾
type: ff
status: open
created: 2026-10-16
---

本地电视节目与混合库的新增、扫描、剧集 → 季 → 单集浏览及网页播放链路已完成隔离验证。在既有未提交实现上补齐实际季号、类型筛选、详情返回恢复、异常链接和翻页重试；保持 HTTP/HTTPS STRM 播放边界，不修改生产配置、数据库或 Redis。保留 open，真实媒体与客户端验收尚未完成；本轮未提交、推送或部署。

- 改动：`internal/server/television.go`、`library.go` 修正特别篇及第 2 / 10 季名称、NextUp 类型筛选；`internal/store/store.go`、`television.go` 使 Video 包含影片和单集；定向 store/server 测试覆盖季集排序、图片、空库、非法父级/用户、播放进度及失效 STRM 修复。
- 改动：`TelevisionBrowser.vue`、`api/television.ts`、`MediaWallPage.vue`、`legacy-url.ts` 把剧集与季选择写入路由，恢复详情返回和直接访问，首次优先正片季，校验类型/ID 与跨库链接；翻页失败保留已加载内容并从原偏移重试；媒体墙窄屏标题局部换行。
- 夹具：`seed-television.mjs` 与 `preview-local.mjs --television` 仅在隔离临时目录生成两部剧集（季 0 / 1 / 2 / 10，各两集）、一部影片与空目录。README 补充类型、目录约定与复现入口，指纹仍为当前代码的 `nfo-v3`。
- 自动验证：剧集前端单测 9/9；前端类型检查、生产构建、`go vet ./...` 通过。`go test ./... -skip '^Test(RedisBehavior|MediaProbe|ProbeCircuitBreaker)$' -count=1 -timeout 180s` 全包通过；RedisBehavior 会操作本机 DB15，未运行。MediaProbe 的 reframes 断言、ProbeCircuitBreaker 超时在此前全量运行失败，排除不代表通过。
- 前端全量：234/235 通过；唯一失败为已有 `baseline.test.ts` CSS 原始字节比较，原文件与拷贝分别 37284 / 37285 字节，规范化 CRLF 后相同，未修改该基线。剧集定向 Go 测试另单独通过。
- 浏览器：真实 Chrome + 独占 Go/Redis/本机 0.5 秒 MP4。新增电视节目和混合库后扫描；剧集/季海报实际加载，季 0 / 1 / 2 / 10 和单集按编号排序；混合默认一部剧集加一部影片，平铺索引 9 项；第 2 / 10 季详情返回与刷新恢复；390×844 无横向溢出。临时 STRM 改 FTP 后保留详情并隐藏播放，恢复 HTTP 后免扫描成功解码播放至结尾（readyState=4、videoWidth=320、无媒体错误）。
- 证据：本机忽略目录 `output/playwright/television-season10-desktop.png`、`television-season10-mobile.png`。合成夹具与网页播放不等同于真实 Emby 客户端、生产媒体、SMB/NFS、Linux 或竞态验收；这些仍未验证。
- codestable：当前无关联 spec/epic；README 已同步当前能力和边界。本轮不扩展未承诺的高级客户端查询参数，也不改动漫迁移记录。

### 2026-10-16 Emby 接口复测

- 稳定复现 `TestTelevisionEmbyClientContractRouteVariantsAndPlayback` 的 8 组失败：公共续播 SQL 未排除 `played=1`，已看单集仍在继续观看中。`ResumedScoped` 在 COUNT 和分页前统一排除已看，保留播放进度、次数和隐藏状态；影片、普通剧集及全局续播采用一致规则，动漫显式库原有规则保持一致。
- 新增 `internal/store/resume_test.go`，覆盖全局/指定库、Movie/Episode/Video、分页计数、隐藏/无进度排除、取消已看后恢复及历史保留；修复前 7 个子例失败、修复后通过。扩展 `television_test.go` 的电视节目/混合库 × 普通/emby 前缀 × 原大小写/小写 8 组 HTTP 契约测试，覆盖全局续播、已看/未看、隐藏/恢复及下一集推进。`server_test.go` 先断言已看影片不在续播，再取消已看验证隐藏行为。
- 通过：剧集扫描/NFO/store/server 定向测试；剧集与管理端前端测试 19/19；前端类型检查及生产构建；`go vet ./...`。`go test ./internal/server -skip '^TestMediaProbe$' -count=1 -timeout 180s` 通过，耗时 93.427 秒。
- 全量 Go 首次复跑仅跳过会操作本机 Redis DB15 的 `TestRedisBehavior`：`TestMediaProbe` 的 `<reframes>1</reframes>` 断言仍失败；`TestPollingMonitorDebouncesRetriesAndTracksMoves` 出现一次聚合事件断言失败，单独 `-count=3` 重跑通过，仍记录为偶发风险；`TestCoreAPI` 的旧续播预期随本次规则校正后通过。其余包通过，未将排除项视为通过。
- 前端全量 234/235 通过；唯一失败仍为 CSS 冻结文件的字节比较。源文件 37284 字节、拷贝 37285 字节，统一 CRLF/LF 后内容相同；本轮未修改样式基线。
- 本机忽略日志：`output/television-go-regression.log`、`television-server-regression.log`、`television-frontend-regression.log`、`television-frontend-build.log`。测试使用临时目录和 SQLite/内存缓存；未操作现有业务库或 Redis。本轮为服务端 HTTP 契约验证，未做真实 Emby 客户端、生产媒体、SMB/NFS 或 Linux 验收；测试阶段未提交、推送或部署，事项继续 open，当前无 spec/epic 需同步。
- 交付：用户随后授权提交和推送本批剧集/混合库实现、续播修复及测试记录；`.gitignore` 补充忽略前端目录中的 Playwright 临时快照。真实客户端验收边界与上述已知失败保留，本次不关闭事项、不部署。
