# P2 核心通路与历史导航

日期：2026-10-10。依据根目录迁移计划 VUE-04/05，接续 P1，不建立第二套迁移计划。

## 当前任务

- [x] VUE-04：真实初始化与登录、已有 token、网络失败保留 token/重试、并发 401 合并、重登恢复合法目标；迁移入口保持 `/admin-vue`。
- [x] VUE-04：最小媒体墙导航、通知生命周期、旧 hash/外层 query 转换、历史保存与返回、详情页 Escape。
- [x] VUE-05：必要媒体墙/详情 GET，100 条分页，300/250 快照、位置与焦点恢复，迟到详情/分页隔离。
- [x] 桌面 1280×800 与手机模拟 390×844 真实 embedui 主链路；旧入口与协议接口继续隔离。
- [x] P3：总览操作、媒体库、只读设置、API 密钥、补录、任务历史、接口探针；接续结果见 [P3/progress.md](../P3/progress.md)。

本轮只完成 P2；P4 的全部媒体动作、推荐、详情分区和播放器，P5 的任务与刮削，以及 P6/P7 全量回归、真机与发布切换不提前标为完成。

## 状态与请求归属

- URL 是筛选唯一来源。离散筛选 push，搜索 400ms 防抖 replace；离开/切筛选时清定时器。库列表读取后清理失效的 library_id，保留其它参数。
- `media-wall` store 持有分页结果、图片标签、总数、加载/失败状态和单个快照。只在历史恢复时复用相同 query、未 dirty/failed 且不足 60 秒的快照；否则按历史条目保存的条数重新读取。
- 页面的请求域持有库列表/详情 GET；store 的分页请求同时检查页面 signal 与请求代次。切页停止读取；成功短写操作，包括 204，统一使快照 dirty；失败写入与读取不置 dirty。认证 token 变化清空快照。
- 只向 `history.state.__embyView` 写入 scrollY/wallCount/focusID，保留 Router 的 back/current/forward/position 等字段。渲染完成后恢复焦点和滚动，不依赖 KeepAlive。
- 当前会话曾访问上一历史位置时，详情返回走 Router back；直开/刷新后的详情 replace 到媒体墙，保留筛选，避免离开控制台。
- 详情标题由页面 emit 给布局，业务内容由 Vue template 渲染；没有 innerHTML/v-html 或旧脚本修改 Vue 节点。图片一次回退后固定占位。通知 3.2 秒开始退场，260ms 后移除；销毁清理定时器。

## 布局与本阶段边界

原全局 CSS 与冻结版本字节一致，未加 scoped。MediaGrid/MediaCard 与 ItemHero 使用原 wall/item-hero 容器；P2 筛选及加载文案暂留页面，完整 WallFilters/WallLoadState 在 P4 扩充时再按 P0 清单拆出。

P2 有意只展示最小媒体内容：卡片没有播放/探测/重读/删除动作及用户数据徽章，详情仅 hero/剧情/基础元数据/源路径；尚无完整详情操作、展开区、推荐和播放器。保留媒体墙原库/状态/搜索/排序结构；刮削与实体筛选的完整可操作 UI 留到 P4。P2 截图只作为核心通路证据，不声明全页面视觉等价。

## GL 验收映射

| 编号 | 证据 |
|---|---|
| GL-01/13/34/35 | auth.test.ts 的认证、单跳转、旧 token 隔离；core-path.spec.ts 的初始化、网络失败重试和 401 重登 |
| GL-02 | main.ts 设置 manual；真实浏览器断言 |
| GL-03/05/28 | view-history.ts 保存独立命名空间；media-components.test.ts 保留 Router 字段；真实 300/250、前进后退 |
| GL-04 | Router 唯一导航入口；300/250 请求计数：无重复分页，返回新增分页 0 |
| GL-06/22 | route meta 高亮媒体墙/详情；页面 emit 有效详情标题；浏览器检查 h1，标签页标题沿用入口 |
| GL-07/15 | 详情返回按钮/Escape 与直开/刷新详情的媒体墙兜底；弹层优先级待 P4/P5 加入对应浮层 |
| GL-12/27/30 | 页面取消/代次单测与真实浏览器迟到详情、迟到分页不覆盖后来页面/筛选 |
| GL-14 | API client 读取/写入请求域单测；浏览器 push/replace、离开取消防抖 |
| GL-16/24/25/26 | 通知定时器/卡片键盘/快照有效性单测；浏览器无效库清洗与位置恢复 |

