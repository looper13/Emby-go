# 全局行为编号清单（GL）

P0/VUE-01 产物。核对计划 §4.2 的 GL-01..GL-16 并全面补充。所有锚点基于当前冻结基线 `internal/server/web/app.js`（2052 行）与 `login.js`（93 行）。

规则：新实现中每条行为必须有明确归属；编号是 P2 起"历史导航设计成立"等验收的引用依据。标 ⚠ 的行是**有意与旧版不同的显式改进**，需在测试中记录差异。

## A. 计划 §4.2 原始 16 条（核实后）

| 编号 | 既有行为与代码锚点 | 补充说明 | 新归属 | 验证阶段 |
|---|---|---|---|---|
| GL-01 | `boot` 登录校验：无 token 或 `/Users/Me` 非 2xx → 清 token → `location.replace('/')`（app.js:1983-1992）；登录页同样预检（login.js:80-87） | `/Users/Me` 抛异常（网络故障）与 401 都走"回登录"——⚠ 新实现要把网络失败与明确 401 分开（计划 §5.2） | auth store 与路由守卫 | P2 |
| GL-02 | `history.scrollRestoration='manual'`（app.js:1993） | boot 时全局设置一次；浏览器原生的滚动恢复必须关闭，否则与自实现恢复打架 | 路由恢复模块，全局一次设置 | P2 |
| GL-03 | `rememberPage`：把 `scrollY`、媒体墙 `wallCount`（已加载条数）与 `focusID` 写进当前历史条目（app.js:17-25，用 `history.replaceState`） | 调用点：媒体墙 pushQuery（app.js:383）、openItemPage（app.js:619）、gotoEntity（app.js:665）、导航点击（app.js:1997）。Vue Router 有自己的 `history.state` 内部字段，新实现只能用独立命名空间（如 `state.__wall = {...}`），不能覆盖 Router 字段 | 历史条目恢复模块 | P2/P4 |
| GL-04 | 导航点击用 `history.pushState` + 直接调 `page()`，避免 hash 变化再触发 popstate 造成双重渲染（app.js:1995-2001 注释） | 新实现只有 Router 一个导航入口；用请求计数证明不重复渲染 | Router 唯一导航入口 | P2 |
| GL-05 | `popstate`：按 `location.hash` + `location.search` 重渲染页面，并传 `{restore: history.state}`（app.js:2003-2009） | 返回时恢复的是"被返回的那条历史条目"记录的条数/焦点/位置 | Router 历史导航与恢复 | P2/P4 |
| GL-06 | `setMeta`：更新 `#page-title`(h1) 与面包屑，导航高亮把 `item` 映射到 `items`（app.js:154-163，META 表 140-152） | ⚠ 注意：改的是 h1 而非 `document.title`，浏览器标签页标题从不变化（见 GL-22） | route meta 与当前有效详情 | P2/P4 |
| GL-07 | Escape 优先级链（app.js:2020-2026）：刮削预览 → 播放器 → 详情返回（`currentItemID()` 判在详情页时 `backFromItem`） | 新实现同序；输入控件聚焦、原生 confirm 弹出时不误触 | 应用快捷键/浮层管理 | P2/P4/P5 |
| GL-08 | 关闭路径：播放器 ✕ 与遮罩点击（app.js:2012-2015）、刮削 ✕ 与遮罩点击（app.js:2016-2019） | 遮罩判定为 `event.target.id === overlay.id`（点到内容不关） | 对应对话框 | P4/P5 |
| GL-09 | `window` 被动 scroll 监听触发媒体墙补页 `wallMaybeLoadMore`（app.js:2011，实现 531-536） | 条件：媒体墙活跃、未 loading/done/failed/restoring，且哨兵 `#wall-more` 距视口 ≤ innerHeight+600 | 媒体墙域订阅并清理 | P4 |
| GL-10 | 图片 error 捕获委托在 `#content`（capture 阶段）：先换 `data-fallback`（一次），再替换为占位 `<span role="img">`（app.js:109-122） | `delete img.dataset.fallback` 保证只回退一次；占位保留 aria-label 与 className | 图片组件 | P4 |
| GL-11 | boot 时恢复 scan 进度：`/scan/progress` running → 渲染 + 600ms 轮询 + 禁用扫描按钮（app.js:2027-2037）；以及 probe 进度同样恢复（app.js:2038-2046） | 两个独立 try/catch，失败静默 | 应用任务模块 | P5 |
| GL-12 | `page()` 切页取消读取链、关播放器/预览（app.js:1747-1759）；`abortPage` 沿 `previous` 链逐个 abort（app.js:10-15）；刷新（preserveScroll）保留旧内容与展开态（app.js:1735-1744） | 取消的是页面 GET；写请求与 `/progress` 不自动挂 signal（见 GL-14） | 页面请求与刷新域、浮层生命周期 | P2/P4/P5 |
| GL-13 | `api()` 401：`localStorage.removeItem('emby_token')` + `location.replace('/')` 并抛"登录已失效"（app.js:85-89）；非 GET 成功后 `wallState.dirty = true`（app.js:97） | ⚠ 旧版多请求同时 401 会各自触发一次 replace；新实现要合并为一次跳转（计划 §5.2） | API client、auth/wall store | P2/P3 |
| GL-14 | 离散筛选 push（状态/刮削/库/排序/实体清除，app.js:385-442）；搜索输入 replace + 400ms 防抖（app.js:417-427, debounce 70-76）；`api()` 对只读请求自动挂 `activePage` signal，但 `*/progress` 除外、写请求除外（app.js:79-80） | 搜索 replace 时同步更新 `wallState.search/queryKey` 后走 `reloadWall`（不整页重渲染，保输入焦点） | route query + MediaWallPage / API client | P2/P4 |
| GL-15 | `backFromItem`（app.js:627-631）：有 `wallState` 且当前 `history.state.item` 存在 → `history.back()`；否则（直开详情/详情页刷新）replace 到 `#items` | 直开详情返回必须落在媒体墙且不增加历史条目 | 路由恢复模块 | P2/P4 |
| GL-16 | toast 3.2s 后加 `out` 类，再 260ms 移除（app.js:61-67）；各轮询/隐藏定时器按生命周期清理（scan 完成 4s 隐藏 1954；probe 完成 8s 隐藏 1852-1855；scrape 轮询 1370-1379） | 定时器必须随宿主销毁清理，防切页后操作已卸载 DOM | ToastHost、任务模块 | P3/P5 |

