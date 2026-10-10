# 页面 → 组件 / 状态 / 事件拆分清单（P0 冻结基线）

P0/VUE-01 产物。细化并冻结计划 §3.5。P3–P5 按此清单验收；实施中需改边界时先更新本文件并说明原因。
原则：页面负责业务编排与页面请求；子组件以 props 接收展示数据、以 emit 表达用户意图；请求只放在拥有独立生命周期的页面、store 或对话框模块内。没有独立状态/复用/生命周期的标题、表格行不拆组件。

P3 实施补充（2026-10-10）：按末尾 A2 采用旧版浏览器原生 `window.confirm`，媒体库/API 密钥删除不引入自定义弹层，文案和取消行为逐字验证。下表 ConfirmDialog 是原先候选边界，本轮由原生确认承担。TaskProgress 常驻 App，直接读取 tasks store，不发请求；当前只接扫描/重建，probe/scrape 的统一展示留 P5。SidebarNav/TopBar 暂留 ConsoleLayout 模板，ToastHost 暂留 App，均无独立业务状态；独立表单仍拆 LibraryForm/ManualForm。扫描/重建的应用生命周期提前在 P3 落地，以支持管理入口，不能借此把 P5 标为完成。

P4 实施补充（2026-10-10）：WallFilters/WallLoadState 已拆出，卡片/grid 以 emit 传出所有动作；Detail 分区按清单落地。FileDetails 只展示媒体文件/流；文件与时间单独 FileTimeline（持有原生 details 展开事件），因此保留“媒体信息 → 元数据 → 文件与时间”的旧顺序。MetadataRow 在 MetadataList/FileTimeline 复用。短写及按钮锁由 useMediaActions 承担，完成时核对原 route.fullPath；离开后不刷新后来页面。PlayerDialog 常驻 App 层，按 player store 显隐，每次打开拥有一个实例；路由变化/认证失效关闭。详情根网格和 item-sections 加 `grid-template-columns:minmax(0,1fr)`，限制推荐横向列表的固有宽度，避免撑出页面；原 CSS 不变，此布局修正单独记录并在桌面/手机检查。单条刮削预览及批量探测仍属 P5，P4 对应入口明确链接旧控制台。

P5 实施补充（2026-10-10）：ScrapePage/ScrapePreviewDialog/ScheduledPage/TasksPage/ProbePage 按清单落地。probe/scrape 进度面板改为新增的 BackgroundTaskProgress（扫描/重建仍用 TaskProgress），因为探测/刮削状态在 useBackgroundTask 中，取消、失败退避与自动隐藏策略与扫描不同；ScrapeRunPanel 复用该组件而不自建轮询。单条预览的 preview/inspect 读取归 stores/scrape-preview.ts，浮层常驻 App 层；计划任务编辑态改用 stores/scheduled.ts 的 editingId 以跨路由保留（GL-21）。

## 0. 现有渲染函数 → 新归属总表

| 现有实现（锚点） | 新归属 | 备注 |
|---|---|---|
| `boot`/导航/浮层/键盘（app.js:1983-2051） | App + ConsoleLayout + 路由守卫 + tasks store | GL-01..GL-16 逐条落地 |
| `page()`/`abortPage`/`refreshPage`（10-29, 1730-1792） | usePageRequest composable + 路由恢复模块 | GL-12/17/23 |
| `pageOverview`（166-212） | OverviewPage | |
| `pageLibraries`（215-261） | LibrariesPage + LibraryForm | |
| `pageItems`/wall*（264-604） | MediaWallPage + WallFilters/MediaGrid/MediaCard/WallLoadState + media-wall store | |
| `pageItem`（715-896） | ItemDetailPage + 8 个展示子组件 | |
| `pageManual`（990-1041） | ManualPage + ManualForm | |
| `pageSettings`（1044-1069） | SettingsPage（只读） | |
| `pageAPIKeys`（1072-1117） | ApiKeysPage + 共享 ConfirmDialog | |
| `pageScrape`（1179-1379） | ScrapePage + ScrapeSettingsForm/ScrapeRunPanel + tasks store | |
| `openScrapePreview` 系列（1382-1509） | ScrapePreviewDialog + CandidateList + ScrapeDiff | |
| `pageScheduled`（1511-1713） | ScheduledPage + ScheduledForm + ScheduledList | |
| `pageTasks`（1120-1151） | TasksPage | |
| `pageProbe`（1154-1175） | ProbePage | |
| 播放器（909-987） | PlayerDialog + 播放器控制模块 | |
| scan/probe 轮询与面板（1794-1981） | tasks store + TaskProgress 组件 | 单一轮询所有者 |
| `toast`（61-67） | ToastHost + ui store（或 composable） | |
| `imageURL`/`entityIdOf`/`numberUnlessInTitle`/`fmt*`/`esc`（31-133, 671-712, 899-904） | src/lib/ 纯函数模块（逐字迁移）+ 单测 | CT-01/02 |
| 图片 error 回退（109-122） | 图片组件（MediaImage） | GL-10 |

