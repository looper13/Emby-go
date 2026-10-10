# P0 隔离环境与真实浏览器采集

更新：2026-10-10。范围：P0/VUE-01 的真实基线，以及供 P1-E/VUE-17、P2 复用的隔离环境。**本轮采集已完成；不等于整个 Vue 迁移或全部 P0 行为已验收。**

## 本次结果

- 一条 Node 命令连续运行两轮，均完成：真实 Go + SQLite + 独立 Redis + 媒体 HTTP 源；旧前端真实 UI 登录；加载 300 张卡片；进入第 250 条；浏览器返回；再检查 3 条 pending。
- 每轮覆盖 1280x800、390x844，共 4 次主链路；ID 顺序、焦点、scrollY、卡片阅读位置均恢复；重复分页 0，返回分页 0。
- Redis 启动后、Go 启动后、真实 UI 登录后共 3 类注入故障均通过清理。浏览器故障保留脱敏 trace 和失败截图。
- 旧前端 Node 回归 17/17、新工具单元测试 4/4 通过。未重跑全量 Go 测试，不沿用旧全量结果作为本轮通过证明。
- `internal/server/web/`、`internal/server/web_test/` 起止逐文件 SHA-256 相同；旧 16 张截图未改。未调用 6379、18080 上的任何 API，也未发送真实用户写请求。

## 复用 API

入口：`vue-migration/tools/live-env/runner.mjs`（re-export），或 `environment.mjs`。纯 Node，不需要 Bash，不修改 frontend package 或 E2E 配置。

```js
import { startLiveEnvironment } from './vue-migration/tools/live-env/runner.mjs';

const env = await startLiveEnvironment({ binaryPath: '/absolute/path/to/test-binary' });
try {
  // env.baseURL: 本次动态端口上的真实 Go 服务
  // env.credentials: { username, password }，一次性凭据，仅保存在内存
  // env.root: 独立临时目录；env.mediaURL: 可控媒体源
  // 调用方在这里自行创建浏览器，并通过实际登录页登录。
} finally {
  // 先关闭调用方拥有的 browser/context，再关闭环境。
  const cleanup = await env.close();
  // cleanup.rootRemoved、processes[].exited、ports.*.released、errors
}
```

`binaryPath` 可传主线程 `go build -tags embedui` 的产物。环境只复制二进制到自己的目录，不修改或删除传入文件。省略时执行 `go build -o <temp>/emby-go-live.exe ./cmd/metatube`。还支持 `redisPath`、`goPath`、`ffprobePath`；`faultAfter` 仅用于测试（root/redis/media/go/seed）。

附加能力：`request(path, options)` 仅访问本实例，写前验证随机 ServerId；`redact(text)`、`addSecret(value)` 可用于调用方证据脱敏；`diagnostics()` 返回已脱敏的自有服务日志。不要把整个 `env` 或 credentials 序列化进测试报告。

外部 import + `binaryPath` 已单独实测，`close()` 调用两次结果一致，调用方原始二进制 SHA-256 不变。此证明使用旧测试二进制的只读副本，不代表已经验证主线程的新 Vue/embedui 页面。

## 运行命令

在仓库根目录的 PowerShell 执行：

```powershell
$env:PW_CORE = (Resolve-Path 'frontend/node_modules/playwright-core').Path
node vue-migration/tools/live-env/runner.mjs --runs 2 --fault-check
node --test internal/server/web_test/app.test.cjs vue-migration/tools/live-env/live-env.test.mjs
node vue-migration/tools/live-env/audit-evidence.mjs vue-migration/artifacts/live-env
node vue-migration/tools/live-env/audit-evidence.mjs vue-migration/fixtures/json
```

也可传 `--binary <absolute-path>`、`--redis <absolute-path>`、`--playwright <playwright-core-package-dir>`。浏览器默认使用本机 Edge headless；`PW_CHANNEL` 可改为已经安装的其它 Chromium channel。依赖解析先用指定路径，再尝试 frontend 的 playwright-core、旧 temp 下已安装的 playwright-core、常规 Node 解析；不会自动安装或下载。环境本身不依赖 Playwright，只在调用浏览器采集时需要它。

