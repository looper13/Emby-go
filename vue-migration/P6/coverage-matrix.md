# P6 覆盖对照矩阵（VUE-14）

P6/VUE-14 产物。核对 [P0/test-mapping.md](../P0/test-mapping.md)、[P0/global-behaviors.md](../P0/global-behaviors.md) 与计划 §9.2，把每条既有行为/新场景对到**当前实际存在**的测试或代码锚点，并标出仍未覆盖的项。

证据层级：

- **U** = Vitest 单元/组件测试（`frontend/tests/`）
- **E** = 真实 embedui Playwright（`frontend/e2e/`，桌面 1280×800 + 手机 390×844）
- **G** = Go 测试（`internal/**/*_test.go`）
- **C** = 仅代码锚点（读代码可证，无自动断言；在 §6 说明原因）

P6 新增证据：`U overlays.test.ts`（4 项：Escape 顶层顺序、遮罩关闭）、`U media-interactions.test.ts`「Escape returns from detail unless the user is typing…」与「recommends within a 5 second budget…」、`U auth.test.ts`「router fallbacks」2 项、`E core-path.spec.ts` 300/250 增加 `document.title` 不变断言。

## 1. 既有 17 项回归 → 新落点

| # | 旧断言 | 新证据 | 层级 |
|---|---|---|---|
| 1 | 迟到详情不覆盖新页标题/内容 | `U media-interactions.test.ts「late short writes and recommendations cannot refresh or replace another detail」`；`E core-path.spec.ts「late detail and late pagination cannot replace newer pages or filters」` | U+E |
| 2 | 迟到普通页响应丢弃 | `U media-wall.test.ts「ignores late results and errors even if the transport ignores cancellation」` | U |
| 3 | 导航取消只读，不取消写/进度轮询 | `U client.test.ts「cancels explicit page reads without canceling writes or progress」` | U |
| 4 | 推荐并行不阻塞，切页后不写回 | `U media-interactions.test.ts「recommendations do not block detail; reread preserves…」` | U |
| 5 | 推荐超时并释放定时器 | `U client.test.ts「only times out when requested and cleans up timeout timers」`；`U media-interactions.test.ts「recommends within a 5 second budget and hides the section when it times out」`（断言 `timeoutMs: 5000` 与超时只隐藏分区） | U |
| 6 | 失败停止自动重试，允许一次显式重试 | `U media-wall.test.ts「locks duplicate loads and stops after failure until explicit retry」` | U |
| 7 | 300 条恢复；失效按目标条数重载 | `U media-wall.test.ts「restores 300 entries without pagination, but dirty/expired/failed snapshots reload」`；`E core-path.spec.ts「300/250 history restores focus and position without duplicate or return pagination」` | U+E |
| 8 | 旧分页不追加到新筛选 | `U media-wall.test.ts「ignores late results…」`；`E core-path.spec.ts` 迟到分页用例 | U+E |
| 9 | 嵌套按钮键盘不激活卡片 | `U media-components.test.ts「activates cards by click, Enter and Space while excluding nested action keys」`；`E media-interactions.spec.ts「card actions are separate from navigation…」`（Enter 重读后仍在媒体墙） | U+E |
| 10 | 刷新保留剧情/文件展开与阅读位置 | `U media-interactions.test.ts「recommendations do not block detail; reread preserves expanded state…」`；`E media-interactions.spec.ts` 完整详情用例 | U+E |
| 11 | 刷新失败保留旧页面可重试 | 同上；`U auth.test.ts「keeps the previous overview after a failed refresh and does not automatically retry」` | U |
| 12 | 刷新中切页取消新旧生命周期 | `U media-wall.test.ts「keeps a partially loaded snapshot from publishing after leaving the page or logout」`；`U media-interactions.test.ts「late short writes…」` | U |
| 13 | 缩略图带版本参数，原图链接不带 | `U media-components.test.ts「retains versioned thumbnail parameter order and original image links」`；`U media-interactions.test.ts「keeps trailer and stills separate with versioned nonzero indices and original links」` | U |
| 14 | 坏图一次回退再占位 | `U media-components.test.ts「falls back once, then preserves a placeholder and resets for a new source」`；`E media-interactions.spec.ts「broken artwork becomes a placeholder…」` | U+E |
| 15 | 预告片与剧照独立、必要时封面回退 | `U media-interactions.test.ts「keeps trailer and stills separate…」` | U |
| 16 | 播放错误四类分类 | `U media-interactions.test.ts「reports trailer errors directly and classifies network/decode/format/mixed content」` | U |
| 17 | 单次代理回退、销毁后回调失效 | `U media-interactions.test.ts「falls back once, mutes asynchronous playback and ignores callbacks after destroy」`；`E media-interactions.spec.ts「real Artplayer plays, closes…falls back to muted proxy」` | U+E |