## 1. App / ConsoleLayout

| 子组件 | 状态与事件归属 | props → emit | 请求域 | 测试 |
|---|---|---|---|---|
| SidebarNav | 导航项静态；高亮由 route meta（item→items）；点击走 router.push（hash 不同才导航） | meta 由路由提供 | 无 | GL-04/06 |
| TopBar | `#scan` 按钮状态（disabled/文案）读 tasks store | props: scanning | 无（点击 → tasks.runScan） | GL-19 |
| ToastHost | 队列在 ui store；3.2s+260ms 清理 | props: toasts; 无 emit | 无 | GL-16 |
| TaskProgress | 展示组件；三种任务（scan/probe/scrape）共用，文案/明细行来自 tasks store；自动隐藏策略在 store | props: {kind, progress}; emit: retry?（旧版无重试，不加） | 轮询在 store | GL-11/16/20 |
| ConfirmDialog | 全局单例（app 层），promise 化调用 | props: {message,confirmText…}; emit: confirm/cancel | 无 | 文案逐字对照 |
| PlayerDialog / ScrapePreviewDialog | 由对应功能模块控制显隐，挂载在 layout 层 | 见 §10/§8 | 各自域 | GL-07/08 |

## 2. LoginPage（/login）

| 项 | 说明 |
|---|---|
| 子组件 | 登录/初始化同页条件分支，不拆两套 |
| 状态 | 页面本地：用户名/密码/确认密码、mode(initialize\|login)、error、busy |
| 请求 | `GET /api/auth/status`（进入时）、`POST /api/auth/initialize`、`POST /Users/AuthenticateByName` —— 经 auth store 封装 |
| 跳转 | 两条成功路径（初始化后登录、直接登录）与已有 token 预检统一走"目标解析"函数（GL-35）；迁移期落 `/admin-vue#/overview`，正式切换后 `/admin#/overview` |
| 校验 | 本地位数/一致性校验；初始化成功 → 切登录模式并提示 |
| 测试 | GL-01/34/35；新场景：无效 token、网络失败（不清 token）、初始化后登录 |

## 3. OverviewPage（/overview）

| 子组件 | 说明 |
|---|---|
| 统计卡/状态表 | 简单循环，留页面（不拆） |
| TaskProgress | 复用 §1（扫描/重建进行中的展示） |
| ConfirmDialog | 重建/扫描触发时的确认（如后续需要；当前旧版无确认，保持无） |

- 状态：页面本地（统计与状态）。重建按钮 disabled 由页面提交态控制。
- 请求：页面 GET `/libraries`、`/items?status=success`、`/status`（并行、可取消）；重建 `POST /reindex` 由 tasks store 发起（长请求、应用持有）。
- 事件：`reindex` 点击 → tasks.reindex() → 完成后页面失效重读自身数据。

## 4. LibrariesPage（/libraries）

| 子组件 | 状态与事件归属 | props → emit | 请求域 | 测试 |
|---|---|---|---|---|
| LibraryForm | 表单本地状态；失败保留输入；成功页面触发 reset | props: {submitting}; emit: submit({Name,Path}) | 页面执行 POST | 输入保留、重复提交锁 |
| 列表行内操作 | 页面处理 | 行按钮直接调页面函数 | 页面执行 | 删除确认文案逐字、语义（删索引不删文件） |

