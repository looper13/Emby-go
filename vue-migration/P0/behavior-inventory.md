# 行为对照表（P0 功能基线）

P0/VUE-01 产物。逐页穷举当前 UI 的每个交互、端点调用、反馈文案、失败路径与副作用；P3–P5 按此逐项验收。
锚点：`internal/server/web/app.js`（2052 行）、`login.js`（93 行）、`index.html`。toast/confirm 文案按原文抄录（验收逐字对照）。
冻结基线：`internal/server/web/` 只读，迁移完成前不改。

---

## 1. 登录 / 初始化（login.html + login.js）

| 交互/场景 | 触发 | 成功反馈 | 失败路径 | 状态与副作用 | 锚点 |
|---|---|---|---|---|---|
| 打开登录页 · 已有 token | `GET /Users/Me`（X-Emby-Token） | 2xx → `location.replace('/admin')` | 异常 → 清除 token，落到登录/初始化渲染 | — | login.js:80-87 |
| 打开登录页 · 无 token | `GET /api/auth/status` | `initialized` → 登录卡片；否则初始化卡片 | 失败 → 登录卡片 + 错误文案（`无法读取初始化状态`/其他 message） | — | login.js:5-9, 88-92 |
| 初始化表单校验 | 本地比较 `Pw` 与 `confirm` | — | 不一致 → `两次输入的密码不一致` | 表单重建（输入清空） | login.js:49-52 |
| 提交初始化 | `POST /api/auth/initialize` `{Username,Pw}` | `初始化完成，请使用新账户登录`（切回登录模式） | 非 2xx → 后端 `{error}` 或 `请求失败`；网络异常 → `error.message`/`网络错误` | busy 时全部输入与按钮 disabled，按钮文案 `请稍候…` | login.js:53-71 |
| 提交登录 | `POST /Users/AuthenticateByName` `{Username,Pw}` | `AccessToken` → 写 `localStorage.emby_token` → `location.replace('/admin')` | 同上 | 同上 | login.js:72-77 |
| 字段约束 | username `minlength=3` 必填；密码 `minlength=9` 必填；初始化才有确认密码；autocomplete username/new-password/current-password | — | 浏览器原生校验拦截 | 初始化模式自动聚焦用户名（非 busy 时） | login.js:24-33, 43 |

## 2. 控制台外壳（index.html）

| 交互/场景 | 行为 | 锚点 |
|---|---|---|
| 侧栏导航 11 项（总览/媒体库/媒体墙/手动补录/设置/API 密钥/刮削/计划任务/任务/接口探针） | 点击 → 记 `rememberPage`（hash 不同才 push）→ `page(name)`；详情页高亮"媒体墙" | index.html:21-62；app.js:1995-2001, 154-163 |
| 顶栏"开始扫描" | `runScan()`（见 §12） | index.html:74-77；app.js:2010 |
| 顶栏标题/面包屑 | 每页 `setMeta` 更新 h1 + crumb（`Archive / …`） | index.html:70-71；app.js:140-163 |
| 浮层容器 | 刮削预览、播放器、扫描进度、toasts 常驻 DOM，默认 hidden | index.html:85-114 |

## 3. 总览 overview（app.js:166-212）

| 交互/场景 | 触发 | 成功反馈 | 失败路径 | 状态与副作用 | 锚点 |
|---|---|---|---|---|---|
| 加载 | 并行 `GET /libraries`、`GET /items?status=success`、`GET /status` | 4 张统计卡 + 档案状态表 | 任一失败 → 整页错误面板（page() 统一处理） | 迟到响应丢弃 | 168-173 |
| 统计卡 | 媒体库数（`total ?? items.length`）、已入库 `success.total`、待补录 `pending`、不兼容 `incompatible` | — | — | — | 183-190 |
| 档案状态表 | 只显示数量 > 0 的状态行（success/manual/pending/incompatible） | — | 全 0 → 空状态 `档案为空`/`添加媒体库并开始扫描。` | — | 176-181, 197-201 |
| 全量重建索引按钮 | `POST /reindex`（按钮自身 disabled） | toast `重建完成：X 成功 / Y 待补录 / Z 不兼容` | 失败 toast error（`e.message`） | 完成后恢复按钮 + `refreshPage(view)` | 204-211 |

## 4. 媒体库 libraries（app.js:215-261）