## 2. GL-01..GL-35 全量对照

| 编号 | 证据 |
|---|---|
| GL-01 登录校验与失效 | `U auth.test.ts「shares an in-flight session check and caches the verified session」`/「retains token on a failed network check…」/「removes a token only on an explicit protected 401」；`E bootstrap.spec.ts「failed login keeps input and invalid token returns to login」` |
| GL-02 `scrollRestoration='manual'` | `C src/main.ts` 单点设置；`E core-path.spec.ts` 300/250 断言 `history.scrollRestoration === 'manual'` |
| GL-03 历史条目元数据 | `U media-components.test.ts「preserves Router-owned history fields in a separate validated namespace」`；`E core-path.spec.ts` 300/250 焦点/条数/位置恢复 |
| GL-04 唯一导航入口、无双重渲染 | `E core-path.spec.ts` 300/250 与 core-path「discrete filters push…」断言请求集合无重复、`history.length` 增量符合 push/replace |
| GL-05 popstate 恢复 | `E core-path.spec.ts` 300/250（goBack/goForward 恢复焦点与位置且不再请求）；`E core-path.spec.ts「old outer query, direct details and reload return stay inside the console」` |
| GL-06 标题/面包屑/导航高亮 | `E core-path.spec.ts` 300/250 断言 `#page-title` 与 `[data-page=items][aria-current=page]` |
| GL-07 Escape 优先级链 | `U overlays.test.ts`（播放器 Escape 不触达页面 back；刮削浮层存在时播放器不消费 Escape）；`U media-interactions.test.ts「Escape returns from detail unless the user is typing in a control」`；`E p5-workflows.spec.ts` 预览 Escape 恢复焦点；`E core-path.spec.ts` 300/250 详情 Escape 回墙 |
| GL-08 ✕/遮罩关闭 | `U overlays.test.ts`（播放器与刮削均只由遮罩自身点击关闭，内容区点击不关）；`E media-interactions.spec.ts` 播放器 `#player-close` |
| GL-09 全局 scroll 补页与清理 | `C MediaWallPage.vue:105-106` 订阅与 `onScopeDispose` 移除；`E core-path.spec.ts` 300/250 与 `E admin-pages.spec.ts` 扫描用例中滚动补页到 300 条；监听器计数残留检查留 P7（§9.4） |
| GL-10 图片一次回退 | `U media-components.test.ts` 坏图用例；`E media-interactions.spec.ts` 坏图占位 |
| GL-11 boot 恢复 scan/probe 进度 | `U tasks.test.ts「does not show a previously finished scan when logging in」`；`U p5-tasks.test.ts「does not announce a historical finished job as new completion on login」`；`E p5-workflows.spec.ts「batch probe restores after reload…」` |
| GL-12 切页取消读取、关闭浮层、刷新保留 UI | `U client.test.ts「cancels explicit page reads…」`；`U media-wall.test.ts` 离开后不发布半成品快照；`E p5-workflows.spec.ts` 切路由关闭预览且零写入 |
| GL-13 401 清 token、写成功置 dirty | `U client.test.ts「deduplicates concurrent 401 responses for the current token」`/「never invalidates a newer token on a delayed old-token 401」；`U media-wall.test.ts「invalidates on successful short writes including 204…」`；`E core-path.spec.ts「network failure preserves the token; concurrent 401 restores the original target after login」` |
| GL-14 离散 push / 搜索 replace / 防抖清理 | `U media-interactions.test.ts「uses select for nine libraries…」`；`E core-path.spec.ts「discrete filters push, search replaces, and leaving cancels its debounce」` |
| GL-15 `backFromItem` | `U media-interactions.test.ts` Escape 用例（注入 `backFromItem` 断言调用）；`E core-path.spec.ts` 直开详情与 300/250 的 `#item-back` |
| GL-16 toast 与定时器生命周期 | `U media-components.test.ts「animates notification expiry and clears timers on disposal」`；`U tasks.test.ts`/`U p5-tasks.test.ts` 定时器归零断言 |
| GL-17 刷新失败回滚保留旧页 | `U auth.test.ts` 总览失败刷新；`U media-interactions.test.ts` 重读失败保留展开与回退按钮；`E media-interactions.spec.ts` 完整详情用例 |
| GL-18 `embyJSON` 超时与降级 | `U media-interactions.test.ts` 5s 预算/超时隐藏分区；`U media-interactions.test.ts「recommendations do not block detail…」` |
| GL-19 `runScan` 全流程 | `U tasks.test.ts`（7 项：按钮/文案/toast/结束刷新/409/取消）；`E admin-pages.spec.ts「a scan request stays alive across navigation…」`；`G TestCancelledScanTaskStatus`（请求 context 取消 → 499 + cancelled 任务） |
| GL-20 结束态恢复与面板自动隐藏 | `U tasks.test.ts「preserves cancelled completion and resets POST, progress, late callbacks and timers on logout/disposal」`（4000ms 后隐藏）、`U p5-tasks.test.ts`（探测 8s 隐藏与失败保留） |
| GL-21 计划任务编辑态 | `U p5-scheduled.test.ts「editing selection persists when a page leaves and returns, cancellation clears it」`；`E p5-workflows.spec.ts` 计划任务用例（跨路由保留） |
| GL-22 标签页标题不变 | `E core-path.spec.ts` 300/250 新增 `expect(await page.title()).toBe('Emby-go · 控制台')`，同时 `#page-title` 变为影片名 |
| GL-23 正常进入回顶部、历史返回才恢复 | `E core-path.spec.ts` 300/250（位置恢复）与 core-path 直开详情/返回用例 |
| GL-24 卡片键盘边界 | `U media-components.test.ts`；`E media-interactions.spec.ts` 卡片用例（Enter 于嵌套按钮不导航） |
| GL-25 快照复用条件 | `U media-wall.test.ts「restores 300 entries without pagination, but dirty/expired/failed snapshots reload」`；`E core-path.spec.ts` 300/250（0 次重复分页） |
| GL-26 无效 `library_id` 清洗 | `U legacy-url.test.ts「drops invalid numeric filter IDs while preserving other filters」`；`E core-path.spec.ts「invalid library is removed without losing other query parameters」` |
| GL-27 迟到隔离双保险 | `U usePageRequest` 相关用例（`U media-wall.test.ts`/`U media-interactions.test.ts` 迟到用例）；`E core-path.spec.ts` 迟到详情/分页 |
| GL-28 进详情 push + 返回 | `E core-path.spec.ts` 300/250（Enter 进详情、`#item-back` 回墙并恢复焦点） |
| GL-29 长操作按钮忙碌态 | `E admin-pages.spec.ts` 扫描用例（`#scan`/`#task-scan` 禁用恢复）；`E p5-workflows.spec.ts` 探测按钮禁用/恢复；`U useMediaActions` 锁（`U media-interactions.test.ts`） |
| GL-30 媒体墙请求代次 | `U media-wall.test.ts「ignores late results and errors…」`；`E core-path.spec.ts` 迟到分页 |
| GL-31 任务面板跨页面常驻 | `E p5-workflows.spec.ts`（切到总览按钮仍禁用、`page.goBack`+`reload` 后进度仍在）；`U p5-tasks.test.ts`（页面卸载不重置 lifetime） |
| GL-32 详情刷新保留展开 | `U media-interactions.test.ts` 重读用例（`aria-expanded`、`details[open]`、推荐不重取）；`E media-interactions.spec.ts` 完整详情用例 |
| GL-33 图片 URL 形状 | `U media-components.test.ts`；`U media-interactions.test.ts` 剧照索引/原图链接；`U media-interactions.test.ts「consumes real DTO sections…」`（`imageURL` 参数顺序） |
| GL-34 登录接口形状 | `U auth.test.ts「keeps a successful credential login inside %s」`（body `{Username,Pw}`、无 token 头）；`U client.test.ts「sends public credentials without an existing token…」` |
| GL-35 两条成功路径统一目标解析 | `U auth.test.ts` 的 `/admin-vue`、`/admin` 两入口 it.each；`U legacy-url.test.ts「converts a legacy login returnTo only when it is an internal allowed route」`/「rejects external or invalid returnTo」 |