- 请求：页面 GET `/libraries`；POST `/libraries`；DELETE `/libraries/:id`；行"增量扫描" → tasks.runScan(id)。
- 删除确认：ConfirmDialog，文案 `删除该媒体库及其影片索引？不会删除磁盘文件，但该库影片的播放进度/收藏会一并清除。`

## 5. MediaWallPage（/items）—— 最高风险页

| 子组件 | props → emit | 本地状态 | 备注 |
|---|---|---|---|
| WallFilters | props: {query(来自 URL), libraries}；emit: `filter`(payload {key,value,toggle}，页面决定 push 还是 replace)、`search`(draft)、`entity-clear` | 搜索输入草稿（页面持有亦可） | 库≤8 pill / >8 select；状态 pill、刮削 pill、排序 select、实体 chip |
| MediaGrid | props: {items, userdata, imageTags}；emit: `open(id)`、`quickplay(id)`、`probe(id)`、`reread(id)`、`delete(id)` | 无 | 事件委托改为组件内绑定；key 用稳定业务 id |
| MediaCard | props: {item, ud, tags}；emit: 同上各动作 | 无 | 键盘 Enter/Space 判定"目标为卡片本身"（GL-24）；海报回退 data-fallback |
| WallLoadState | props: {loading, done, failed, total, loaded, retryable}；emit: `retry` | 无 | 哨兵文案/失败+重试逐字迁移 |

- **状态归属**：URL query 为筛选唯一权威（status/library_id/search/sort/order/实体/scrape）；media-wall store 持有 `items/userdata/imageTags/offset/total/done/failed/loading/restoring/queryKey/loadedAt/dirty/gen`；搜索草稿在页面/过滤器（防抖 400ms，离开时清 timer）；恢复元数据（scrollY/wallCount/focusID）存路由历史条目（GL-03）。
- **请求生命周期**：分页请求由 store 发起（单在途 + abort + 代次，GL-30）；`GET /libraries` 由页面进入时读取（用于校验 library_id、无效则清洗 URL，GL-26）；写入后的 `dirty` 失效由 api client 统一置位（GL-13）。
- **恢复流程**：popstate → store 若有有效快照（60s/queryKey/dirty/failed 条件，GL-25）直接渲染，否则按 `restore.wallCount` 循环加载；渲染后恢复 focusID 焦点与 scrollY（GL-23）；restoring 期间禁止哨兵补页。
- **测试**：17 项中的 3、6、7、8、9；新增：快切筛选代次、失败后单次重试、实体跳转 URL、无效库清洗、快照失效重载。

## 6. ItemDetailPage（/item/:id）

| 子组件 | props → emit | 备注 |
|---|---|---|
| ItemHero | props: {movie, playable, images/backdropUrl, coverUrl, status, collection, tags}；emit: `back`、`play`、`probe`、`scrape`、`reread`、`entity(tag)` | 含返回按钮、标题、信息行、徽章、动作排（含各按钮 disabled=提交中） |
| PlotSection | props: {plot, expanded}；emit: `toggle` | 折叠阈值 260 字符逐字迁移（GL-33 类纯函数） |
| TrailerSection | props: {trailerUrl, thumbUrl}；emit: `play` | 独立分区；缩略回退链 |
| ArtworkGallery | props: {backdrops(带 URL 生成结果)} | 缩略 480 / 原图链接 |
| ActorList | props: {actors}；emit: `entity(person,name)` | 头像 URL 由 API 模块生成（entityIdOf） |
| SimilarItems | props: {items, loaded}；emit: `open(id)` | 空结果 → 区块隐藏；迟到结果不渲染（页面守卫） |
| FileDetails | props: {files} | streamTable 可留页面或本组件内 |
| MetadataList | props: {movie, libraryName}；emit: `entity(key,value)` | 类型/标签/厂商/合集实体链接 |