| 交互/场景 | 触发 | 成功反馈 | 失败路径 | 状态与副作用 | 锚点 |
|---|---|---|---|---|---|
| 加载 | `GET /libraries` | 表：名称/路径/ID/操作；标题行 `共 N 个 · 扫描/浏览均以库为单位` | 失败 → 错误面板 | 迟到响应丢弃 | 218-223 |
| 行内"增量扫描" | `runScan(lib.Id)`（见 §12） | 扫描 toast 汇总 | 同 §12 | 同 §12 | 232, 255 |
| 行内删除（垃圾桶） | confirm `删除该媒体库及其影片索引？不会删除磁盘文件，但该库影片的播放进度/收藏会一并清除。` → `DELETE /libraries/:id` | toast `媒体库已删除` | 失败 toast error | `refreshPage(view)` | 256-260 |
| 添加媒体库表单 | Name/Path 必填 → `POST /libraries` `{Name,Path}` | toast `已添加媒体库「Name」` | 失败 toast error，**表单不清空**（无 reset 调用） | `refreshPage(view)` | 240-254 |
| 空状态 | 无库 → `还没有媒体库`/`先在下方登记一个存放 .strm 的目录。` | — | — | — | 236 |

## 5. 媒体墙 items（app.js:310-604 + 卡片 264-303）

### 5.1 加载与快照

| 场景 | 行为 | 锚点 |
|---|---|---|
| 进入页面 | 先 `GET /libraries`（校验 library_id），再按需分页拉 `GET /items?limit=100&offset=…&sort&order[&status][&search][&library_id][&entity][&scrape]` | 319, 550-556 |
| 快照复用 | `restore` 且 wallState 存在、`!dirty`、`!failed`、`queryKey===location.search`、60 秒内 → 直接重渲染缓存条目，不发请求 | 331-332, 495-498 |
| 恢复条数 | `options.restore.wallCount` 为目标条数循环加载（默认 100），直到 offset≥目标或 done | 500-503 |
| 无效 library_id | URL 库不存在 → 删参数 + `history.replaceState` 清洗 URL（保留其余 query/hash） | 321-326 |
| 筛选解析 | status/search/sort/order；sort 默认 `datecreated`，order 默认 title→asc 其余 desc；实体筛选取 person/genre/studio/tag/collection 中第一个非空 | 313-316, 329-330, 608 |
| 每页 100 条 | `WALL_PAGE_SIZE=100`；同一时刻一个在途分页（新请求 abort 旧 controller）；`wallGen` 代次 + `current()` 三校验丢弃过期响应 | 305, 538-548 |

### 5.2 筛选与搜索（离散 push / 搜索 replace）

| 交互 | 行为 | 锚点 |
|---|---|---|
| 库筛选 | ≤8 个库平铺 pill，>8 转下拉；点击/切换只动 `library_id`，push | 345-349, 402-415 |
| 状态 pill（全部/已入库/手动/待补录/不兼容） | set/delete `status`，**同时删除 `search`**，push | 342, 385-392 |
| 刮削 pill（刮削失败/待人工确认） | `scrape=failed|confirm` 点第二次取消，push | 364-365, 394-400 |
| 搜索框 | 400ms 防抖 → `replaceQuery`（不产生历史条目）+ 更新 `wallState.search/queryKey` → `reloadWall()`（不重渲染筛选栏、保输入焦点） | 417-427, 508-528 |
| 排序 select | set `sort`、删 `order`，push | 343, 428-435 |
| 实体筛选 chip ✕ | 删除全部实体参数，push | 360-361, 436-442 |
| 空状态文案 | `没有符合条件的影片` / `没有 X 的…`；搜索时副文案 `试试其它关键词。` 否则 `开始扫描或手动补录后再来看看。` | 596-602 |

### 5.3 卡片与无限滚动