本次实测 Node 24.14.1、Playwright 1.64.0、Edge 147.0.3912.60，使用 ffprobe 和仓库的 `internal/server/testdata/probe-sample.mp4`。Sandbox 曾拒绝 `spawn`，正式测试经 require_escalated 执行；不得把 EPERM 当浏览器通过。

## 证据位置

最终版本通过的完整运行时间：2026-10-10 11:20:57 至 11:22:36（Asia/Shanghai；文件名为 UTC）。显式使用主线程已安装的 `frontend/node_modules/playwright-core`。此前 11:10:16 至 11:11:56 的两轮成功证据也保留，但以下以最终复验为准。

- [完整报告](../artifacts/live-env/2026-10-10T03-20-57-718Z/report.json)
- [第一轮桌面请求日志](../artifacts/live-env/2026-10-10T03-20-57-718Z/run-1/desktop-requests.log)
- [第一轮手机请求日志](../artifacts/live-env/2026-10-10T03-20-57-718Z/run-1/mobile-requests.log)
- [第一轮桌面 trace](../artifacts/live-env/2026-10-10T03-20-57-718Z/run-1/desktop-trace.zip)
- [第一轮手机 trace](../artifacts/live-env/2026-10-10T03-20-57-718Z/run-1/mobile-trace.zip)
- 第二轮同名证据位于 `../artifacts/live-env/2026-10-10T03-20-57-718Z/run-2/`。
- [浏览器失败 trace](../artifacts/live-env/2026-10-10T03-20-57-718Z/fault-browser/desktop-trace.zip)
- 补采的 8 份 JSON 位于 `../artifacts/live-env/2026-10-10T03-20-57-718Z/fixtures/`，不替换原 29 份契约夹具。
- 每轮 12 张截图位于 `../screenshots/live-runs/2026-10-10T03-20-57-718Z/run-1/`、`run-2/`，故障截图位于 `fault-browser/`。

trace、日志、运行 JSON 及新截图均在 `.gitignore` 覆盖的 artifacts/screenshots 内，不在 tools 下。此 notes 与工具源码可入库；本线程未执行 git add/commit。

## 请求计数与恢复

计数区间为每个全新浏览器 context 的「登录 → 总览 → 媒体墙 → 300 卡 → 第 250 条详情 → 返回」。pending 筛选检查在此区间之外。浏览器 request/response 事件与 Go Gin 日志分别计数，query 参数排序归一化后逐项相等；若日志解析不到请求，直接失败，不报告虚假的 0。

| 项目 | 轮 1 桌面 | 轮 1 手机 | 轮 2 桌面 | 轮 2 手机 |
|---|---:|---:|---:|---:|
| 加载/返回卡片 | 300/300 | 300/300 | 300/300 | 300/300 |
| 第 250 条 ID / 返回 focusId | 55/55 | 55/55 | 55/55 | 55/55 |
| 离开/返回 scrollY | 15684/15684 | 37035/37035 | 15684/15684 | 37035/37035 |
| 分页请求 offset=0/100/200 | 各 1 | 各 1 | 各 1 | 各 1 |
| 重复分页 / 返回分页 | 0/0 | 0/0 | 0/0 | 0/0 |
| 主链路业务请求总数 | 15 | 15 | 15 | 15 |
| 页面 JS 错误 / 外部请求 | 0/0 | 0/0 | 0/0 | 0/0 |
| 媒体墙横向溢出 px | 0 | 0 | 0 | 0 |

其它业务请求：`/api/admin/libraries` 共 3 次（总览、入墙、返回）；`/api/auth/status`、登录 POST、`/Users/Me`、`/api/admin/scan/progress`、`/api/admin/probe/media/progress`、`/api/admin/status`、`/api/admin/items?status=success`、`/api/admin/items/55/detail`、`/Items/55/Similar?Limit=12` 各 1 次。全部 200。

支持 GL-02/03/04/05/09/25/28/34 对应的这条主链路，不扩张为其所有分支已覆盖。

## 数据与媒体源

