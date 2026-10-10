# P3 管理基础页面

日期：2026-10-10。接续根目录迁移计划 VUE-06/07；旧前端及旧回归测试继续冻结，Vue 入口保持 `/admin-vue`。

## 当前任务与功能对照

- [x] VUE-06：总览重建入口、媒体库读取/新增/删除/单库扫描、只读设置、API 密钥读取/创建/复制/删除。
- [x] VUE-07：手动补录、任务日志/刷新/立即扫描、接口探针读取/清空；验证数字与数组、204、失败保留输入、原生确认、重复提交。
- [x] 扫描/重建长 POST 与进度轮询由应用 tasks store 持有；切页继续执行，完成后刷新当时所在页面。
- [x] P4：完整媒体墙及详情动作、分区/展开恢复、实体筛选、推荐、播放器及代理回退；验收见 [P4/progress.md](../P4/progress.md)。
- [ ] P5：批量媒体探测、刮削配置/任务/预览确认、计划任务；P6/P7 全量、真机、部署/正式入口切换。

| 页面 | 保留的行为与接口 |
|---|---|
| 总览 | 原统计和状态 GET；全量重建 POST `/reindex`，顶部增量扫描 POST `/scan`，全局任务锁 |
| 媒体库 | `GET/POST /libraries`，原 `{Name,Path}`；`DELETE /libraries/:id` 只删索引/关联进度收藏；原确认文案、取消不提交；行扫描传 `library_id` |
| 设置 | 只读 `GET /settings`；监听/数据库/缓存/Redis/监控说明；命中统计排除 token/apikey；未增加保存接口 |
| API 密钥 | `GET/POST/DELETE /apikeys`；trim 名称，原返回字段；clipboard 成功/失败通知；原删除确认；撤销后真实客户端鉴权 401 |
| 手动补录 | 原 `POST /items/manual`；空年份 0，其余整数 1900–2100，逗号/中文逗号数组；http(s) 源；源路径/标题必填；成功 reset，失败保留草稿；真实写 .strm 与同名 NFO |
| 任务 | `GET /tasks` 的类型/状态/时间/错误；刷新读取；扫描完成重读当前日志；扫描按钮共享禁用态 |
| 接口探针 | `GET/DELETE /probe`；未实现协议请求可见，DELETE 204 后重读空状态；沿用旧版无额外确认 |

## 请求、状态与组件边界

`useAdminRead` 持有页面 GET、加载/错误/重试和代次。切页取消读取，迟到读取不写回；失败刷新保留旧数据。短写不带页面 signal，不因卸载取消；离开页面后的成功仍可通知，但不重读已销毁页面。创建/补录输入由表单持有，父页面成功后 reset；提交及每行删除有锁。

`tasks` store 持有 scan/reindex POST、进度 GET、任务代次和定时器。一次只读一份进度；正常 600ms，失败 1.2/2.4/4.8s 上限退避，保留最后状态并显示暂不可用。失败读取不误判完成；409 接续服务端已运行任务，真正结束后再使媒体墙 dirty 并刷新页面。自身 POST 成功只发一次完成修订；认证失效/App 销毁清理读写控制器和定时器。浏览器刷新后恢复服务端 running 状态；旧的 finished 状态不主动显示。结束面板 4s 隐藏，walk 为不确定进度，支持取消/错误字段。

任务完成时，仅当前挂载的总览/媒体库/任务/媒体墙/详情重新读取。媒体墙保留已加载数量、滚动和焦点；详情刷新保留旧数据和阅读位置，失败显示通知。已离开媒体墙时缓存仍标 dirty，下次返回重读保存数量。后续 P4 已验证详情展开区、推荐与阅读位置保留。

LibraryForm、ManualForm、EmptyState、ReadState、TaskProgress 独立；简单表格留页面。TaskProgress 常驻 App，读取 store、不发请求。保留原生 confirm，未引入自定义弹层。边界调整已明确写入 [P0/component-split.md](../P0/component-split.md)。全局 CSS 与原版字节一致；无 scoped、v-html/innerHTML 或旧脚本操作 Vue DOM。截图对照桌面媒体库的原 `.panel/.table-wrap/.field-grid`，手机表单与横向导航无整页溢出；待迁移页面仍通过旧版控制台访问。

## 验证与证据

| 检查 | 结果 |
|---|---|
| strict 类型检查 / Vue 构建 | 通过；HTML 0.53 kB，CSS 29.12（gzip 6.69），JS 163.81（gzip 60.74），favicon；未加载 Artplayer |
| Vitest | 104/104；新增 DTO/204/表单数字数组/草稿保留/确认/重复锁/短写切页/只读设置/剪贴板错误，任务非重叠/退避/409/完成一次/登录恢复/销毁 |
| embedui Playwright | 最终构建桌面/手机 32/32；另 4/4 补验精确补录清理、扫描完成后媒体墙重读 300 条/第 250 条焦点与位置、详情自动刷新 |
| Node 构建脚本 | 11/11 |
| 冻结旧 JS / 临时环境 | 17/17 + 4/4 |
| Go 静态资源与缓存测试 / vet | 默认与 embedui 均通过 |
| 干净源码与真实资源交叉构建 | 默认不依赖 frontend/web_dist；缺资源 embedui 正确失败；Windows/Linux amd64 通过，Linux 未运行 |
| 安全 Go 全包 | 未全绿：`TestMediaProbe` 缺 `<reframes>1</reframes>`（已记录的本机差异）；`TestProbeCircuitBreaker` 在全包运行 60s 超时，单独 `-count=1` 复查通过（2.748s）。本轮 nfo、librarywatch 均通过；不修改后端或放宽断言 |

真实浏览器使用临时 SQLite、独立 Redis、本地媒体源。覆盖：管理页桌面/手机截图和导航、只读设置；创建媒体库途中切页 POST 继续、删除保留目录；真实 API 密钥创建→客户端访问→撤销；补录失败保草稿→写盘→进入媒体墙；探针产生→204 清空；扫描切页→所有扫描入口锁→当前任务历史增加→全量重建。补录测试在拥有的临时根目录内写盘，结束删除自己的索引和文件，避免污染后续 300/250 夹具。

GL-11/19/20 的扫描部分由 tasks 单测和真实切页扫描覆盖；GL-13 写入失效与冲突任务实际完成的 dirty 修订保持一致；GL-16 轮询/4s 隐藏/通知定时器随应用销毁清理。探测、刮削对应的 GL 部分仍待 P5。

Go 全包排除危险 `TestRedisBehavior`（清空本机 Redis DB15），未记为通过。完整失败输出保留 `vue-migration/artifacts/P3/go-regression.json`；单独复查不能抹去全包失败。最终完整浏览器报告及遮住敏感字段的截图另存 `artifacts/P3/playwright-report/` 和 `artifacts/P3/browser-results/`，4 项补验报告另存 `artifacts/P3/focused-report/`，保留 P2 证据。生成证据及构建产物不入库。

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
$env:EMBY_MIGRATION_PHASE = 'P3'
npm --prefix frontend run test:go-regression
Remove-Item Env:EMBY_MIGRATION_PHASE
go test ./internal/server -run '^TestProbeCircuitBreaker$' -count=1
```

本阶段之后的 P4 已完成，记录见 [P4/progress.md](../P4/progress.md)；当前下一步是 P5/VUE-11/12/13。未提交、未部署，正式 `/admin` 仍是旧版；手机视口模拟不代表真机。