- **状态归属**：页面持有详情/相似数据与 UI 状态（plotExpanded/filesExpanded/similar 已加载结果与标记）；相似请求 5s 超时（GL-18）；刷新保留经路由/页面状态，不用 HTML 字符串（旧版 detailUI.similarHTML 改为"已加载数据 + 标记"，见建议调整 §A4）。
- **请求生命周期**：详情 GET 与相似请求随页面取消（GL-27）；探测/重读/刮削为短写操作——不经组件卸载取消，完成后刷新详情（GL-29）。
- **实体跳转**：emit 冒泡到页面 → `router.push({path:'/items', query:{[key]:value}})`（清其他实体参数、保留 library_id，GL-15 邻接）。
- **测试**：17 项中的 1、4、5、10、11、12、13、14、15；CT-01 番号去重展示。

## 7. ManualPage（/manual）

| 子组件 | props → emit | 说明 |
|---|---|---|
| ManualForm | props: {submitting, libraryHint}；emit: `submit(payload)` | 表单本地状态；genres/tags/studios 逗号+全角逗号拆分；失败保留输入；成功后由页面触发重置（reset 信号或 key 重建） |

- 请求：页面 GET `/libraries`（提示归入库）+ POST `/items/manual`。
- 测试：字段拆分、year 数字、成功 reset、失败保留、重复提交锁。

## 8. SettingsPage（/settings）：只读表格留页面，不新增保存按钮。

## 9. ApiKeysPage（/apikeys）

| 子组件 | props → emit | 说明 |
|---|---|---|
| 创建表单/列表 | 留页面（简单） | 复制按钮（clipboard + 失败提示）、删除走 ConfirmDialog（文案 `删除该 API 密钥？使用它的客户端将立即失效。`） |
| ConfirmDialog | 复用全局 | |

- 请求：GET/POST `/apikeys`、DELETE `/apikeys/:key`（encodeURIComponent 编码在 API 层）。
- 测试：密钥编码、复制失败路径、删除语义。

## 10. ScrapePage（/scrape）

| 子组件 | props → emit | 说明 |
|---|---|---|
| ScrapeSettingsForm | props: {settings, saving}；emit: `save(form)`、`test(target)` | 本地草稿；token/api_key 留空=不修改语义保持；测试按钮各自 loading |
| ScrapeRunPanel | props: {libraries, candidateCount, running}；emit: `run(payload)`、`cancel`、`avatar`、`lib-change`、`only-missing-change` | 候选数由页面读取 |
| TaskProgress | 复用 §1（scrape 变体：成功/跳过/失败/当前/错误 + 失败样例列表） | |

- 状态职责：进度数据在 tasks store（单一轮询所有者，1500ms）；页面只读展示。
- 请求：页面 GET settings/libraries/candidates；PUT settings；POST test/run/avatars/cancel；run/avatar 提交成功 → tasks store 启动轮询。
- 测试：保存语义（留空不覆盖）、测试结果两种分支、run payload（library_id Number||0）、取消不误报成功、轮询恢复。

## 11. ScrapePreviewDialog（Dialog 域，app 级挂载）

| 子组件 | props → emit | 说明 |
|---|---|---|
| CandidateList | props: {candidates, active:{provider,id}, query, expected}；emit: `select(provider,id)` | 番号去重展示用 numberUnlessInTitle |
| ScrapeDiff | props: {fields} | old/next/change/（不写入） |
| 图片网格/底部操作 | 留对话框 | 覆盖开关 + 取消 + 确认写入（确认 disabled 防重复） |

- 状态：对话框域持有 {movieId, preview, inspect, provider, id, loading}；关闭即取消未完成读取并抛弃迟到结果；跨页/换片失效校验（GL-27 类似）。
- 请求：`GET preview`、`POST inspect`、`POST confirm`（确认才写）；取消零请求。
- 事件：`confirmed` → 页面刷新详情；`close`。
- 测试：切换候选、取消不写、确认后停在详情、迟到 inspect 不覆盖。

## 12. ScheduledPage（/scheduled）