| 交互 | 行为 | 锚点 |
|---|---|---|
| 海报 | Primary 320（带 tag）→ 无则 Thumb 320 → 无则文本路径；`data-fallback` 为 `cover_url` | 268-269, 280-282 |
| 副行 | `番号(标题已含则省略) · 年份 · 原名`，全空回退协议名 | 270 |
| 进度条 | userdata.position_ticks / (RuntimeSeconds×1e7)，cap 100% | 272-273 |
| 角标 | 非 success 状态徽章、已看、♥（收藏）、`CD×N`（AdditionalParts+1） | 274-279 |
| 卡片操作按钮 | 探测（probeItem）、重读（`POST /items/:id/reread` → toast `状态已更新：…`）、删除（confirm `仅删除数据库索引，不删除源文件。继续？` → `DELETE /items/:id` → toast `已删除索引`）；后两者完成后 `refreshPage` | 292-294, 446-467 |
| 快捷播放 | playable 时显示播放按钮 → `openPlayer(item)`（不跳详情） | 296, 469-475 |
| 点卡片 | `openItemPage(id)`（推历史进入详情） | 476-479 |
| 键盘 | 卡片（tabindex=0，role=button）Enter/Space 仅在目标是卡片本身时进入详情 | 482-489 |
| 无限滚动 | scroll 监听 + `#wall-more` 哨兵距视口 ≤600px 触发下一页；`restoring` 期间禁止 | 531-536, 2011 |
| 加载状态 | 哨兵：`加载中…` / `已全部加载（共 N 条）`；失败 `加载失败：msg` + 重试按钮（点一次只发一轮） | 565-576, 603 |
| 计数行 | `共 N 条 · 库名 · 实体 · 状态 · 已加载 M` | 585-593 |

## 6. 影片详情 item（app.js:715-896）

### 6.1 加载与请求

| 场景 | 行为 | 锚点 |
|---|---|---|
| 并行请求 | 主体 `GET /items/:id/detail`；相似 `GET /Items/:id/Similar?Limit=12`（embyJSON：5 秒超时、失败返回 null、不阻塞主体） | 717-718, 640-653 |
| 迟到隔离 | 主体前后 `isCurrentPage` 校验；相似结果到达时再校验（切页/切详情后丢弃） | 719, 881-882 |
| 标题/面包屑 | h1=影片标题；crumb 固定 `媒体墙 / 详情`（不用 META 表） | 742-743 |
| 返回按钮 | `← 返回媒体墙 (Esc)` → `backFromItem` | 751, 838 |

### 6.2 头部与动作

| 交互 | 行为 | 锚点 |
|---|---|---|
| 背景图 | Backdrop 1280（有 BackdropPath）→ Thumb 1280（LandscapePath）→ 海报兜底；fallback=cover_url | 733-734, 747 |
| 海报 | Primary 320 → 无本地图用 NFO `cover_url` | 732, 749 |
| 头部信息行 | `番号(标题已含则省略) · 年份 · 时长 · ★评分`，空则 `—` | 738, 753 |
| 徽章 | 状态徽章 + 合集徽章（m.collection）+ Tags 芯片（点击跳标签筛选） | 754 |
| 播放按钮 | 仅 `playable`（detail.playable ?? success/manual）显示；→ `openPlayer(m)`；不可播显示提示文案 | 756-758, 839-841 |
| 探测媒体信息 | 按钮 disabled → `POST /items/:id/probe` → toast 摘要（`已写入 NFO：1920x1080 · H.264 · …`）→ 成功则 `refreshPage` | 759, 842-848, 1884-1905 |
| 刮削 | → `openScrapePreview(m)`（见 §8.2） | 760, 849-850 |
| 重读源 | disabled → `POST /items/:id/reread` → toast `状态已更新：…` → `refreshPage` | 761, 851-861 |

### 6.3 分区

| 分区 | 行为 | 锚点 |
|---|---|---|
| 简介 | >260 字符默认折叠（`is-clamped`）；`展开全部`/`收起简介` 切换 aria-expanded | 656-657, 769, 862-867 |
| 预告片 | 有 `trailer_url` 才渲染；缩略图=首张 Backdrop 840 → 无则背景图/封面；点击 → 内置播放器播远程 URL（无代理回退） | 771-775, 868-870, 980-987 |
| 剧照 | Backdrops 网格；缩略 480 带 tag；`<a target=_blank>` 打开原尺寸 | 777-779 |
| 演员 | 有图 → `/Items/{entityIdOf('person',name)}/Images/Primary?maxWidth=200[&tag]`；无图 → 首字占位；点击 → 跳媒体墙按演员筛选 | 781-790, 899-904 |
| 相似影片 | 初始 hidden 空区；结果到达填充（海报 Primary 320 + 年份/时长/评分）；点卡片 → `openItemPage`（压历史） | 792, 876-895 |
| 媒体信息 | 每文件：主文件/分段 N、文件名、大小、未探测徽章、流表（视频/音频/字幕：编码/分辨率/码率/Profile/BitDepth/声道/语言/采样率/默认/强制/外挂）、完整路径 | 794-806, 687-704 |
| 元数据 dl | 番号/原名/年份/分级/类型/标签/厂商（Studios→Maker→Label）/导演/合集/媒体库/来源；类型·标签·厂商·合集为实体链接 | 808-822, 706-712 |
| 文件与时间 details | 折叠区（刷新时保留展开态）：源文件/NFO/最后修改/入库时间/上次刮削/刮削结果 | 824-834 |