每次从零创建 307 个条目：L001..300 为普通 NFO+STRM；L301..303 无 NFO，pending；L304..305 为指向环回地址的 FTP 源；L306 为 HTTP 503；L307 为可探测媒体。合计 304 success、3 pending、0 incompatible、0 failed。

中文、特殊字符 `《》&"'<>`、缺图，以及 6 个有图条目均有播种。默认日期倒序的第 250 条是 ID 55，特意有图，以检查详情图片实际解码。图片是确定性生成的测试 PNG，不是外部媒体。没有设置外网 URL 或真实库路径。

FTP 有 NFO 因而仍计入 success；真实详情返回 `playable:false`。不能把 `incompatible:0` 解释为 FTP 可播放。

媒体服务只监听 `127.0.0.1` 动态端口：

| 路径 | 行为与实测 |
|---|---|
| `/sample.mp4` | GET/HEAD；9260 bytes；Range `0-31`、`-16`、`9250-` 均 206，Content-Range/长度正确 |
| `/sample.mp4` 非法范围 | 超界、逆序均 416；单元测试还覆盖空、零长度后缀、多范围拒绝 |
| `/redirect.mp4` | 302 到同源 `/sample.mp4`；带 Range 跟随得到 206 |
| `/delay.mp4?ms=350` | 延迟上限 5000ms；最终两轮实测 367ms、364ms |
| `/failure.mp4` | 确定 503；真实 Go 单条 probe 返回 502 错误体 |
| `/disconnect.mp4` | 断开 socket，客户端请求拒绝 |
| `/health`、`/stats` | 就绪与只读媒体请求记录 |

真实 Go 对 L307 的 probe 成功，返回 info/files 并仅写回本次临时媒体目录。采集不运行真实库扫描、重建、刮削或真实任务写接口。

## 生命周期与清理保证

- 每次 `mkdtemp` 创建新的 `emby-go-vue-live-*` 目录；不会复用历史 SQLite/Redis/token。测试开始时不遍历并删除其它环境。
- Redis、Go 使用动态未占用端口；显式排除 6379/18080 及旧测试 6391/18098/18099。Redis 只绑定 127.0.0.1，有随机密码，关闭持久化，数据库目录在本环境下。
- 子进程使用 `child_process.spawn`，`windowsHide:true`、`shell:false`；解析 Scoop shim 得到真实 executable，避免多余 launcher。Redis 使用相对配置/数据路径，兼容本机 Cygwin 发行版。
- 就绪顺序：Redis AUTH+PING；媒体 IPC 返回已监听端口；Go Public Info 的随机 ServerId 匹配；全新管理员初始化与登录；添加唯一临时库并扫描。
- `close()` 幂等，逆序停止仍由本次持有的子进程。Windows 仅以这些 PID 的 taskkill `/T /F` 清理子树（含可能的 ffprobe），不按进程名全局杀 Redis。
- 停止后检测各端口不再监听；随后校验真实目录父路径、固定前缀、非符号链接和随机 `.owner` 标记，再删除本次目录。清理失败抛出含 `cleanup` 的异常，保留目录，不静默宣称成功。
- 启动任一步骤抛错会内部调用 close；runner 的成功/失败均有 finally；浏览器 context/browser 由采集器 finally 关闭，导入 API 的调用方需自己关闭浏览器。SIGINT/SIGTERM 会触发环境清理。
- 不保证断电、强制终止 Node 或操作系统崩溃时能执行 finally；本轮发现的未处理 Promise 已修正。此类不可捕获终止后需要基于 PID/ExecutablePath/命令行重新核验，不能用宽泛删除/杀进程恢复。
- Go 当前配置只能输出 `:port`，**Go 自身不是仅环回绑定**。runner 只访问 127.0.0.1，但不声称已改变底层监听语义；未修改后端或防火墙。需要严格网络隔离的 CI 应放入受控网络环境。

最终两轮正常清理：Redis/media/Go 端口分别为 `56709/56712/56713`、`51259/51262/53816`，全部 released，全部 PID exited，rootRemoved=true。

