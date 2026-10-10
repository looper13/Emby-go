# P7 性能、正式入口与发布验证（进行中）

2026-10-10，接续 P6。VUE-15 已完成；VUE-16 已在源码切换正式入口并生成五平台发行验证产物，Windows 回滚与恢复 Vue 已通过。用户确认目前没有 Linux 测试环境，Linux systemd 冒烟及 Linux 回滚保留待验。本轮按用户授权提交到本地 Git，未 push、未部署；不把 P7 或整体迁移标为完成。

- [x] VUE-15：请求计数、历史返回、图片尺寸、大库、长任务、资源体积、50 次切页/20 次播放器开关、真实 chunk 404。
- [x] 正式入口：GET/HEAD `/`、`/web`、`/admin`、`/web/index.html` 返回 Vue 外壳，no-store；前两者认证后进入 `/admin`，其余保留合法目标。`/admin-vue` 307 重定向，保留 query/fragment。API 与未知协议错误不被 HTML 吞掉。
- [x] 当前源码的干净导出：无 node_modules/生成 UI 默认 Go 构建通过；`npm ci`、类型检查、真实 Vue 构建后生成五平台 `embedui` 二进制，每份通过 `go version -m` 检查构建标签并记录 SHA256。
- [x] 最终 Windows 发行二进制的完整桌面/手机回归：68/68 执行项通过，2 项大库由独立性能套件覆盖；旧二进制回滚后重新登录、媒体墙/详情浏览、刷新恢复 Vue，独立发布套件 6/6 通过。
- [ ] 提交后真实干净检出验证（已获用户授权本地提交，既有验证为干净源码导出）。
- [ ] Linux systemd 冒烟、旧二进制登录/媒体墙/详情回滚演练。
- [ ] Linux 验收后移除旧 UI 及已被替代的 VM/DOM harness，共享图标/播放器先确认迁移。

## 改动及原因

`web_ui.go` 统一服务正式页面和资源；迁移入口改为重定向，GET/HEAD 一致。Vue Router 在 `/`、`/web` 认证后归一到 `/admin`；移除控制台中的“旧版控制台”链接。浏览器场景改用正式地址，补旧 query/hash、迁移地址重定向及 390/768/1280 浮层命中/溢出检查。

真实资源计数发现 **Artplayer 5.4.0 每次 loadedmetadata 注册两条匿名 document 全屏监听器，destroy 不清理**。首次泄漏检查失败，第二次打开/关闭就比基线多 `fullscreenchange`、`fullscreenerror`。仅在 Vue 自有 vendor 副本中给这两个回调命名，并在 destroy 时调用 off；保留原版本与许可证，冻结旧 bundle 未改。补丁与 SHA256 见 `frontend/src/vendor/README.md`。修复后桌面/手机各 20 次开关，window/document 监听器、timeout/interval 回到基线，视频节点归零。

隔离环境增加可选 307–10000 条夹具（默认仍 307），用于大库测量；允许在已拥有的临时服务停机后替换二进制，以同一临时 SQLite/Redis 演练回滚。构建脚本 `verify-release.mjs` 从当前源码导出到新临时目录，安装锁定依赖并跑类型/单元/构建检查，复用一次前端产物交叉编译五平台，并从固定的迁移前提交 **5c457893698e74928277d013931f55a2aafb1c84** 的归档另编译 Windows/Linux amd64 旧 UI 二进制（可用 `EMBY_RELEASE_PREVIOUS_REF` 指定其他基准）。提交新版不会改变回滚基准；Linux 旧产物仅准备、未运行。

## 性能与泄漏观察（真实 embedui 服务）

独立 Edge/Chromium CDP 测量，两种视口，临时 8000 条数据库、本地媒体源；未使用用户媒体库、私人配置或在线密钥。测量机器为 Windows x64 / i7-13650HX（20 逻辑核）/ 63.7 GiB，Edge 147.0.3912.60，Node 24.14.1；机器记录见 `artifacts/P7/machine.json`。数据随已加载条数增加，未将 8000 条一次性塞进 store/DOM。

