# 上游增量审核与选择性同步（2026-11-10）

## 比较基线

- 上游：[holll/Emby-go](https://github.com/holll/Emby-go)，本轮固定到 [acfc5602938f167cf8acd887aca6a0346cf524d0](https://github.com/holll/Emby-go/commit/acfc5602938f167cf8acd887aca6a0346cf524d0)。
- 上次已审核快照：`8cdcc1bbebd9ab89be306b7d2b55ecfe7b682406`，见 [2026-10-22 审核记录](UPSTREAM-REVIEW.md)。上次使用手工移植，没有把上游提交历史整体 merge，因此不能按共同祖先把那 98 个旧提交再次当作新工作。
- 本轮上游增量：51 个提交，514 个文件，72,192 行新增、6,481 行删除。检查提交主题和文件范围，并逐段检查拟采用的实现及其本地调用方；没有逐行证明整个上游分叉无缺陷。
- 开始时本地 `main=71011f8`，工作区干净，落后自己的 `origin/main` 8 个提交。拉取并确认是快进关系后，将本地快进至 `7f1e1bb`，包含光鸭网盘和最新第三方客户端播放改动，以此作为代码适配起点。
- `origin` 仍为 `looper13/Emby-go`，`upstream` 仍为 `holll/Emby-go`。本轮从上游取参考代码，以手工适配完成选择性同步，没有 merge/cherry-pick 整个上游分支。

## 实际吸收的八项改进

| 改进 | 上游来源 | 本地适配与结果 |
| --- | --- | --- |
| 合法空元数据读取 | `b884f00` | 在 SELECT 投影中给可空字符串、年份、评分、片长默认值，补探测和合集流派读取；保留 TV、背景图、预告片、封面及账号作用域字段。读取不修改数据库，非法非空数值仍报转换错误。 |
| 空 JSON 列快路径 | `4f6fcf9`、`f838113` | 空串直接返回 nil，`[]` 返回非 nil 空切片；保持既有结果形状，减少列表每行的无效 JSON 解析。此项与上一项共同构成元数据读取改进。 |
| 图片标签缓存容量 | `4f6fcf9`、`f838113` | 正、负缓存插入均触发摊销清理；超过 10,240 条时先移除过期记录，再收敛至 8,192。保留本地磁盘版本检查、覆盖同名文件和目录失效机制，命中路径不遍历全表。 |
| 缓存复制锁竞争 | `580c4f0` | 把 Get/Set 的大字节副本复制移到锁外。保留全局精确 LRU、原子 Clear、DeleteMatching、每条字节上限及私有副本契约，没有复制上游的分片配额、View 或所有权转交接口。 |
| FC2 别名精确匹配 | `6d3f9a5` | 比较时兼容完整数字型 `FC2-PPV`、`FC2PPV` 和 `FC2` 表达；不改搜索词或 NFO 写入规则，不放宽未知尾缀。保留原 PPV 前导零严格比较及标准番号补零行为。 |
| NFO 有界普通文件读取 | `6e8eb99` | 引入上游 regularfile 打开检查，统一 NFO 读取、带统计读取、刮削、探测写回、头像写回、字段和列表预览入口，单文件最多 8 MiB。保留本地 XML、电视、多音轨、命名空间及固定条带路径锁。 |
| 空范围探测跳过 | `5cb8891` | 先按现有库能力、状态和数量筛选目标，确实有目标时才检查 ffprobe。空库成功跳过，有目标但二进制缺失仍明确失败。 |
| 探测取消原因与计数 | `6b11d00` | Run/Verify 保留可用 errors.Is 判断的 context 原因；进一步适配本地 probeOne，调用方取消不累计失败样例或连续失败，不触发熔断。实际单条执行超时仍报告失败。 |
| 紧凑媒体信息缓存 | `b9bdcfd` | 新 mediainfo.json 使用紧凑 JSON，保持已有结构、源链接校验、探测版本和原始 ffprobe 信息；旧缩进缓存仍可读取，无需重新探测。 |

表中元数据默认值与空 JSON 快路径是一项改进，其余七项独立，共八项。性能变化没有套用上游机器的基准数字。

NFO 超限作为现有扫描的单文件失败：记录具体 NFO 路径和原因，保留原索引，不提交失败指纹、不执行本轮删除清理；修复源文件后继续重试。拒绝文件大小并不等于远程文件系统 I/O 超时。普通文件检查增加文件属性访问，真实 SMB/NFS 成本未测；Unix 使用非阻塞打开防 FIFO 竞争，非 Unix 只能做打开前和句柄类型检查。

## 没有整体搬入的变化

- 上游原生 JS 管理页、任务中心、监控仪表盘、Web 自升级和发行资产镜像依赖另一套前端、启动布局及升级生命周期；本地继续使用 Vue/embedui、Docker 和自己的任务状态契约。
- 上游新增翻译、受保护封面维护、视频雪碧图/BIF、字幕标记、Webhook 投递历史、IP 封禁、API Key 轮换都需要本地配置/UI/持久化联动，不能仅复制后端文件就认为功能完成。
- 上游 `302-only` 播放会移除当前代理播放能力，与本地按库代理、115/光鸭准备和第三方客户端起播路径冲突。最新提交还删除了上游备份和探针能力，不据此删除本地仍在使用的 CLI 与诊断接口。
- FTS、可见排序索引、写事务闸门和扫描状态表依赖上游迁移编号、实体范围和查询语义。本地已独立实现增量扫描、批量写入、用户/电视/在线资料库和倒排特征；直接替换可能改变搜索匹配、账号隔离或索引字段，暂不整体移植。
- `0f8912f` 修复的是上游 outbound 默认 15 秒响应头预算；本地 MetaTube 使用直接设置 Timeout 的 http.Client，没有该隐藏的 15 秒限制，无需照搬 outbound 模块。
- 上游图片池、持久化缩略图、后台预热、GC 调整、监控分片及零拷贝接口有性能参考价值，但尚未在本地 TV/R18 图片路径、缓存容量和字节所有权上完成证明，不套用其测量或修改本地资源策略。

## 51 个增量提交处置清单

“保留本地实现”表示没有复制该提交的整套功能，不表示本地已包含其所有细节。“参考”不把上游 CI 或验收状态视为本地通过。

| 提交 | 主题摘要 | 本轮处置 |
| --- | --- | --- |
| `1a77249` | 增量扫描、翻译、多用户加固 | 保留本地增量扫描和权限实现；翻译及凭据迁移不搬入。 |
| `2ef6afb` | 管理员凭据生命周期、稳定性演练 | 保留本地账户与任务契约；演练只参考。 |
| `b884f00` | 合法 NULL 元数据读取 | 适配采用。 |
| `bea7a73` | 仪表盘、磁盘图片缓存 | 需 Vue/配置和缓存生命周期适配，暂缓。 |
| `a37217b` | 移动仪表盘与管理页 | 上游旧 UI，不复制。 |
| `36e1eeb` | CI 缓存、浏览器稳定性 | 参考，不替换本地 CI。 |
| `ff6f05e` | Chromium 临时目录清理 | 上游测试工具不同，仅参考。 |
| `ec14882` | 封面维护、STRM 视频预览 | 新模块与 UI/迁移联动，暂缓。 |
| `e09f6dd` | 完整 race 分组和预算 | CI 参考。 |
| `04bda03` | 精简测试夹具与 race 分区 | 上游夹具不同，仅参考。 |
| `8ee608f` | 测试初始化与浏览器复用 | 上游测试结构不同，仅参考。 |
| `990c8d0` | 浏览器启动互斥、测试缓存 | 上游测试工具不同，仅参考。 |
| `0d00876` | v2.2.2 报告修复、安全与活动功能 | 不移植新 IP/审计策略与旧 UI；权限保留本地实现。 |
| `4be5054` | 移动弹窗几何测试 | 旧 UI 测试，不复制。 |
| `34209b7` | 119 文件 Go modernizer | 机械改写不解决本轮兼容问题，不扩大冲突面。 |
| `b610297` | Webhook Close/Send 竞争 | 本地没有对应分发器，不适用。 |
| `580c4f0` | LRU 分片、锁外复制 | 仅采用锁外复制，保留本地容量和清空契约。 |
| `7976fc7` | 图片缓冲池与 singleflight panic | 本地缩略图路径不同；性能策略暂缓。 |
| `f17467a` | 关闭后台任务准入、图片清理 | 依赖上游 worker/维护任务结构，不覆盖本地生命周期。 |
| `1d31a8e` | 扫描事务取消、原子计数、事务卫生 | 上游写闸门和 scan_state 不同，不整体搬入。 |
| `e90e1e3` | 锁外校验封面与雪碧图 | 本地没有对应模块，不适用。 |
| `6b11d00` | 备份竞争、路径锁、探测取消、日志重建 | 采用探测取消并补本地计数处理；路径锁与唯一备份 staging 已有本地适配，不重写日志启动流程。 |
| `a0a50ac` | FIFO 写闸门、Key 回填、播放清理 | 上游凭据/会话与写事务结构不同，暂缓。 |
| `4ec168a` | 监控分片、GC、原子临时文件 | 不调整本地进程 GC 或监控策略；临时文件流程保持现有实现。 |
| `8a74e2a` | 列表分配、DTO 所有权 | 不引入零拷贝所有权接口，保留 TV/R18 与 Managed 缓存。 |
| `4f6fcf9` | 标签缓存容量、空 JSON | 适配采用。 |
| `f838113` | 媒体库排序写闸门、摊销清理 | 采用标签摊销清理和空数组快路径；不复制写闸门。 |
| `8c132e4` | 所有计划任务结果日志 | 保留本地扫描/探测任务日志，不搬入上游新增任务。 |
| `0466639` | 缓存零拷贝 View | 保留私有副本契约，不采用。 |
| `171440d` | 监控时间串复用 | 本地监控结构不同，暂缓。 |
| `0f8912f` | MetaTube 响应头超时 | 本地无同一隐藏超时限制，无需改动。 |
| `b9bdcfd` | 紧凑 mediainfo.json | 采用，验证旧缓存兼容。 |
| `5cb8891` | 空范围先跳过再检查 ffprobe | 适配采用。 |
| `6d3f9a5` | FC2 别名、隐藏 CD 显示尾缀 | 采用 FC2 精确别名；保留本地 CD/多版本显示与播放身份。 |
| `234a220` | Webhook 旧 UI 控件 | 本地无该页面，不复制。 |
| `57a5500` | 手动刮削、Webhook 测试、provider ID | 本地已有手动刮削；新的 provider 编码/旧 NFO 迁移和 Webhook 需联动，暂缓。 |
| `2aeedf0` | 文档清理 | 不覆盖本地 docs 及历史验收记录。 |
| `52836dd` | reports/tools 忽略 | 本地测试证据目录已明确，不复制忽略策略。 |
| `6e8eb99` | 平铺部署、自升级及读取加固 | 仅采用普通文件检查和 NFO 8 MiB 边界，其余部署/预览/字幕标记不搬入。 |
| `25453a9` | 升级旧 UI 查询修复 | 不复制。 |
| `c04091b` | 任务中心、诊断、投递历史、备份 | 保留本地任务/诊断/备份契约，旧 UI 与新迁移不搬入。 |
| `f61840a` | 旧页面测试夹具 | 不复制。 |
| `2b679c9` | 发行镜像配置 | 依赖上游自升级/安装流程，暂缓。 |
| `e902bb5` | 删除上游死代码 | 本地调用方不同，不按上游清单删除。 |
| `1419f1f` | v2.3.4 发布记录 | 仅参考，不代表本地发布。 |
| `0628ea0` | 旧浏览器 hash 测试 | 测试入口不同，不复制。 |
| `54ae1b4` | 任务中心、升级条、仅管理员登录 | 与本地 Vue 普通账户使用契约不同，不复制。 |
| `092d386` | Folder 查询、完整请求 URL 日志 | Folder 需联动本地 TV/R18/库可见性查询；完整 URL 留存不替换当前凭据脱敏策略，暂缓。 |
| `ac6b37a` | 默认 CI 精简 | 本地工具链和发布门禁保持原有。 |
| `e5f73d2` | Emby 批次、302-only、FTS、索引 | 保留本地查询/播放/扫描与迁移；不合入取消代理或 FTS 匹配语义。 |
| `acfc560` | 11 月 10 日 UI、审计与功能删改 | 不替换 Vue、管理员策略、凭据/会话；不删除本地备份与探针，不合入磁盘缩略图预热。 |

## 本地验证

验证日志放在忽略目录 `frontend/test-results/`；不包含真实服务凭据或生产配置。测试使用临时 SQLite、临时媒体目录及夹具。

- 第一轮定向验证通过：cache、metatube、nfo、regularfile、probe、store、server；覆盖副本隔离/并发替换/字节上限、完整 FC2 别名和严格拒绝、NFO 体积边界和修改入口拒绝、NULL 的列表/批量/探测/合集读取及非法非空数据拒绝、空库探测、取消计数、标签清理后同名覆盖失效、紧凑/旧缩进缓存兼容。
- 广泛 Go 回归使用 `go test -json ./... -skip '^Test(RedisBehavior|MediaProbe|ProbeCircuitBreaker)$' -count=1 -timeout=240s`。cache、metatube、regularfile、probe、store、scanner、cloud115、cloudguangya、clouddrive、offline、config、imageutil、avatar、actorassets、alist、gfriends、backup、logging、subtitle、translate、r18、scheduler 通过；scanner 新增的超限保留旧索引/修复后重试用例通过。server 除监听端到端用例外的其余测试通过。
- 此轮暴露一个现有测试仍读取 `../../vue-migration/fixtures/ct/ct-01-number.json`。按项目目录规则修正到 `../../frontend/tests/fixtures/ct/ct-01-number.json`；没有恢复退役目录或修改夹具。修正后该用例及 scraper 整包通过。这是本地验证修复，不计入上游八项改进。
- 广泛回归的 NFO 用例 `TestSaveFileInfoOnlyDirectMovieElement/direct_after_comment`、`TestEpisodeNFOReadSaveAndProbePreserveRootAndNumbers` 出现 Windows rename `Access is denied`；NFO 整包三轮复测又出现一次 `TestSetActorThumb` 改名失败，之后 NFO 整包独立一轮通过。基线 `7f1e1bb` 的 NFO 源码副本先对前两个用例跑五轮、再跑整包二十轮，均通过。本轮没有在基线复现失败，不能以历史记录代替根因证明；保留 Windows 外部占用/时序风险，不宣称全量全绿。
- 广泛回归的 `TestPollingMonitorDebouncesRetriesAndTracksMoves` 发生移入子树归并断言失败，`TestLibraryPollingEndToEnd` 在新增库正在扫描时删除另一库收到 409。监听实现未改动；两项独立复测各一轮通过（librarywatch 1.706 秒，server 34.596 秒），仍保留时序风险。
- `go vet ./...` 通过；Windows 普通和 embedui、Linux amd64 embedui 编译通过，全部使用 Go 1.27.3、CGO_ENABLED=0。Unix regularfile/FIFO 测试已交叉编译成功，未在 Linux 运行。
- 前端类型检查、43 个测试文件的 505 项单元测试、Vue 生产构建通过。构建发布 7 个已校验 UI 文件到忽略的 `internal/server/web_dist`；HLS 分块大于 500 kB 的既有提示不影响构建。
- 不运行 `TestRedisBehavior`（会清理本机 Redis DB15），以及此前记录的 `TestMediaProbe` / `TestProbeCircuitBreaker`；排除不代表通过。CGO 关闭，不声明 race 或 Unix FIFO 运行验收通过。
- 没有生产部署、实际 Emby/移动客户端或 SMB/NFS 验收；Windows/Linux 构建不能代替这些运行验证。

本轮关键日志：`upstream-focused.log`、`upstream-go-regression.jsonl`、`upstream-nfo-repeat.log`、`upstream-nfo-scraper-final.log`、`upstream-watch-repeat.log`、`upstream-baseline-nfo-full.log`、`upstream-typecheck.log`、`upstream-frontend-unit.log`、`upstream-frontend-build.log`、`upstream-vet.log`。所有日志及基线副本/编译产物均位于忽略的 `frontend/test-results/`。

## Git 交付

用户在实现与验证后明确授权本批改动提交并推送。交付范围为上述适配代码、回归测试、审核记录与 README，目标为 `origin/main`（`looper13/Emby-go`）；以包含本记录的 Git 提交及远端 `refs/heads/main` 一致性核验为准。临时测试日志、基线副本、编译产物及真实配置不入库。未部署，真实客户端与 Windows 时序风险仍保留原验证边界。