最终故障清理：Redis 阶段 `54410`；Go 阶段 `54468/54483/54484`；浏览器阶段 `54503/54515/54546`，全部释放且目录删除。外部 import 的附加检查为 `50262/50276/53040`，同样全部释放。收尾再读监听状态，本次所有端口均无监听，真实 6379/18080 仍为原 PID 14048/35412。

## 脱敏与旧成果

旧目录 `$env:TEMP/emby-go-live-vue01` 的 29 份 JSON 均能解析，16 张 PNG 的尺寸符合桌面 1280x800、手机 780x1688（390 CSS px、DPR 2）。旧 shots 脚本只测了首批 100 条，没有完成 300/250 验证，不能从这些产物推断中断任务已完成。

只在重新核对 PID/ExecutablePath/命令行后，停止旧 Go PID 8468、专用 Redis PID 49940、媒体 Node PID 25788；Redis shim PID 32328 已随子进程退出。**旧目录和数据保留，不取旧 token**。真实 Redis PID 14048 的 6379、真实服务 PID 35412 的 18080 未操作。

原契约夹具不重采：仅 11 份 JSON 将旧本机临时目录前缀替换为 `TEMP_LEGACY`，保留响应字段、ID、时间、状态和数据；原 token 占位仍为 `TOKEN`。脚本 `sanitize-legacy-fixtures.mjs` 是可重复的结构化机械脱敏，第二次不产生改动。

新增证据的 redaction 规则：

- 一次性用户名、密码、Redis 密码、已知 token 替换为 `REDACTED`；Go 的 48 位十六进制登录 token 通用替换为 `TOKEN`，覆盖跳转丢失登录响应的情况。
- 临时根路径的 Windows、斜杠、JSON 多层转义、Cygwin 变体替换为 `TEMP_LIVE`；带 token 的图片 query 替换为 `TOKEN`。
- 不持久化浏览器 storageState、请求头或原始登录 JSON。trace 在 temp 内生成，逐个 ZIP 文本条目脱敏后才导出；原始 trace 随 temp 清理。
- trace 禁止内嵌截图，避免密码输入过程烘焙进图像；独立截图在登录输入前或认证后拍摄，失败图遮盖 input，媒体墙路径节点遮盖。本机路径出现在老截图的情况属于旧证据，本轮未篡改老截图。
- 审计解压 ZIP 文本资源，检查 token、临时用户名、Windows/Cygwin 用户路径、Pw 字段。2026-10-10 最终审计 artifacts 47 个文件（含 230 个 ZIP 条目、29 份 JSON），原 fixtures 29 份 JSON，均无匹配残留；旧夹具脱敏脚本再次运行 changed=[]。该审计不等于通用 PII 检测，应只对受控合成数据使用。

## 明确缺口

1. 390px 的旧详情页右侧操作区裁切；原 `mobile-item-detail.png` 与新 `mobile-detail-250.png` 均可见。本轮记录但不改冻结 CSS/DOM；媒体墙无横向溢出。
2. Range/302/delay/failure 已验证媒体源和真实 probe，不等于 Artplayer 真实播放、自动播放、全屏或代理回退全部通过；手机为 Edge 移动视口模拟，不是真机。
3. 尚未覆盖全部页面动态态、过期/失效快照、全局并发 401、迟到请求、CT-03 写入语义、任务取消与所有 GL 分支；不冒充完整 P0/P1-E/P2 毕业。
4. MetaTube 外部刮削未配置、未接入；未验证 Vue 页面、embedui 发布/Linux 部署或跨平台运行，这些由后续迁移任务推进。
5. 前期出现 EPERM、Redis Cygwin 绝对路径解析失败、UI 登录响应体导航竞态。失败记录归档 `artifacts/live-env/development-attempts/`；它们不计入两轮成功。响应竞态那次遗留目录在确认自有进程已退出、三个端口关闭后单独清理，旧 temp 未删除。

本轮修改范围：`tools/live-env/**`、本 notes、`fixtures/json/**` 的必要脱敏/说明，以及忽略的 artifacts/screenshots。工作树中 frontend、Go 静态服务、构建、README 等其它改动来自主线程，未覆盖或回退。