### 6.4 刷新与实体跳转

| 场景 | 行为 | 锚点 |
|---|---|---|
| 刷新保留 UI | 探测/重读/刮削后 `refreshPage`：保留剧情展开、文件展开、相似区 HTML、阅读位置 | 1737-1743, 792 |
| 实体跳转 | `gotoEntity(key,value)`：清全部实体参数 → set 新参数 → 保留 `wallState.libraryID` → push `?key=value#items` → `page('items')`（走 page 以重置标题） | 660-669 |
| 刷新失败 | 保留旧数据与按钮可用（page() 回滚，GL-17）；可重试 | 1775-1787 |

## 7. 手动补录 manual（app.js:990-1041）

| 交互/场景 | 触发 | 成功反馈 | 失败路径 | 状态与副作用 | 锚点 |
|---|---|---|---|---|---|
| 加载 | `GET /libraries`（取第一个库名做归属提示，无库则不显示提示） | — | 失败 → 错误面板 | — | 992, 1011 |
| 字段 | 标题*、番号、年份(1900-2100)、原名、.strm 目标路径*、媒体直链*(url)、简介、类型/标签/制作商（逗号或全角逗号分隔） | — | — | — | 999-1008, 1019 |
| 提交 | `POST /items/manual`（genres/tags/studios 拆分数组；year=Number\|\|0；source_path/source_url/title/number/original_title/plot） | toast `「title」已入库` + **form.reset()** | 失败 toast error，表单保留（submit 按钮恢复） | 提交期间按钮 disabled | 1015-1040 |

## 8. 刮削 scrape（app.js:1179-1379）

### 8.1 配置页

| 交互/场景 | 触发 | 成功反馈 | 失败路径 | 状态与副作用 | 锚点 |
|---|---|---|---|---|---|
| 加载 | 并行 `GET /scrape/settings` + `GET /libraries`；`GET /scrape/progress`（catch 静默） | 配置表单回填 + 进度面板 | 前两者失败 → 错误面板 | progress.running → 启动轮询 | 1181-1186, 1325-1326 |
| 配置字段 | metatube_url、metatube_token（留空=不修改，已有值显示占位提示）、timeout_seconds(30)、concurrency(2)、image_quality(90)、avatars_dir、download_images、overwrite；translate：title/summary/target_lang(ZH)/timeout_seconds(30)/api_url/api_key | — | — | — | 1197-1231 |
| 保存 | `PUT /scrape/settings`（完整 body，含 translate 嵌套） | toast `配置已保存并即时生效` | 失败 toast error | `refreshPage` | 1257-1278 |
| 测试 MetaTube / 测试翻译 | `POST /scrape/test` `{target: metatube\|translate}` | toast `` `${detail}（${elapsed_ms}ms）` `` | toast `连接失败：{error}` | — | 1281-1288 |
| 候选数 | `GET /scrape/candidates?library_id&only_missing` → `本次将处理 N 条`；库/只补缺失变化即刷新 | — | 静默忽略 | — | 1290-1299 |
| 开始刮削 | `POST /scrape/run` `{library_id:Number\|\|0, only_missing, overwrite(本次强制覆盖)}` | toast `刮削已启动（N 条）` | 失败 toast error | 启动 1500ms 轮询 | 1301-1312 |
| 补演员头像 | `POST /scrape/avatars` `{library_id}` | toast `头像任务已启动（N 个演员）` | 失败 toast error | 启动轮询 | 1313-1319 |
| 中止 | `POST /scrape/cancel` | toast `已请求中止` | 失败 toast error | 轮询继续到 running=false | 1320-1323 |
| 进度面板 | 标题：`正在刮削`/`正在补演员头像`/`刮削结束`；计数 done/total；成功/跳过/失败/当前/错误 parts；失败样例 details 列表（mono） | — | 轮询请求错 → 停止轮询（旧行为，见 GL-C3） | 无 total 且 running → 不确定进度条 | 1342-1366 |