## 3. 计划 §9.2 新场景

| 场景组 | 证据 | 状态 |
|---|---|---|
| 初始化/已有 token/失效 token/认证网络异常/并发 401/登录恢复合法目的页 | `U auth.test.ts`（19 项源级用例，含 2 个 `it.each`）、`U client.test.ts` 401 用例、`E bootstrap.spec.ts`(3)、`E core-path.spec.ts` 初始化与 401 用例 | ✅ |
| 新旧地址、中文查询、前进/后退、直开详情、被删除的库、非法 ID、未知路由 | `U legacy-url.test.ts`（8 项源级用例，含 2 个 `it.each`）、新增 `U auth.test.ts「router fallbacks」`（未知路径→总览、`/item/0`→媒体墙）、`E core-path.spec.ts`(7) | ✅ |
| 嵌入资源 MIME/缓存/HEAD、缺失 chunk 404、未知 Emby API 原协议错误 | `E bootstrap.spec.ts「embedded assets, old entries and protocol errors remain separate」`；`G` web_ui/cache policy 测试 | ✅ |
| 扫描中切路由继续、标签刷新取消手扫、后台探测/刮削刷新恢复、无重叠轮询 | `E admin-pages.spec.ts` 扫描用例（切路由继续、按钮锁定）；`E p5-workflows.spec.ts`（批量探测/刮削 reload 恢复、互斥 409）；`G TestCancelledScanTaskStatus`（请求 context 取消）；`U p5-tasks.test.ts`（无重叠轮询、退避） | ✅（浏览器刷新取消扫描为 G 级证据，见 §6） |
| 204、非 JSON 错误、FormData、取消、重复提交、失败后输入保留 | `U client.test.ts`（14 项）、`U admin.test.ts`/`U auth.test.ts`/`U p5-*.test.ts` 各表单用例 | ✅ |
| 批量任务占用/部分失败、取消预览无写入、确认后保持详情、cron 后端校验 | `E p5-workflows.spec.ts`（5 项）；`U p5-scrape.test.ts`/`U p5-scheduled.test.ts` | ✅ |
| 升级旧 chunk 缺失提示；回滚二进制可登录浏览 | `U auth.test.ts「prompts on preload error without losing input and unregisters the event on unmount」`；回滚演练属 P7 | 部分（P7 收口） |
| GL 编号全量对照；CT-01/02/03；登录两条路径 | 本文件 §2；`U media-interactions.test.ts` CT-01/02 各 19 例；`G TestVueMigration(Number|Entity|Overwrite|PreviewAndRootCancellation)Contract`；`E p5-workflows.spec.ts` CT-03 真实写入 | ✅ |