| 场景 | 结果/观察 |
|---|---|
| 8000 库分页到 1000 条 | 桌面/手机均恰好 10 次请求，offset 0..900，各页一次、ID 无重复 |
| 图片预留尺寸 | wall-poster aspect-ratio 2/3，lazy 图片，宽高 >0；缺图也有同尺寸容器 |
| 首次加载资源 | JS 231.29 kB / gzip 82.11 kB；Artplayer 独立动态 chunk 133.51 / 36.29 kB，关闭前不会重复插入旧脚本；CSS 29.12 / 6.69 kB |
| 大库长任务 | 桌面 2 次（50/55ms），手机 2 次（53/82ms）；记录为观测，不制定虚构百分比预算 |
| 大库堆/DOM | 100→1000 卡，桌面 JS 堆 6.20→30.39 MB，手机 6.22→30.01 MB；CDP Nodes 约 12042→114652（含文本/SVG/子节点），随当前列表规模增长，未观察重复页或每页越来越慢的持续长任务 |
| 50 次导航 | 媒体墙卸载后 window scroll 监听归零；固定 100 卡的连续采样 Nodes 约 12046、JSEventListeners 667，堆约 7.7–8.0 MB，未随导航次数持续增加 |
| 20 次播放器开关 | 关闭后的 window/document 监听器 18、timeouts/intervals 0；每个周期与暖基线严格相同；CDP Nodes 桌面 691/手机 690、JSEventListeners 64 稳定。第 10→20 次堆桌面 5.73→5.77 MB、手机 5.72→5.75 MB；模块/JIT 暖机增长单独观察，没有播放器/监听器持续残留 |
| chunk 升级缺失 | 拦截真实 Artplayer 动态 chunk 为 404，显示刷新提示，保留当前搜索输入，不强制整页刷新 |
| 300/250 返回 | 复用正式入口全量回归：条数、焦点及位置恢复，重复/返回分页请求 0 |

性能独立套件 **6/6 通过**。原始测量已保存在忽略目录 `artifacts/P7/performance-report/{desktop,mobile}-{large-library-observations,resource-trend}.json` 与同目录 HTML 报告。全面基线对比、硬资源预算、虚拟化没有被触发；目前不引入额外架构。

## 验证记录

| 检查 | 当前结果 |
|---|---|
| TypeScript / Vitest / Node build | 通过 / 189/189 / 11/11 |
| 最终发行版浏览器全套 | 68 通过、2 大库场景跳过（已在独立性能套件 6/6 覆盖），6.0 分钟，无失败；桌面 34 + 手机 34，实际 Windows 发行二进制，临时 SQLite/Redis/媒体与刮削源 |
| 发布/回滚独立套件 | 6/6；正式四入口及两条认证路径、旧书签与迁移入口重定向、390/768/1280 浮层、同数据库旧 UI 登录浏览及恢复 Vue |
| 冻结旧场景 | 17/17 通过；web/、web_test/ 未写入 |
| Go vet | 默认与 embedui 均通过 |
| Go 正式入口/缓存 | 默认与 embedui `Test(WebUI\|WebIndexCompatibility)`、embedui `TestCachePolicyHeaders` 通过 |
| 五平台构建 | linux/amd64、linux/arm64、windows/amd64、darwin/amd64、darwin/arm64 均 embedui、CGO=0；干净源码副本的 189 单测/11 构建测试也通过；仅 Windows 运行，Linux/macOS 未运行 |
| 安全 Go 全包 | 14 包通过、1 无测试包跳过、server 包失败。`TestMediaProbe` 既有 ffprobe `<reframes>` 差异；`TestProbeCircuitBreaker` 在并发浏览器负载下 60s 超时（P5 已记录过同现象），单独复查 3.067s 通过。全包失败仍保留，不记全绿；明确排除危险的本机 Redis DB15 测试 |
| 隔离环境/缺资源 | live-env 4/4；干净源码默认 Go 构建通过、缺资源 embedui 正确拒绝、真实资源 Windows/Linux amd64 交叉构建通过 |
| 本地构建脚本 | Git Bash `build.sh`、Windows `build.bat` 均实际执行成功；分别检查生成的 Linux/Windows 产物带有 embedui 构建标记 |
| CSS 冻结 | style.css 与 legacy.css SHA256 均 `2F8FB7F756C0BC96BBB856E4CB76CF19E06CDDA762F4F48F5CD5D597F0A73DF3` |