### 8.2 单条刮削预览对话框（app.js:1382-1509）

| 交互/场景 | 触发 | 成功反馈 | 失败路径 | 状态与副作用 | 锚点 |
|---|---|---|---|---|---|
| 打开 | 详情页"刮削"按钮 | body `正在搜索候选…` → `GET /items/:id/scrape/preview` | 失败 → 红字错误（保留浮层）；无候选 → 提示搜索词 | state 含 view 引用；迟到/跨页校验 `scrapeState===state && isCurrentPage` | 1391-1412 |
| 自动 inspect | 推荐项 `recommended>=0?recommended:0` → `loadScrapeInspect` | body `正在读取详情…` → `POST /items/:id/scrape/inspect` `{provider,id}` → 渲染预览 | 失败 toast | 切换候选取消旧读取（state 校验） | 1406-1407, 1414-1427 |
| 候选列表 | 缩略图/标题/副行（番号去重展示/provider/★score/番号命中徽章）；点击切换 → loadScrapeInspect | — | 失败 toast | active 高亮当前 provider+id | 1436-1444, 1489-1493 |
| 字段差异 | 每行 label/old/new；`is-change` 高亮；无 new → `（不写入）` | — | — | — | 1446-1450 |
| 图片列表 | 预览图；`exists` → 标注 `（已存在，只补缺失时不覆盖）` | — | — | — | 1452-1456 |
| 强制覆盖开关 | 随 inspect.overwrite 回显 | — | — | — | 1484 |
| 取消 / ✕ / 遮罩 / Esc | 关闭浮层，**零写入零确认请求** | — | — | scrapeState=null | 1384-1388, 1494, 2016-2019, 2022 |
| 确认写入 | `POST /items/:id/scrape` `{provider,id,overwrite}` | toast `已写入「title」（图片 N 张）` → 关闭浮层 → `refreshPage(state.view)` **停在详情页** | 失败 toast error，按钮恢复可再试 | 确认按钮 disabled 防重复 | 1495-1508 |

## 9. 计划任务 scheduled（app.js:1511-1713）

| 交互/场景 | 触发 | 成功反馈 | 失败路径 | 状态与副作用 | 锚点 |
|---|---|---|---|---|---|
| 加载 | 并行 `GET /scheduled` + `GET /libraries` | 表单 + 已配置任务表 | 失败 → 错误面板 | 编辑态由模块变量 `scheduledEditId` 决定 | 1539-1547 |
| 表单字段 | 名称*、类型 select（data.types 或内置 TASK_TYPE_TEXT）、cron*、媒体库（reindex 时隐藏）、影片状态+仅未探测（仅 probe 显示）、启用（新建默认勾选）、常用预设 5 个 | — | — | `syncParams` 按类型显隐 | 1557-1580, 1611-1620 |
| cron 校验 | 输入/选预设 → 300ms 防抖 → `POST /scheduled/validate` `{cron}` | `接下来执行：t1 · t2…` | `表达式非法：error`（红字）；空输入提示 `请输入 cron 表达式。` | 清空/合法时恢复默认色 | 1623-1632, 1646-1650 |
| 编辑 | 列表"编辑" → `scheduledEditId=id` → refreshPage；表单回填 params（JSON.parse 容错） | — | — | 取消编辑清空 id | 1634-1643, 1651-1653, 1699-1700 |
| 提交（新建） | `POST /scheduled` `{name,type,cron,enabled,params}` | toast `计划任务「name」已创建` | 失败 toast error，按钮恢复 | `refreshPage` | 1656-1684 |
| 提交（编辑） | `PUT /scheduled/:id` | toast `计划任务已保存` + 清编辑态 | 同上 | 同上 | 1674-1677 |
| 启停开关 | `POST /scheduled/:id/toggle` | toast `状态已更新` | toast error（无论成败都 refreshPage） | `refreshPage` | 1690-1693 |
| 立即执行 | `POST /scheduled/:id/run` | toast `已触发执行，可在「任务」页查看结果` | toast error | — | 1694-1698 |
| 删除 | confirm `删除该计划任务？已产生的影片数据不受影响。` → `DELETE /scheduled/:id` | toast `计划任务已删除` | toast error | 若删的是编辑中任务则清编辑态；`refreshPage` | 1701-1710 |
| 列表列 | 名称/类型/表达式/下次执行（禁用时显示 `已禁用`）/上次结果（徽章+时间+消息）/启用开关/操作 | — | — | — | 1594-1607 |
| 时间格式 | RFC3339 → `zh-CN` locale string（hour12:false）；空值 `—` | — | — | — | 1526-1530 |