## 4. 「现有行为但无测试」补测清单（20 项）

| # | 项 | 证据 |
|---|---|---|
| 1 | 登录/初始化全流程 | `U auth.test.ts`；`E bootstrap.spec.ts`、`E core-path.spec.ts` |
| 2 | 401 与并发合并、网络失败区分 | `U auth.test.ts`、`U client.test.ts`、`E core-path.spec.ts` |
| 3 | `runScan` 全流程 | `U tasks.test.ts`；`E admin-pages.spec.ts` 扫描用例 |
| 4 | 批量探测 8s 隐藏/失败保留；单条探测文案 | `U p5-tasks.test.ts`；`E media-interactions.spec.ts` 卡片探测（toast 与 NFO）、`E p5-workflows.spec.ts` 批量探测 |
| 5 | 刮削配置/测试/候选/run payload/取消/轮询恢复 | `U p5-scrape.test.ts`；`E p5-workflows.spec.ts`(3 项) |
| 6 | 单条预览候选/覆盖/确认/取消/迟到 | `U p5-scrape.test.ts`(4 项)；`E p5-workflows.spec.ts`（预览取消、CT-03 双击只写一次） |
| 7 | 计划任务 cron/编辑态/类型显隐/动作 | `U p5-scheduled.test.ts`(8)；`E p5-workflows.spec.ts`(2) |
| 8 | 手动补录字段与失败保留 | `U admin.test.ts`；`E admin-pages.spec.ts` 手动用例 |
| 9 | API 密钥创建/删除/复制失败 | `U admin.test.ts`；`E admin-pages.spec.ts` 密钥用例 |
| 10 | 媒体库添加/删除语义 | `U admin.test.ts`；`E admin-pages.spec.ts` 媒体库用例 |
| 11 | 总览统计与重建流程 | `U auth.test.ts`（空库统计 4 卡）；`E admin-pages.spec.ts` 重建用例 |
| 12 | 接口探针清空 | `U admin.test.ts「clears probes with 204 and rereads once…」`；`E admin-pages.spec.ts` 探针用例 |
| 13 | 实体跳转 URL 形状 | `U media-interactions.test.ts`；`E media-interactions.spec.ts` 实体用例 |
| 14 | 无效 `library_id` 清洗 | `U legacy-url.test.ts`；`E core-path.spec.ts` |
| 15 | Esc 优先级链与遮罩判定 | `U overlays.test.ts`（新）、`U media-interactions.test.ts`（新）、`U p5-scrape.test.ts`；`E p5-workflows.spec.ts`/`E core-path.spec.ts` |
| 16 | toast 生命周期 | `U media-components.test.ts` |
| 17 | 未知路由/非法 item ID | `U auth.test.ts「router fallbacks」`（新）；`U legacy-url.test.ts` |
| 18 | 快捷播放与卡片点击分支 | `U media-components.test.ts`；`E media-interactions.spec.ts` 卡片用例 |
| 19 | 卡片行内 reread/delete/probe 与写后刷新 | `U media-interactions.test.ts`；`E media-interactions.spec.ts` 卡片用例（真实写 NFO、删索引保留源文件） |
| 20 | 写成功置 dirty → 返回媒体墙重载 | `U media-wall.test.ts`；`E admin-pages.spec.ts` 扫描后媒体墙用例 |