## 验证结果

| 检查 | 结果 |
|---|---|
| strict 类型检查 | 通过 |
| Vitest | 87/87；包含真实混合大小写 DTO、分页失败/重试、过期/dirty 快照、迟到结果、短写 204 失效、卡片/图片/历史/通知 |
| 真实 embedui Playwright | 最终构建 20/20；桌面/手机各 10 项，使用临时 SQLite、独立 Redis 与本地媒体源 |
| 构建脚本 Node 测试 | 11/11 |
| 冻结旧 JS 回归 / 隔离工具测试 | 17/17 + 4/4 |
| Go 静态资源针对性测试 | 默认与 embedui `Test(WebUI\|CachePolicyHeaders)` 均通过 |
| Go vet | 默认与 embedui 均通过 |
| 干净临时源码构建 | 无 frontend/web_dist 的默认 Go 构建通过；无资源 embedui 正确失败；真实 Windows/Linux amd64 交叉编译通过，Linux 未运行 |
| 安全 Go 全包回归 | 非全绿：TestMediaProbe 缺 reframes；TestPollingMonitorDebouncesRetriesAndTracksMoves 目录事件断言失败。P1 已记录同类失败。本轮 nfo 包通过；排除危险 TestRedisBehavior，未记作通过 |

第一轮 E2E 有三项断言失败：无效库参数已经正确清洗，而登录 helper 仍等待原 URL；Vue Router 保留 returnTo 中的斜杠，而断言固定为 `%2F`。已按行为契约改为最终清洗地址/解码参数断言，重建后完整 20 项通过。未为满足测试改变 URL 行为。

浏览器证据保留于 `frontend/test-results/`、`frontend/playwright-report/`，300/250 截图及请求记录另存 `vue-migration/artifacts/P2/`。截图等待面板动画结束并遮住临时源路径。阶段 Go 回归报告为 `artifacts/P2/go-regression.json`，P1 原报告保留。生成产物和证据不入库。

最终静态文件：HTML 0.53 kB，CSS 29.12 kB（gzip 6.69），JS 134.52 kB（gzip 51.55），另有 favicon；未加载 Artplayer。

## 可重复命令与接续点

```powershell
npm --prefix frontend run typecheck
npm --prefix frontend run test:unit -- --run
npm --prefix frontend run test:build
npm --prefix frontend run build
$env:PW_CHANNEL = 'msedge'
npm --prefix frontend run test:e2e
node --test internal/server/web_test/app.test.cjs vue-migration/tools/live-env/live-env.test.mjs
go test ./internal/server -run 'Test(WebUI|CachePolicyHeaders)' -count=1
go test -tags embedui ./internal/server -run 'Test(WebUI|CachePolicyHeaders)' -count=1
go vet ./...
go vet -tags embedui ./...
npm --prefix frontend run test:go-build
$env:EMBY_MIGRATION_PHASE = 'P2'
npm --prefix frontend run test:go-regression
Remove-Item Env:EMBY_MIGRATION_PHASE
```

末条回归命令目前非零，原因如上。下一阶段按 VUE-06/07 接管理基础页面；扫描/重建长请求必须归应用任务模块，不能挂到页面卸载取消。保持只读设置和真实接口语义，不补造写接口。

旧 `internal/server/web/` 与 `web_test/` 继续冻结；未 commit/push，未部署，未切换正式入口。五个平台发布与 Linux systemd/回滚仍待 P7，手机视口模拟不代表真机验证。