## 命令与待验收口

在 Windows PowerShell 执行；不要使用 WSL 启动 Win32 测试进程：

```powershell
npm --prefix frontend run test:release
$env:PW_CHANNEL='msedge'
$env:EMBY_E2E_BINARY=(Resolve-Path vue-migration/artifacts/P7/release/emby-go-windows-amd64.exe).Path
$env:EMBY_E2E_ROLLBACK_BIN=(Resolve-Path vue-migration/artifacts/P7/release/emby-go-previous.exe).Path
npm --prefix frontend run test:e2e -- --output=../vue-migration/artifacts/P7/final-suite
# 大库必须单独运行，避免改变原业务场景的 304/307 夹具断言
$env:EMBY_E2E_MEDIA_COUNT='8000'
npm --prefix frontend run test:e2e -- performance.spec.ts --output=../vue-migration/artifacts/P7/performance
Remove-Item Env:EMBY_E2E_MEDIA_COUNT
$env:EMBY_MIGRATION_PHASE='P7'
npm --prefix frontend run test:go-regression
```

Linux 当前没有环境，用户明确选择保留待验；复验部署时按 `deploy/README.md` 等待业务写入结束、停服备份、保存旧二进制、替换并检查 systemd/认证/媒体墙/详情，再恢复旧二进制重做登录浏览。二进制回滚不撤销已经执行的 NFO/图片/数据库业务写入。

保留旧 `web/` 与 `web_test/` 是为了尚未完成的最终迁移验收；源码正式 HTML 已切换，旧脚本不随 Vue 页面执行。最后一步再移除旧资源处理代码、旧嵌入与被替代的旧 harness，并复验 favicon/播放器/资源缺失/干净构建。当前不得把“Windows 回滚”和“交叉编译”写成 Linux systemd 已验收。

回滚用例初轮旧版登录/媒体墙/详情已通过，恢复 Vue 时因 `page.goto('/admin#/items')` 是同文档 hash 导航而失败。改为显式 reload，发布套件桌面/手机 6/6 通过；二进制替换不会自动替换已打开的浏览器文档。旧版启动另等待总览内容就绪，避免启动阶段导航竞态。

Artplayer 补丁使原先“整个 vendor 与旧版逐字相同”的单测失效；已只对 CSS/favicon 保留逐字断言，播放器检查冻结原始 SHA256 和未变的版本/版权/MIT 声明，生命周期由真实浏览器检查证明。实际 Git Bash `build.sh` 已贯通 npm ci、189 单测、11 构建测试、资源发布与 Linux amd64 编译；这仍是 Windows 上交叉编译，不是 Linux 运行。

干净源码导出新增完整检查时，第一次构建测试在临时 `web_dist.next → web_dist` 重命名遇到 EPERM，原因未确定；独立双构建脚本正常，重新从新临时目录导出后 189/189、11/11、五平台全部通过。记录为一次未复现的环境文件占用/权限现象，不忽略失败、不据此修改发布逻辑。

最终证据位于忽略目录：`artifacts/P7/final-suite/`（浏览器截图、资源趋势 JSON，零失败）、`final-report/`（最终完整 HTML 报告）、`performance-report/`（大库/泄漏原始数据与报告）、`release-e2e/`（独立发布检查）、`release/verification.json`（五平台及旧版产物 SHA256）、`go-regression.json`（保留既有失败）、`verification.json`（汇总）。