## 10. 任务 tasks（app.js:1120-1151）

| 交互/场景 | 触发 | 反馈 | 锚点 |
|---|---|---|---|
| 加载 | `GET /tasks` | 表：类型（TASK_TYPE_TEXT 映射）/状态徽章（RUN_STATUS_TEXT）/开始/结束/错误 | 1122-1148 |
| 进行中提示 | `data.running` → `有任务正在进行…`（accent 色） | — | 1135 |
| 刷新 | `refreshPage(view)` | — | 1150 |
| 立即扫描 | `runScan()`（§12） | — | 1149 |
| 空状态 | `暂无任务`/`点击「立即扫描」开始索引。` | — | 1147 |

## 11. 接口探针 probe（app.js:1154-1175）

| 交互/场景 | 触发 | 反馈 | 锚点 |
|---|---|---|---|
| 加载 | `GET /probe` | 表：方法/路径/时间；提示 `记录客户端发来但本服务未注册的 Emby 请求，用于补齐端点。上限 1000 条。` | 1156-1169 |
| 清空记录 | `DELETE /probe` | toast `探针记录已清空` → `refreshPage` | 1171-1174 |
| 空状态 | `暂无探针记录`/`当客户端请求了未注册的 Emby 接口后会显示在这里。` | — | 1169 |

## 12. 扫描（全局按钮 + 进度面板，app.js:1907-1981）

| 场景 | 行为 | 锚点 |
|---|---|---|
| 触发 | 顶栏"开始扫描"（无参）或媒体库行"增量扫描"（`?library_id=`） | 2010, 255, 1969 |
| 启动 | 禁用按钮 + 文案 `扫描中…`；显示 `#scan-progress` 面板（标题 `正在扫描…`，清空计数/详情）；启动 600ms 轮询；`POST /scan[?library_id]` | 1959-1969 |
| 完成 toast | `增量扫描完成：新增 X / 更新 Y / 跳过 Z / 删除 W / 失败 V` | 1971 |
| 收尾 | 重新渲染 progress；停止轮询；恢复按钮；按当前 hash 刷新当前页（`#item/<id>` → 原地刷新详情，其他已注册页 → `page(name)`） | 1973-1980 |
| 进度渲染 | walk 阶段：标题 `正在扫描 lib`、计数 `已发现 N`/`遍历中`、不确定进度条；process 阶段 `done/total` + 百分比；末尾 `cancelled` → `扫描已取消`；详情行：新增/更新/跳过/删除[/已入库/待补录/不兼容/失败/当前 basename][/错误] | 1919-1940 |
| 多库 | `libraries>1` 时标题带 `（index/total）` | 1922 |
| 结束隐藏 | 轮询见 `!running` → 停止 + 4 秒后面板 hidden | 1950-1955 |
| 刷新页面恢复 | boot 查 `/scan/progress`，running → 渲染 + 恢复轮询 + 按钮禁用（GL-11） | 2027-2037 |
| 取消语义 | 浏览器刷新/关闭 → 后端跟随请求 context 取消（不在迁移内改动） | 计划 §5.3 |

## 13. 批量探测（媒体墙内，app.js:1794-1905）

| 场景 | 行为 | 锚点 |
|---|---|---|
| 触发 | 媒体墙"探测媒体信息"按钮 → `runProbeAll` | 443-444, 1861 |
| 启动 | 按钮 disabled + `探测中…`；面板 `正在启动探测…`；`POST /probe/media` `{only_missing:true}` | 1861-1868 |
| 无可探测 | `r.total==0` → toast `r.message` 或 `没有可探测的影片`；恢复按钮、藏面板 | 1869-1874 |
| 开始 | toast `开始探测 N 条影片`；1000ms 轮询；渲染面板 | 1875-1877 |
| 进度渲染 | 标题 `正在探测媒体信息`/`探测已中止`/`探测完成`；计数、百分比、成功/跳过/失败/当前/错误 | 1808-1826 |
| 结束 | 轮询见 `!running` → 停 + 恢复按钮 + toast 汇总（cancelled 用 `探测已中止：…`） + 失败样例 toast（第一条）；**无失败时** 8 秒后面板自动隐藏（有失败保留对照） | 1838-1859 |
| 单条探测 | 详情/卡片 → `POST /items/:id/probe` → toast `probeSummary`（`已写入 NFO：WxH · CODEC · Profile · fps · Mbps` 或 `已写入 NFO`） | 1884-1905 |