## B. P0 补充编号（原清单未覆盖）

| 编号 | 既有行为与代码锚点 | 说明/边界 | 新归属 | 验证阶段 |
|---|---|---|---|---|
| GL-17 | `page()` 刷新失败回滚：preserveScroll 刷新失败且旧内容仍在 → `activePage` 回退到 previous、恢复标题/面包屑、toast 错误、**保留旧页面可用**（app.js:1775-1787）；非刷新失败 → 错误面板（1789-1790） | 这是"刷新失败保留旧数据"的实现点，与 17 项里"failed refresh preserves old callbacks"对应 | 页面请求与刷新域 | P2/P4 |
| GL-18 | `embyJSON`：独立 AbortController + 父 view signal 联动 + 5000ms 超时（app.js:640-653）；失败一律返回 `null` 由调用方降级；abort 后不再读 body | 相似推荐/详情里的 Emby 接口共用；超时 timer 必须在 finally 清理（测试 5 断言 timers 归零） | API client（emby 域）| P2/P4 |
| GL-19 | `runScan` 全流程（app.js:1959-1981）：禁用 #scan 按钮改文案"扫描中…"→ 显示面板 → 启动 600ms 轮询 → `POST /scan[?library_id]` → toast 汇总 → 重新渲染 progress → 停止轮询/恢复按钮 → 按当前 hash 刷新当前页（详情走 `refreshItemPage`，其他 `page(name)`） | 长请求由应用持有；浏览器刷新时后端跟随 context 取消（计划 §5.3）；⚠ 新实现不允许把该 POST 挂到组件卸载 | 应用任务模块 | P5 |
| GL-20 | runScan 结束态恢复：轮询见 `!running` → 停止、恢复按钮（app.js:1950-1955）；scan 面板结束 4s 后自动隐藏 | 与 boot 恢复（GL-11）是同一状态机的两个入口 | 应用任务模块 | P5 |
| GL-21 | `scheduledEditId` 模块级编辑态（app.js:1523）：编辑点击设值→重渲染表单回填；取消/保存/删除同名任务时清空（1652, 1677, 1706） | 跨页面刷新保留（回到 scheduled 时仍在编辑态）；⚠ 这是旧版"页面函数外状态"的一个例子，新实现归 ScheduledPage 的组件状态还是 store 需在 P3 定（建议页面级 reactive，随路由离开清理） | ScheduledPage | P3 |
| GL-22 | 浏览器标签页标题从不变化：`titleEl` 是 h1（index.html:71，app.js:4/742），详情页只改 h1 | 新实现沿用；若将来要改 `document.title` 属产品变更，不在迁移内 | setMeta/路由 meta | P2/P4 |
| GL-23 | 导航/切页 `window.scrollTo(0,0)` + 骨架屏（app.js:1756-1759）；媒体墙渲染完恢复滚动（1767-1772） | 正常进入从顶部，历史返回才恢复位置；不用 KeepAlive 兜底 | 路由恢复模块 | P2/P4 |
| GL-24 | 媒体墙卡片键盘：Enter/Space 仅在事件目标是卡片本身时进入详情；嵌套按钮不冒泡触发（app.js:482-489，`event.target !== card` 判定） | 与 17 项测试 9 对应；卡片内 probe/reread/delete/quickplay 都是按钮 | MediaCard | P4 |
| GL-25 | 媒体墙快照复用条件（app.js:331-332）：restore 且 wallState 存在、`!dirty`、`!failed`、`queryKey === location.search`、`Date.now()-loadedAt < 60000` | 任一不满足重建 wallState；dirty 由任何写成功（GL-13）置位 | media-wall store | P4 |
| GL-26 | 无效 `library_id` 清洗（app.js:321-326）：URL 的库不存在 → 删参数 + `history.replaceState` 同步 URL（保留其余 query 与 hash） | 旧书签/前进后退都适用；同 17 项测试之外的"被删除的库"场景 | MediaWallPage（读库列表后） | P4 |
| GL-27 | 详情页 `previous` 链与迟到隔离：`isCurrentPage(view)` = 当前页引用相等且 signal 未 abort（app.js:8）；`pageItem` 在 await 前后都校验（719, 882）；相似推荐 then 里再校验（882） | "迟到响应不提交"的双保险：请求号/代次 + 卸载取消 | 页面请求域 | P2/P4 |
| GL-28 | `openItemPage` 先 `rememberPage(id)` 再 pushState `#item/<id>`（app.js:616-622）；应用内进详情后返回走 `history.back()` | 直接打开详情（无 wallState 或 state.item 缺失）由 GL-15 兜底回墙 | 路由恢复模块 | P2/P4 |
| GL-29 | 探测按钮忙碌态：`setProbeButton` 改文案"探测中…"（app.js:1830-1836）；`probeItem` 单条探测按钮在详情页禁用-恢复（846-848）；`#detail-reread` 同样（851-861） | 所有提交/长操作按钮必须防重复点击 | 对应页面/任务组件 | P4/P5 |
| GL-30 | `wallGen` 请求代次（app.js:307, 340, 545, 548）：切筛选/reloadWall/page() 都递增；`current()` 同时校验 view 引用、wallState 引用与代次 | 与 AbortController 双保险，"旧分页不得追加到新筛选"的实现点 | media-wall store | P4 |
| GL-31 | `renderScrapeProgress`/`renderProbeProgress` 在对应 DOM 不存在时**只保持轮询不渲染**（app.js:1334-1335, 1810）；轮询在切页后继续 | 任务进度组件常驻应用层（不随页面卸载）；与 GL-11 恢复路径一致 | 应用任务模块 | P5 |
| GL-32 | 详情刷新传入 `detailUI`（plotExpanded/filesExpanded/similarHTML）保留展开与已渲染的相似区（app.js:1737-1743, 869-867 附近 792） | 刷新（探测/重读/刮削后）不得丢用户阅读状态 | ItemDetailPage | P4 |
| GL-33 | 图片 URL 形状：`imageURL` 查询参数顺序固定 `tag` 在前 `maxWidth` 在后（app.js:101-106）；演员头像 URL 直接拼接 `entityIdOf('person', name)`（785） | 17 项测试 13/15 断言 URL 形状；URL 构造集中到模块，禁散落字符串拼接 | api/图像 URL 模块 | P4 |
| GL-34 | Emby 登录接口形状：`POST /Users/AuthenticateByName` body `{Username, Pw}` → `AccessToken`；初始化 `POST /api/auth/initialize` 同 body（login.js:54-74） | token 存 `localStorage.emby_token`；请求头 `X-Emby-Token`（app.js:81） | api/auth.ts + auth store | P2 |
| GL-35 | 登录页两处成功跳转都硬编码 `/admin`（login.js:74, 84） | ⚠ 按计划 §4.1：Vue 的两条成功路径统一走目标解析函数；迁移期留在 Vue 入口（`/admin-vue`），正式切换后 `/admin` | 登录目标解析模块 | P2 |

## C. 有意变更（与旧版的显式差异，需测试记录）

1. **网络失败 vs 401 区分**（对应 GL-01/GL-13）：旧版任何 `/Users/Me` 异常都清 token 回登录；新实现网络故障不清 token，提供重试。
2. **并发 401 合并**（GL-13）：旧版 N 个并发 401 触发 N 次 `location.replace('/')`；新实现只跳转一次。
3. **轮询连接异常≠任务结束**（计划 §6.4）：旧版 scan/probe 轮询 `catch{}` 后按 `!p` 当"结束"处理（app.js:1950, 1844-1857 均如此）；新实现保留最后状态显示"状态暂不可用"，有限退避，不误判完成。这是**行为修正**，验收时单独记录。
4. **scrape 轮询**在 progress 请求出错时直接停止（app.js:1377），同上归入第 3 条处理。
