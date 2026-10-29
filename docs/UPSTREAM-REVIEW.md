# 上游提交审核与选择性同步（2026-10-22）

## 范围与方法

- 上游：[holll/Emby-go](https://github.com/holll/Emby-go)，固定到 [8cdcc1b](https://github.com/holll/Emby-go/commit/8cdcc1bbebd9ab89be306b7d2b55ecfe7b682406)。
- 本地起点：f9477300c469e2dc649c1d0d6ff864b1b4ce0951；共同祖先：e524c071af8e84130f1a2920ec05886e253a21f5。
- 起点分叉：本地独有 33 个、上游独有 98 个提交。远端 upstream 仅用于拉取；origin 仍为 looper13/Emby-go。
- 已检查 98 个提交的主题、文件范围和与本地实现的重叠，重点逐段审核拟移植路径及其调用方。没有把超过三万行的整个分叉当作已经逐行证明无缺陷的代码。
- 使用最终上游快照与相关提交 diff 手工适配，未 merge/cherry-pick 整个上游。上游的文档验收、覆盖率和 CI 结果不代表本地验证结果。
- 上游移植保留本地 Vue、电视、字幕/音轨、HLS、媒体库代理、多用户/收藏/进度及版本缓存实现。最终交付按用户“都帮我提交”的授权，一并纳入本地前端图片显示/隐藏改动；该功能属于本地修改，不计为上游移植。

## 采用的变化

| 路径 | 来源 | 本地结果与适配 |
| --- | --- | --- |
| MetaTube/刮削 | 66c12e5、9c4fcb0 | 详情最多回退 3 个精确匹配来源，MOSAIC 尾缀识别，环境故障分类；本地磁盘写失败不误触发环境熔断；串行发布并发回调，取消时刷新已写图片缓存，已提交 NFO 完成索引发布。 |
| NFO | dde9d9a、8146c70、9c4fcb0、1b93a06 | 根节点直接 XML 字段更新、扩展命名空间隔离、元数据锁、原子临时文件清理。路径锁用 256 条带避免无界 map，Windows 路径归一；保留 ReadWithStats、电视/流轨/预告片及 FillActorThumb。 |
| 图片 | 8146c70、1b93a06 | 解码前读取尺寸，拒绝超过 5000 万像素，增加 writer 编码入口；保留 GIF/TBN、影片专属图片和现有缩略图实现。 |
| 请求及诊断 | cf0cb44、3499029、39fea7e、3006abc | 请求体预算、登录 IP/账号失败限流、安全响应头、CSP 仅报告；探针过滤扫描噪声、6h 去重/128 条内存界限/落库令牌桶、清空并发序列；探针不保存正文。 |
| 会话凭据 | a75085e | 沿用本地 users/access_tokens，首次启动事务摘要化现存令牌；客户端令牌、用户归属、创建时间、禁用/改密/注销撤销规则不变；数据库摘要不能直接充当凭据。API Key 和会话期限另行保留。 |
| 合集兼容 | 816b470、bbd201f、ca868c3、8cdcc1b | CollectionIds、单片合集归属、ListItemIds 与 Ids 交集、ParentId 外部库 ID、库作用域统计；明确检查原始 Movie 查询结果的用户媒体库授权。 |
| 调度/连接 | 328632c、c1c4bc1 | 相同任务互斥、立即运行忙拒绝；关闭数据库前等待 cron；HTTP ReadHeaderTimeout=10s、IdleTimeout=2min。未替换整个后台任务生命周期。 |
| 运维 | 996447d、751f5e7、0c5f307、f78429d、8fc574c | 公开构建版本；管理员 metrics、内存最近 500 条应用日志；扩展脱敏覆盖带空格引号、Bearer 及 URL userinfo；日志按日期保留默认 0，配置范围 0..3650，重启生效；不搬运旧 UI 或 webhooks。 |
| 备份 | ec49bb7、267f20c | 独立 CLI，SQLite VACUUM INTO + integrity_check、NFO 副本、完成后发布；拒绝覆盖、唯一 staging、输出排除、root 限定清理、有效 manifest 才归属保留策略。 |
| 工具链 | 3749654、e71da49、1d10b2d | 最低 Go 1.26，toolchain go1.27.3；更新 x/net、x/crypto、x/text、x/sys；保留本地依赖。release 更新 Go 1.27.x 并注入 commit/buildTime；build.sh 保留 Vue/embedui 流程。 |

## 审核发现与拒绝原样移植的原因

1. 上游备份会删除固定 staging 和同秒已有目标目录，失败可能损失已有完整备份，并发会互相干扰。采用唯一临时目录和已有目录拒绝；剪裁仅作用于本工具 manifest 归属的目录，备份落在媒体库内时排除输出本身。NFO 仍为逐文件复制，不是文件系统原子快照；不存在的库会跳过。工具不备份演员资料、配置、STRM/视频与 mediainfo.json。
2. 上游 SaveFileInfo 只接受 movie，会破坏本地 episodedetails；上游整文件替换还会移除 FillActorThumb 的只补空值/只读保护。已适配并保留本地函数及电视技术字段。
3. 上游 rootItems 等路径仍出现 UserId 固定为 1 的判断，不能以有多用户提交就假设所有路由均支持本地账户授权。保留本地 request-scoped Store 与播放会话所有者检查，只将合集补充接到这条权限链。
4. 上游旧 JS 管理页、异步扫描 202/task_id、播放事件表、严格 PATCH、恢复列表及统一错误信封依赖另一套前后端/迁移契约。直接合并会覆盖已有 Vue 与任务/取消行为，均未原样引入。
5. API Key 摘要与一次性展示需要联动本地 APIKey 页面；新会话过期/滑动续期、初始化验证码/来源白名单会改变部署与用户策略。本轮只迁移会话摘要，不静默替换其他策略。
6. 出站一刀切私网限制可能阻断 NAS、内网 MetaTube 和 STRM；Redis 可选会改变本地“启动必须可连，运行中失败按未命中且有预算”的约定。保留本地边界。
7. 原始 HTTP 请求 debug 会扩大凭据/正文留存；沿用脱敏日志。图片统一 24h、SQL 投影/候选游标、同系列分层推荐需要对照当前 TV/媒体墙字段与查询/推荐语义实测，暂缓；现有索引、缓存和相似实现保留。
8. Windows FileInfo 懒初始化会让日志目录替换检测失效；移植时先固定文件标识。部分上游测试依赖 Linux 文件名/符号链接权限，已做 Windows 条件适配；无符号链接权限时相关场景会跳过。
9. 未搬运 foreign CI 的 70% 覆盖率基线、旧前端容器/systemd 模板、旧性能/验收数字；本地继续使用 Vue/embedui 发行流程。
10. 上游 IsFatal 只把 401/403 当作 HTTP 环境故障，503 持续不可用时不再熔断；同时把普通解析错误当作网络故障。最终适配为网络错误、401/403/429/502/503/504 参与熔断，500 元数据不完整、404、响应解析和本地写入不参与；取消/请求超时保持不计入。原有“熔断后发布已成功 NFO”回归先失败，修复后通过。
11. 在图片下载或 NFO 写入附近加入取消检查时，必须保留已发生的提交事实。已写图片即使本条随后取消，也返回路径供上层失效缓存；NFO 原子提交后不再将该片改报取消而漏掉索引刷新。图片提交锁同样改为固定 256 条带，避免路径锁表无限增长。

## 升级与验证边界

- 使用 Go 1.27.3；CGO_ENABLED=0。SHA-256 会话迁移在升级启动时生效，旧二进制不能读取摘要化会话；回退需要重新登录。升级前保存数据库和其他部署资料，避免同时运行不同版本服务。
- 备份工具是显式 CLI，不自动定时；日志保留默认不删；运维仅新增后端接口，不增加 Vue 页面。
- 验证结果见下节；没有部署到用户服务，也没有把本地 HTTP 测试当作实际 Emby/电视客户端验收。

## 验证记录

- `go test ./... -skip='^(TestMediaProbe|TestProbeCircuitBreaker|TestRedisBehavior)$' -count=1 -timeout=180s`：最终服务器包通过（111.330 秒），刮削、NFO、扫描、存储、备份、日志及其余包通过；Windows `TestPollingMonitorDebouncesRetriesAndTracksMoves` 在整轮及一次独立整包三轮运行中偶发聚合/改名断言失败。之后该用例独立三轮及监听整包一轮通过。没有把首次全量结果写为全绿。
- 监听代码未改动；起点 f947730 的临时完整源码副本运行该用例五轮通过。本次偶发问题符合既有 Windows 时序记录，但本轮没有在起点副本复现失败，因此不声称已证明其根因。
- NFO、MetaTube、刮削、存储在最终取消/错误分类修改后复跑：MetaTube、刮削、存储通过；NFO 两个 Windows 原子改名用例一轮出现 `Access is denied`，随后 NFO 整包三轮通过。起点副本这两个用例五轮通过；这类外部文件占用仍是未定位的偶发风险。
- 新增/扩展回归覆盖：旧会话迁移与重启/撤销/失败回滚；管理员诊断访问限制、请求预算、Authorization 空白、合集归属及跨库隔离；探针容量/TTL/清空序列；仅精确候选详情回退、503 熔断、并发进度、取消时部分图片提交；NFO 直接节点/扩展/锁/电视技术信息；超大图片头拒绝；备份拒绝覆盖、保留归属及输出排除。
- `go test -tags embedui ./internal/server -run '^(TestUpstream|TestAccount|TestInterruptedScrapeRefreshesSuccessfulMovies|TestTelevision)' -count=1 -timeout=90s` 通过。Windows 普通/`embedui` 编译、Linux amd64 `embedui` 交叉编译、普通/`embedui` 的 `go vet` 通过。未验证 GitHub 标签发布流程。
- 最终一并交付前端图片显示/隐藏开关：Node 24.14.1、npm 11.11.0；类型检查、28 个测试文件的 289 项单元测试、11 项构建测试通过。Vue 生产构建通过，发布 7 个已校验 UI 文件，并使用新产物完成 Windows `embedui` 编译。Vite 提示 HLS 分块超过 500 kB；构建正常完成。冻结的 `legacy.css` 未改动。
- `govulncheck` 使用 Go 1.27.3 扫描：0 个当前调用可触达漏洞，0 个已导入包漏洞；依赖模块 `x/crypto` 有未使用的 `openpgp` 提示（GO-2026-5932），本项目使用 bcrypt，未导入 openpgp。
- 三个排除项：`TestMediaProbe` 为既有 ffprobe reframes 差异；`TestProbeCircuitBreaker` 为既有探测时序异常；`TestRedisBehavior` 会清理本机 Redis DB15，因此未运行。排除不等同通过。CGO_ENABLED=0，未运行 race；真实 Emby/移动设备、生产、SMB/NFS 和 Linux 运行验收未做。
- 备份/日志整包验证通过；本机无创建符号链接权限，`TestBackupSupportsLinkedLibraryRoot`、日志目录符号链接替换/初始链接场景跳过，其他目录替换检查执行。格式检查及 `git diff --check` 通过（使用仓库正常文本归一规则）。
- 本机验证输出：临时目录的 `emby-upstream-final-regression.log`、`emby-upstream-server-regression.jsonl`、`emby-upstream-safety-checks.log`、`emby-all-submit-frontend-unit.log`。交付包含当前后端、前端、配置示例、工具链、测试与审核记录，推送目标为 `origin/main`（looper13/Emby-go），Git 提交记录为交付依据。运行数据、私有配置和生成产物不入库；未部署。

## 98 个提交处置清单

“已有覆盖”指核心能力已由本地独立实现覆盖，不表示附属的审计/旧 UI 等也已全量移植。“暂缓适配”保留为明确差异，不宣称与上游功能全等。

| 上游提交 | 主题 | 处置 | 依据/范围 |
| --- | --- | --- | --- |
| [996447d](https://github.com/holll/Emby-go/commit/996447d) | feat: 管理页左下角展示仓库地址与运行期版本号 | 部分移植 | 采用版本接口；页面底栏不改。 |
| [66c12e5](https://github.com/holll/Emby-go/commit/66c12e5) | fix: 刮削取详情失败时回退到其余精确命中来源，熔断只计环境性故障 | 移植 | 选取兼容代码并补本项目回归，见上表。 |
| [cf0cb44](https://github.com/holll/Emby-go/commit/cf0cb44) | fix: 探针不再记录公网扫描器的探测请求 | 移植 | 选取兼容代码并补本项目回归，见上表。 |
| [28041c3](https://github.com/holll/Emby-go/commit/28041c3) | perf: 媒体墙海报改取缩略图 | 已有覆盖 | 对应核心能力已有本地实现，保留本地账户/缓存/性能/播放约定；不等同包含该提交全部附属能力。 |
| [a8eee3a](https://github.com/holll/Emby-go/commit/a8eee3a) | feat: 控制台换用 Apple TV 设计体系，媒体库支持排序/封面/影片数 | 不移植旧前端 | 作用于上游旧原生 JS 页面，与本项目 Vue 结构不同；不复制文件。 |
| [0b0e5f6](https://github.com/holll/Emby-go/commit/0b0e5f6) | feat: 侧边栏合并为 7 项，状态收敛为 5 档互斥筛选，移除不兼容类别 | 已有覆盖 | 对应核心能力已有本地实现，保留本地账户/缓存/性能/播放约定；不等同包含该提交全部附属能力。 |
| [dde9d9a](https://github.com/holll/Emby-go/commit/dde9d9a) | fix: 代码审查修复——NFO 写入通道锁死、筛选通配符、版本号漂移等 7 处 | 部分移植 | 采用 NFO 临时文件/锁修复；LIKE、版本/删除沿用本地实现。 |
| [8146c70](https://github.com/holll/Emby-go/commit/8146c70) | fix: 修复播放鉴权兼容性、NFO 并发写入和任务状态处理 | 部分移植 | 采用 NFO 路径锁、Authorization 空白兼容；保留本地取消状态。 |
| [9c4fcb0](https://github.com/holll/Emby-go/commit/9c4fcb0) | fix: 修复全项目审查缺陷并新增 NFO 字段级元数据锁 | 部分移植 | 采用 lockdata/lockedfields、并发进度；保留本地用户及缓存实现。 |
| [1b93a06](https://github.com/holll/Emby-go/commit/1b93a06) | chore: checkpoint 40 construction batches before BE-05 | 部分移植 | 采用直接 XML 节点定位/namespace 防串写与图片像素预算；不复制整套 40 批次。 |
| [28ef719](https://github.com/holll/Emby-go/commit/28ef719) | docs: reconcile plan and README with implementation; add status ledger | 仅参考证据 | 文档、CI、验收/基准或测试证据，不把上游通过结论当作本地验收。 |
| [3499029](https://github.com/holll/Emby-go/commit/3499029) | feat(security): BE-05 request budgets and auth rate limiting (batch 41) | 移植 | 选取兼容代码并补本项目回归，见上表。 |
| [03cc265](https://github.com/holll/Emby-go/commit/03cc265) | feat(security): BE-09 出站信任边界与响应头预算 | 暂缓适配 | 出站私网拦截会影响 NAS/私网 MetaTube/STRM 等现有调用；需按来源设计。 |
| [29f1ce4](https://github.com/holll/Emby-go/commit/29f1ce4) | ci: 新增 build/vet/test 与前端 VM 回归工作流（批次 43） | 仅参考证据 | 文档、CI、验收/基准或测试证据，不把上游通过结论当作本地验收。 |
| [b4687d1](https://github.com/holll/Emby-go/commit/b4687d1) | fix(web): 登录失败/忙碌保留输入与焦点（FE-09 部分） | 不移植旧前端 | 作用于上游旧原生 JS 页面，与本项目 Vue 结构不同；不复制文件。 |
| [bd428eb](https://github.com/holll/Emby-go/commit/bd428eb) | fix(web): 筛选/开关/编辑展开补可访问状态（FE-10 部分） | 不移植旧前端 | 作用于上游旧原生 JS 页面，与本项目 Vue 结构不同；不复制文件。 |
| [2eaef83](https://github.com/holll/Emby-go/commit/2eaef83) | fix(web): 遮罩焦点栈与 Esc 层级（FE-04 部分） | 不移植旧前端 | 作用于上游旧原生 JS 页面，与本项目 Vue 结构不同；不复制文件。 |
| [39fea7e](https://github.com/holll/Emby-go/commit/39fea7e) | fix(diagnostics): NoRoute 去重 TTL/LRU 与写库预算（BE-08 主体） | 移植 | 选取兼容代码并补本项目回归，见上表。 |
| [708d4f9](https://github.com/holll/Emby-go/commit/708d4f9) | docs: D02 权限矩阵提案与 D07/D09/D10 默认值（待批） | 仅参考证据 | 文档、CI、验收/基准或测试证据，不把上游通过结论当作本地验收。 |
| [d78d424](https://github.com/holll/Emby-go/commit/d78d424) | docs(security): D02 落地——API 密钥管理员语义（BE-02 风险接受） | 已有覆盖 | 对应核心能力已有本地实现，保留本地账户/缓存/性能/播放约定；不等同包含该提交全部附属能力。 |
| [328632c](https://github.com/holll/Emby-go/commit/328632c) | feat(server): readiness 与有界关闭/后台任务 join（BE-04，批次 49） | 部分移植 | 仅采用请求头超时及 cron Stop 等待；未引入 readiness/完整停机协议。 |
| [cf21d27](https://github.com/holll/Emby-go/commit/cf21d27) | feat(server): 扫描异步任务表 202/task_id/幂等/容量（BE-07，批次 50） | 暂缓适配 | 涉及本地部署策略、API/数据契约、推荐语义或待量化的性能路径，详见暂缓原因。 |
| [eb42f42](https://github.com/holll/Emby-go/commit/eb42f42) | feat(store): DB-02 播放会话/事件表与序列号计数（批次 51） | 暂缓适配 | 涉及本地部署策略、API/数据契约、推荐语义或待量化的性能路径，详见暂缓原因。 |
| [9782d64](https://github.com/holll/Emby-go/commit/9782d64) | test(acceptance): 实时实例验收集骨架与验收矩阵（批次 52） | 仅参考证据 | 文档、CI、验收/基准或测试证据，不把上游通过结论当作本地验收。 |
| [43c9c46](https://github.com/holll/Emby-go/commit/43c9c46) | docs(acceptance): 本地真实二进制冒烟通过 A03/A05/A11/A14 子集（批次 53） | 仅参考证据 | 文档、CI、验收/基准或测试证据，不把上游通过结论当作本地验收。 |
| [a1a3baf](https://github.com/holll/Emby-go/commit/a1a3baf) | ci: gofmt 门禁与 store race；补 D03/D05/D08/D11/D12 提案（批次 54） | 仅参考证据 | 文档、CI、验收/基准或测试证据，不把上游通过结论当作本地验收。 |
| [a0714d7](https://github.com/holll/Emby-go/commit/a0714d7) | test(acceptance): A06 并发计数与 A10 扫描任务幂等（批次 55） | 仅参考证据 | 文档、CI、验收/基准或测试证据，不把上游通过结论当作本地验收。 |
| [f3cfeff](https://github.com/holll/Emby-go/commit/f3cfeff) | test(acceptance): A07 编辑往返与 A09 恢复演练（批次 56） | 仅参考证据 | 文档、CI、验收/基准或测试证据，不把上游通过结论当作本地验收。 |
| [54bd514](https://github.com/holll/Emby-go/commit/54bd514) | test(acceptance): A15 5000 部性能冒烟（批次 57） | 仅参考证据 | 文档、CI、验收/基准或测试证据，不把上游通过结论当作本地验收。 |
| [a796d48](https://github.com/holll/Emby-go/commit/a796d48) | test(acceptance): A02 旧正缓存对照（旧版 200 vs 当前 401，批次 58） | 仅参考证据 | 文档、CI、验收/基准或测试证据，不把上游通过结论当作本地验收。 |
| [71cfe66](https://github.com/holll/Emby-go/commit/71cfe66) | fix(web): 媒体卡片消除嵌套交互，标题改独立按钮（A12/A13，批次 59） | 不移植旧前端 | 作用于上游旧原生 JS 页面，与本项目 Vue 结构不同；不复制文件。 |
| [0c48156](https://github.com/holll/Emby-go/commit/0c48156) | fix(web): 刮削页媒体库选择补可访问名；跨浏览器巡检证据（A12/A13，批次 60） | 不移植旧前端 | 作用于上游旧原生 JS 页面，与本项目 Vue 结构不同；不复制文件。 |
| [c9dc377](https://github.com/holll/Emby-go/commit/c9dc377) | docs(acceptance): A12 功能级断言与 A15 混合负载证据（批次 61/62） | 仅参考证据 | 文档、CI、验收/基准或测试证据，不把上游通过结论当作本地验收。 |
| [3749654](https://github.com/holll/Emby-go/commit/3749654) | chore(deps): 升级 x/net/x/text 修复已知漏洞；CI 增加 govulncheck（批次 63） | 移植 | 选取兼容代码并补本项目回归，见上表。 |
| [f5f860f](https://github.com/holll/Emby-go/commit/f5f860f) | docs(status): D01/D03/D04/D05/D06/D08/D11/D12 按建议默认值批准 | 仅参考证据 | 文档、CI、验收/基准或测试证据，不把上游通过结论当作本地验收。 |
| [e71da49](https://github.com/holll/Emby-go/commit/e71da49) | chore(deps): go.mod 上调 go 1.27.0（CI vuln 发现 Go 1.26 标准库漏洞） | 移植 | 选取兼容代码并补本项目回归，见上表。 |
| [db3b635](https://github.com/holll/Emby-go/commit/db3b635) | feat(auth): 会话期限、滑动续期与撤销全部会话（D01，批次 64） | 暂缓适配 | 涉及本地部署策略、API/数据契约、推荐语义或待量化的性能路径，详见暂缓原因。 |
| [bf78ee0](https://github.com/holll/Emby-go/commit/bf78ee0) | feat(security): D04 初始化验证码、来源白名单与可信代理（批次 65） | 暂缓适配 | 涉及本地部署策略、API/数据契约、推荐语义或待量化的性能路径，详见暂缓原因。 |
| [8499aab](https://github.com/holll/Emby-go/commit/8499aab) | feat(auth): D03 API 密钥摘要化与一次性原文展示（批次 66） | 暂缓适配 | 摘要 API Key/一次性展示需联动现有 Vue APIKey UI；本轮保留原契约。 |
| [2ed5aa8](https://github.com/holll/Emby-go/commit/2ed5aa8) | test(cache): D06 版本域读己之写与脏键验收（批次 67） | 已有覆盖 | 对应核心能力已有本地实现，保留本地账户/缓存/性能/播放约定；不等同包含该提交全部附属能力。 |
| [e999951](https://github.com/holll/Emby-go/commit/e999951) | docs(security): D05 历史凭据审计记录与上线轮换清单（批次 68） | 仅参考证据 | 文档、CI、验收/基准或测试证据，不把上游通过结论当作本地验收。 |
| [b3bd054](https://github.com/holll/Emby-go/commit/b3bd054) | feat(recovery): D08 索引失败恢复列表与超 7 天提示（批次 69） | 暂缓适配 | 涉及本地部署策略、API/数据契约、推荐语义或待量化的性能路径，详见暂缓原因。 |
| [ec49bb7](https://github.com/holll/Emby-go/commit/ec49bb7) | feat(backup): D11 SQLite 快照与 NFO 备份、保留策略（批次 70） | 移植 | 选取兼容代码并补本项目回归，见上表。 |
| [f002e38](https://github.com/holll/Emby-go/commit/f002e38) | docs(acceptance): D12 落地记录与 A01–A15 证据回填（批次 71） | 仅参考证据 | 文档、CI、验收/基准或测试证据，不把上游通过结论当作本地验收。 |
| [ffed71e](https://github.com/holll/Emby-go/commit/ffed71e) | docs(acceptance): 修正 A04 证据测试名 | 仅参考证据 | 文档、CI、验收/基准或测试证据，不把上游通过结论当作本地验收。 |
| [1d10b2d](https://github.com/holll/Emby-go/commit/1d10b2d) | ci: vuln 门禁使用最新 1.27.x 补丁版（修复 stdlib 误报） | 移植 | 选取兼容代码并补本项目回归，见上表。 |
| [45c1344](https://github.com/holll/Emby-go/commit/45c1344) | docs(ci): 记录 GitHub 实跑闭环（批次 72） | 仅参考证据 | 文档、CI、验收/基准或测试证据，不把上游通过结论当作本地验收。 |
| [ca91ae9](https://github.com/holll/Emby-go/commit/ca91ae9) | docs: 清理已完成条目与过期提案（PLAN-STATUS/dev-plan） | 仅参考证据 | 文档、CI、验收/基准或测试证据，不把上游通过结论当作本地验收。 |
| [4e39b8d](https://github.com/holll/Emby-go/commit/4e39b8d) | test(store): 修复 API key 错误凭据断言的随机误报 | 仅参考证据 | 文档、CI、验收/基准或测试证据，不把上游通过结论当作本地验收。 |
| [8a09153](https://github.com/holll/Emby-go/commit/8a09153) | feat(api): v2 契约基线与错误信封测试（批次 73） | 暂缓适配 | 涉及本地部署策略、API/数据契约、推荐语义或待量化的性能路径，详见暂缓原因。 |
| [8a6006e](https://github.com/holll/Emby-go/commit/8a6006e) | feat(api): 管理面错误信封统一 code（批次 74） | 暂缓适配 | 涉及本地部署策略、API/数据契约、推荐语义或待量化的性能路径，详见暂缓原因。 |
| [2df8d37](https://github.com/holll/Emby-go/commit/2df8d37) | feat(api): 弃用登记表与旧同步扫描弃用提示；Web 迁移异步扫描（批次 75） | 暂缓适配 | 涉及本地部署策略、API/数据契约、推荐语义或待量化的性能路径，详见暂缓原因。 |
| [2150352](https://github.com/holll/Emby-go/commit/2150352) | fix(api): Emby 兼容端点错误 code 回显与协议抽检（批次 76） | 暂缓适配 | 涉及本地部署策略、API/数据契约、推荐语义或待量化的性能路径，详见暂缓原因。 |
| [cc222c5](https://github.com/holll/Emby-go/commit/cc222c5) | fix(web): 计划任务单次提交、cron latest-wins 与服务器时区明示（FE-15，批次 77） | 暂缓适配 | 涉及本地部署策略、API/数据契约、推荐语义或待量化的性能路径，详见暂缓原因。 |
| [bd97a87](https://github.com/holll/Emby-go/commit/bd97a87) | fix(web): 播放层命中区复核与遮罩焦点受控循环（FE-07，批次 78） | 不移植旧前端 | 作用于上游旧原生 JS 页面，与本项目 Vue 结构不同；不复制文件。 |
| [ddf8508](https://github.com/holll/Emby-go/commit/ddf8508) | perf(web): 图片宽度预算与 srcset；30 页 DOM/内存基线（FE-11，批次 79） | 已有覆盖 | 对应核心能力已有本地实现，保留本地账户/缓存/性能/播放约定；不等同包含该提交全部附属能力。 |
| [7248f20](https://github.com/holll/Emby-go/commit/7248f20) | perf(store): 二级索引与迁移 ANALYZE（DB-05/06，批次 80） | 已有覆盖 | 对应核心能力已有本地实现，保留本地账户/缓存/性能/播放约定；不等同包含该提交全部附属能力。 |
| [cf08784](https://github.com/holll/Emby-go/commit/cf08784) | perf(store): 搜索路径基准与 DB-04 量化结论（批次 81） | 仅参考证据 | 文档、CI、验收/基准或测试证据，不把上游通过结论当作本地验收。 |
| [f47d333](https://github.com/holll/Emby-go/commit/f47d333) | perf(store): 刮削候选游标分批、回填进度与 JSON 损坏定位（DB-10，批次 82） | 暂缓适配 | 候选游标值得后续实测，但需同时调整进度 total/批处理和取消恢复；不混入本轮兼容修复。 |
| [3e792a8](https://github.com/holll/Emby-go/commit/3e792a8) | docs: 修正批次 82 测试计数与 CI 记录 | 仅参考证据 | 文档、CI、验收/基准或测试证据，不把上游通过结论当作本地验收。 |
| [0056730](https://github.com/holll/Emby-go/commit/0056730) | perf(store): DB-05 列表投影与深分页量化结论（批次 83） | 暂缓适配 | 列表字段投影与深分页需对照本地媒体墙字段、TV 与索引基准后再定。 |
| [7732269](https://github.com/holll/Emby-go/commit/7732269) | docs(acceptance): 记录批次 83 后真实二进制复跑与 R4 收口 | 仅参考证据 | 文档、CI、验收/基准或测试证据，不把上游通过结论当作本地验收。 |
| [45ce493](https://github.com/holll/Emby-go/commit/45ce493) | feat(auth): 多用户基座 users 表、会话绑定与管理员边界（批次 84） | 已有覆盖 | 对应核心能力已有本地实现，保留本地账户/缓存/性能/播放约定；不等同包含该提交全部附属能力。 |
| [1b83259](https://github.com/holll/Emby-go/commit/1b83259) | feat(store): 按用户隔离播放数据、播放会话与登录审计（批次 85） | 已有覆盖 | 对应核心能力已有本地实现，保留本地账户/缓存/性能/播放约定；不等同包含该提交全部附属能力。 |
| [3feede8](https://github.com/holll/Emby-go/commit/3feede8) | feat(users): 用户管理、媒体库授权与登录/播放审计 API（批次 86） | 已有覆盖 | 对应核心能力已有本地实现，保留本地账户/缓存/性能/播放约定；不等同包含该提交全部附属能力。 |
| [e99998e](https://github.com/holll/Emby-go/commit/e99998e) | feat(users): 媒体库可见性强与跨用户缓存隔离（批次 87） | 已有覆盖 | 对应核心能力已有本地实现，保留本地账户/缓存/性能/播放约定；不等同包含该提交全部附属能力。 |
| [8371293](https://github.com/holll/Emby-go/commit/8371293) | feat(web): 用户管理与活动记录面板、普通用户自助改密（批次 88） | 已有覆盖 | 对应核心能力已有本地实现，保留本地账户/缓存/性能/播放约定；不等同包含该提交全部附属能力。 |
| [f866f67](https://github.com/holll/Emby-go/commit/f866f67) | docs: 批次 88 计数与 CI 实跑记录 | 仅参考证据 | 文档、CI、验收/基准或测试证据，不把上游通过结论当作本地验收。 |
| [3be2ad7](https://github.com/holll/Emby-go/commit/3be2ad7) | feat(web): 活动记录拆为独立侧边栏页面（批次 89） | 不移植旧前端 | 作用于上游旧原生 JS 页面，与本项目 Vue 结构不同；不复制文件。 |
| [543720a](https://github.com/holll/Emby-go/commit/543720a) | fix(playback): 播放会话按用户隔离（架构 P0，批次 90） | 已有覆盖 | 对应核心能力已有本地实现，保留本地账户/缓存/性能/播放约定；不等同包含该提交全部附属能力。 |
| [816b470](https://github.com/holll/Emby-go/commit/816b470) | fix(emby): 影片 DTO 增 CollectionIds（生产反馈，批次 91） | 部分移植 | 影片 DTO 增 CollectionIds，按已有 boxsetID 及权限组装。 |
| [f148b7f](https://github.com/holll/Emby-go/commit/f148b7f) | docs: 补充 CollectionIds 契约说明 | 仅参考证据 | 文档、CI、验收/基准或测试证据，不把上游通过结论当作本地验收。 |
| [bbd201f](https://github.com/holll/Emby-go/commit/bbd201f) | fix(emby): 实现 /Items/{id}/Collections 归属查询（生产反馈，批次 91） | 部分移植 | 新增 item Collections 路由，复用本地可见性、最低合集数量及用户授权。 |
| [4fe7e52](https://github.com/holll/Emby-go/commit/4fe7e52) | feat(api): 相似作品同系列优先并按账户过滤（批次 92） | 暂缓适配 | 现有 Similar 已按用户及倒排特征筛选；同系列优先是推荐产品语义变化，暂缓。 |
| [c27b714](https://github.com/holll/Emby-go/commit/c27b714) | feat(api): 相似作品分层推荐算法（批次 92） | 暂缓适配 | 分层排序会改变现有推荐结果，暂缓。 |
| [0354a69](https://github.com/holll/Emby-go/commit/0354a69) | ci: 重跑（上次 go job 测试失败，本地同版本复跑全绿） | 仅参考证据 | 文档、CI、验收/基准或测试证据，不把上游通过结论当作本地验收。 |
| [b90b4cf](https://github.com/holll/Emby-go/commit/b90b4cf) | ci: 测试失败时输出具体用例注解 | 仅参考证据 | 文档、CI、验收/基准或测试证据，不把上游通过结论当作本地验收。 |
| [478856b](https://github.com/holll/Emby-go/commit/478856b) | test(server): 相似用例改用内存缓存，避免依赖本机 Redis | 已有覆盖 | 对应核心能力已有本地实现，保留本地账户/缓存/性能/播放约定；不等同包含该提交全部附属能力。 |
| [3276da8](https://github.com/holll/Emby-go/commit/3276da8) | perf(store): userdata/login_events 索引与周期 ANALYZE；图片缓存 24h（批次 93） | 已有覆盖 | 本地有 user 范围、特征表/索引与版本化图片缓存；不复制 userdata/login_events 迁移或统一 24h 缓存。 |
| [267f20c](https://github.com/holll/Emby-go/commit/267f20c) | fix(store): 全量 schema 校验与备份加固（批次 94） | 部分移植 | 采用备份完整性校验思路；不覆盖本地 schema 初始化。 |
| [b018031](https://github.com/holll/Emby-go/commit/b018031) | refactor(store): 删除硬编码 user 1 的用户数据 API 并收敛管理员回退（批次 95） | 已有覆盖 | 对应核心能力已有本地实现，保留本地账户/缓存/性能/播放约定；不等同包含该提交全部附属能力。 |
| [ebd6352](https://github.com/holll/Emby-go/commit/ebd6352) | feat(cache): Redis 可选并回退进程内缓存（批次 96） | 暂缓适配 | 本地 Redis 必选是已记录部署约定，运行中失败回退与时间预算已实现。 |
| [a75085e](https://github.com/holll/Emby-go/commit/a75085e) | feat(auth): 登录会话摘要化与迁移 12（批次 97） | 移植 | 选取兼容代码并补本项目回归，见上表。 |
| [3006abc](https://github.com/holll/Emby-go/commit/3006abc) | feat(security): 安全响应头、CSP Report-Only 与管理操作审计（批次 98） | 部分移植 | 仅安全响应头/CSP Report-Only，不引入审计表迁移。 |
| [3493632](https://github.com/holll/Emby-go/commit/3493632) | chore(deploy): Docker/systemd 部署样例（批次 99） | 暂缓适配 | 上游容器/systemd 面向旧前端，直接复制会漏 Vue embedui 构建及本地部署参数。 |
| [e25bbab](https://github.com/holll/Emby-go/commit/e25bbab) | fix(web): 路由收起浮层、键盘可见卡片操作、筛选后还原焦点（P1-7/8/9，批次 100） | 不移植旧前端 | 作用于上游旧原生 JS 页面，与本项目 Vue 结构不同；不复制文件。 |
| [ef9bc44](https://github.com/holll/Emby-go/commit/ef9bc44) | fix(web): 刮削候选慢回包不覆盖新选择（P1-10，批次 101） | 不移植旧前端 | 作用于上游旧原生 JS 页面，与本项目 Vue 结构不同；不复制文件。 |
| [c1c4bc1](https://github.com/holll/Emby-go/commit/c1c4bc1) | fix(scheduler): 调度器层任务防重入与 RunNow 忙拒绝（P2-10，批次 102） | 移植 | 选取兼容代码并补本项目回归，见上表。 |
| [b4d1352](https://github.com/holll/Emby-go/commit/b4d1352) | ci: 覆盖率门禁 ≥70%（P2-17，批次 103） | 仅参考证据 | 文档、CI、验收/基准或测试证据，不把上游通过结论当作本地验收。 |
| [751f5e7](https://github.com/holll/Emby-go/commit/751f5e7) | feat(admin): 管理端运行指标接口（P2-9，批次 104） | 移植 | 选取兼容代码并补本项目回归，见上表。 |
| [ac0106c](https://github.com/holll/Emby-go/commit/ac0106c) | docs: 架构复审 2.3 收口基线与剩余项 | 仅参考证据 | 文档、CI、验收/基准或测试证据，不把上游通过结论当作本地验收。 |
| [0c5f307](https://github.com/holll/Emby-go/commit/0c5f307) | feat(server): 运行日志面板与 ffprobe 状态自检；镜像内置 ffprobe（批次 105） | 部分移植 | 仅内存应用日志环与管理员查询，不搬运旧面板/容器/ffprobe 设置。 |
| [65e5c8d](https://github.com/holll/Emby-go/commit/65e5c8d) | fix(web): 移动端日志与用户表格改为堆叠布局（批次 106） | 不移植旧前端 | 作用于上游旧原生 JS 页面，与本项目 Vue 结构不同；不复制文件。 |
| [f78429d](https://github.com/holll/Emby-go/commit/f78429d) | feat: add configurable webhooks and log retention | 部分移植 | 仅日志保留策略；不引入尚无本地业务配置面的 webhooks。 |
| [ca868c3](https://github.com/holll/Emby-go/commit/ca868c3) | fix: resolve movie parents and scope collection queries | 部分移植 | 影片 ParentId 外部库 ID、合集库作用域与过滤，以及鉴权空白；本地电视父级保持。 |
| [8fc574c](https://github.com/holll/Emby-go/commit/8fc574c) | feat: expose build version API and inject release metadata | 移植 | 选取兼容代码并补本项目回归，见上表。 |
| [9dcfeca](https://github.com/holll/Emby-go/commit/9dcfeca) | feat: add opt-in raw HTTP request debug logging | 保留本地日志策略 | 不引入原始请求头/正文记录；沿用脱敏请求日志。 |
| [8cdcc1b](https://github.com/holll/Emby-go/commit/8cdcc1b) | fix: filter collection ownership queries by ListItemIds | 移植 | ListItemIds 按影片归属过滤合集，保留 Ids 交集与空参数语义。 |