## 14. 内置播放器（app.js:909-987）

| 场景 | 行为 | 锚点 |
|---|---|---|
| 打开（正片） | `openPlayer(item)`：`/Videos/:id/stream`，type 按 `source_container==='webm'` 取 webm/mp4，proxyURL=`/Videos/:id/proxy` | 969-977 |
| 打开（预告片） | `openTrailer`：NFO 远程 URL，按扩展名定 type，**无代理回退** | 980-987 |
| 实例管理 | 全局单实例 `artInstance`；打开前 destroy 旧实例；清空 stage DOM；播放器配置：autoplay、volume1、播放速率/比例/全屏 web/设置/热键/PiP、主题 `#e0a44b`、zh-cn | 925-952 |
| 错误回退 | error 回调先校验 `artInstance===player`；有 proxyURL 且未回退 → 切代理 + `muted=true` + `play().catch`（移动端静音自动播放）；否则一次性 toast（`reported` 标记） | 954-966 |
| 错误文案 | HTTPS 页面加载 HTTP 源 → 混合内容提示；code 2/3/4 → 网络/解码/不支持源；其他默认文案 | 917-923 |
| 关闭 | ✕/遮罩点击/Esc → `destroy()` + 隐藏 + 移除 body 类；切页（非刷新）自动关 | 909-914, 1747-1752, 2012-2015 |
| 标题 | `播放` / 影片标题或文件名 / `预告片 · 标题` | 971-972, 982-984 |

## 15. 页面公共机制（app.js:1716-1792）

| 场景 | 行为 | 锚点 |
|---|---|---|
| 页面注册表 | overview/libraries/items/item/manual/settings/apikeys/scrape/scheduled/tasks/probe；未知 → `模块即将开放`/`请先使用上方导航。` | 1716-1728, 1761 |
| 正常切页 | abort previous 链 → 关播放器/预览 → setMeta → `scrollTo(0,0)` → 骨架屏 → 执行页面函数 | 1747-1759 |
| 刷新（preserveScroll） | 捕获 restore{scrollY,wallCount} + detailUI{plotExpanded,filesExpanded,similarHTML}；完成后恢复焦点（focusID）与滚动；媒体墙再触发补页检查 | 1735-1744, 1763-1774 |
| 成功渲染后 | `abortPage(previous)`（旧页面请求生命周期结束） | 1765-1766 |
| 刷新失败 | 内容未变 → 回退 activePage/title/crumb，toast 错误，保留旧页面（GL-17） | 1775-1787 |
| 加载失败（非刷新） | 错误面板（alert 图标 + message） | 1789-1790 |

## 16. 状态文本映射（验收对照）

| 常量 | 值 | 锚点 |
|---|---|---|
| STATUS_TEXT | success 已入库 / manual 手动录入 / pending 待补录 / incompatible 不兼容 / failed 失败 | 124-128 |
| TASK_TYPE_TEXT | scan 增量扫描媒体库 / watch 实时局部刷新 / poll 兼容模式局部刷新 / reindex 全量重建索引 / probe 媒体信息探测 | 1520 |
| RUN_STATUS_TEXT | success 成功 / failed 失败 / skipped 跳过 / cancelled 已取消 / running 进行中 | 1521 |
| CRON_PRESETS | 每小时 0 * * * * / 每 6 小时 0 */6 * * * / 每天 03:00 0 3 * * * / 每周一 04:00 0 4 * * 1 / 每月 1 日 05:00 0 5 1 * * | 1513-1519 |
| ENTITY_PARAMS | person 演员 / genre 类型 / studio 厂商 / tag 标签 / collection 合集 | 608 |
| META 标题/面包屑 | 见 §2 外壳（详情页例外：h1=影片名，crumb=媒体墙 / 详情） | 140-152, 742-743 |