| 子组件 | props → emit | 说明 |
|---|---|---|
| ScheduledForm | props: {editing, libraries, types, submitting}；emit: `submit(body)`、`cancel-edit` | 本地草稿；类型显隐参数；cron 校验 300ms 防抖（`POST /scheduled/validate`）由表单发起（页面域读请求）；预设按钮 |
| ScheduledList | props: {items}；emit: `toggle(id)`、`run(id)`、`edit(id)`、`delete(id)` | runBadge 映射 |

- **状态**：`scheduledEditId` 需跨路由保留（旧版是模块级变量，app.js:1523）→ 放页面模块级 ref 或 app store slice（见建议调整 §A3）。
- 请求：页面 GET `/scheduled`+/libraries；POST/PUT/DELETE；toggle/run。
- 测试：编辑回填（params JSON 容错）、类型切换显隐、保存/取消/删除编辑态清理、cron 非法分支。

## 13. TasksPage（/tasks）/ ProbePage（/probe）

- 简单表格留页面；TasksPage "立即扫描" → tasks.runScan()；ProbePage "清空记录" → DELETE `/probe` + 重读；空状态复用 common/EmptyState（或页面内联，与旧文案一致）。

## 14. PlayerDialog（app 级）

| 项 | 说明 |
|---|---|
| props → emit | props: {title, url, type, proxyURL?}；emit: `close`；错误提示走全局 toast（由对话框调用 ui store） |
| 状态 | Artplayer 实例（shallowRef/普通引用）；usedProxy/reported 局部；DOM 挂载后创建，关闭/切页/卸载销毁 |
| 请求 | 无 fetch；URL 由页面/composable 组装（`/Videos/:id/stream|proxy`，预告片远程 URL 无代理） |
| 测试 | 17 项中的 16、17；错误分类纯函数单测 |

## 15. 公共模块

| 模块 | 内容 |
|---|---|
| composables/usePageRequest | 页面 GET 的取消 + 迟到丢弃（view 引用 + signal），对应 GL-12/17/27 |
| composables/useTaskPolling | "本轮结束再安排下一轮"轮询、退避、单一所有者（GL-11/16/19/20/31） |
| lib/number.ts | compactNumber/titleCarriesNumber/numberUnlessInTitle（逐字迁移，CT-01） |
| lib/entity-id.ts | entityIdOf（逐字迁移，CT-02） |
| lib/format.ts | fmtSize/fmtDuration/fmtTime/plotClamped |
| api/contracts.ts | 原始 DTO 类型（真实字段名）待 api-inventory.md 完成后落定 |
| stores/auth.ts / tasks.ts / media-wall.ts | 见 §3.3 状态表 |

## 16. 建议调整（对 §3.5 基线的增补说明，需在 P1 确认）

- **A1. TaskProgress 粒度**：计划未区分三种任务的面板差异。建议单个 TaskProgress 展示组件 + tasks store 输出已映射好的文案/明细行；轮询与自动隐藏策略全部在 store。副作用：无。
- **A2. ConfirmDialog vs 原生 confirm**：现 UI 用浏览器原生 confirm；计划 §3.5 写"复用确认组件"。引入自定义对话框是可见 UI 变化，建议 P1 明确采用（样式沿用 `.scrape-modal` 系）或首轮保持原生，二者都不影响行为验收。
- **A3. scheduledEditId 跨路由保留**：旧行为是模块级变量（离开再回来仍在编辑态）。Vue 页面组件默认卸载即失；需要模块级 ref / store slice 才能保持等价。若不保留，属行为变化，需记录。
- **A4. 详情刷新保留"相似影片"**：旧版把相似区 HTML 存进 detailUI（app.js:1741）；新实现应存"已加载结果 + loaded 标记"而非 HTML，刷新时直接以状态重渲染，避免重新请求与闪烁。
- **A5. 图片组件归属**：GL-10 的全局 error 委托在新架构中放 MediaImage 组件（@error 一次回退 + 占位），不保留全局委托。
- **A6. 媒体墙搜索防抖**：timer 必须在组件卸载/路由离开时清理（GL-14 注释），归属 MediaWallPage/onBeforeUnmount。