## 5. 有意变更（C1–C4）的对照记录

| 差异 | 新证据 |
|---|---|
| C1 网络失败 ≠ 401 | `U auth.test.ts「retains token on a failed network check and retries only when asked」`、`E core-path.spec.ts` 网络失败保留 token |
| C2 并发 401 合并为一次跳转 | `U auth.test.ts「redirects concurrent protected 401s once and permits re-login」`、`U client.test.ts` 401 去重 |
| C3 轮询连接异常不判为结束 | `U tasks.test.ts「keeps the last running progress after a network error, backs off and does not overlap reads」`、`U p5-tasks.test.ts` 同语义与「service reset cannot be announced as a successful completion」 |
| C4 刮削轮询异常同 C3 | 同上（`useBackgroundTask` 共用退避与 `unavailable`） |

## 6. 未覆盖与开放项

1. **浏览器刷新取消手动扫描**：语义由 `G TestCancelledScanTaskStatus`（请求 context 取消 → 499 + `cancelled` + 释放运行态）覆盖；未加浏览器层用例，因为需要人造慢扫描，时间敏感易抖动。P7 若做真机/发布冒烟，可在真实服务器上手工确认一次。
2. **回滚二进制可登录浏览**：属 P7 发布演练（保存上一版二进制 + Linux systemd 冒烟）。
3. **资源泄漏计数（切页 50 次 / 播放器 20 次、监听器与定时器归零）**：属 P7 §9.4 必做检查；当前仅在单测中断言定时器归零。
4. **Linux 运行验证**：仅交叉编译，未在 Linux 运行（P7）。
5. **真机验证**：手机为 390×844 视口模拟，未使用真实手机设备（计划允许，P7 记录为未运行项）。
6. **GL-09 监听器清理**：代码锚点在 `MediaWallPage.vue:105-106`，无独立断言，归 P7 泄漏检查。