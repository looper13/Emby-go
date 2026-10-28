# R18 PostgreSQL 资料库

作为独立的 `postgres_r18` 媒体库来源，直接按需读取 PostgreSQL 的 `public.r18_video` 和 `public.article`。不生成 NFO/STRM 文件，也不将全库影片导入本地 SQLite。现有目录库、扫描、播放和用户状态保持独立。

## 添加与浏览

1. 更新并启动包含新 Vue 页面及后端的版本。
2. 管理员进入“媒体库”，展开“添加 PostgreSQL / R18 资料库”，填写名称、主机、端口、数据库、用户名、密码和 SSL 模式。
3. 点击“连接并添加 R18 资料库”。服务先检查两张表的所需字段，成功后保存配置；失败时保留表单供修正。
4. 点击该库的“浏览”，或进入侧栏“R18 资料库”。多个来源可在页面选择切换。普通用户须由管理员在“用户管理”授权媒体库后访问。
5. 修改连接时点击该库的“连接配置”。密码不回显；编辑时留空保留已保存密码。

影片列表仅展示 `dvd_id` 有值的记录，排除 NULL、空字符串及仅含空格的值；搜索、顺逆序前后分页及直接读取详情使用同一过滤条件。列表展示封面、标题、番号、内容 ID、发行日期和来源类型。可按番号/内容 ID 精确查询，或搜索日文、英文标题关键词；`digital` / `mono` 可单独筛选。默认按 `release_date DESC NULLS LAST` 排序，最新发行在前，也可选择最早发行在前；两种方向均将未知发行日期放最后，同日按 `(content_id, service_code)` 稳定排序。网页使用条目 ID 游标在同一查询内解析日期边界，支持前后翻页、不支持任意页码跳转；旧条目 ID 游标仍可读取。详情返回恢复筛选、游标、滚动位置及所选卡片。

页面“源表约”数量是 PostgreSQL `pg_class.reltuples` 的统计估计，包含没有 `dvd_id` 的记录，不是过滤后的精确数量，也不是当前搜索条件的匹配数。数据包含普通影片，不将来源名称视为全部记录均属于成人影片的过滤条件。

## 图片尺寸

- 列表与详情封面统一使用与媒体墙相同的 `2:3` 海报框，等比填充、不拉伸。横向完整 DVD 封套展示右侧正面，竖版封面居中；缺图或隐藏图片时仍保留相同尺寸的占位。
- 网页封面使用 `/images/poster`，服务器先裁海报再等比缩小，默认最大 `320×480`、WebP 质量 82，不放大小图；像素取整可能使实际宽度略小于 320。原有 `/images/cover` 保留完整未裁封套，便于查看原图。
- 列表复用媒体墙的小/中/大卡片设置、网格宽度与间距。尺寸偏好共用并保存在浏览器中；窄屏按可用宽度自动减少列数，不强制压成两列。
- 详情封面宽度与普通影片详情对齐：桌面 156 像素，720 像素及以下为 120 像素，不随右侧长标题/资料区拉高。
- 剧照框统一为 `16:9`，等比完整展示，不再使用固定像素高度；查看原图仍通过同源代理获取未裁剪图片。

## 详情与磁力

进入详情后先加载影片资料，再单独查询 `article`。资源使用影片 `dvd_id` 与 `article.number` 关联；缺少 `dvd_id` 时回退内容 ID。仅增加大小写、去连字符候选，不随意删除前导零或截断番号。

- 展示影片封面、首末剧照，以及磁力资源标题、来源网站、日期、大小和标签。
- 排除已拒绝记录及无效磁力。每次读取最多 50 条，可继续加载，资源偏移上限为 10000。
- 可复制磁力或打开 HTTP(S) 来源页面，不启动磁力协议处理程序。
- `size_bytes` 优先；旧 `size` 按 MB 换算，缺失时显示大小未知。资源可用性未经下载验证。
- 资源请求失败不影响已加载的影片详情，可以单独重试。封面和剧照遵循已有“隐藏媒体图片”设置。

## 图片代理

列表封面、详情封面、首末剧照及“查看原图”链接统一使用本服务的同源图片代理，浏览器不直接访问数据库里的源站图片 URL。服务器仅按已授权条目的图片类型/序号读取源图，不接受任意 URL 代理。

- 图片请求接受登录会话 Cookie，并在缓存命中或 304 前重新检查用户与媒体库权限；撤销授权后不能继续通过代理获取图片。
- 只代理 HTTP(S) 公网图片；每次请求及重定向检查目标地址，直连固定已校验 IP，拒绝内网、回环、链路本地等地址。不转发浏览器 Cookie、Emby Token 或 Authorization。
- 最多 4 路上游图片请求，与刮削预览共享限流；HTTP 超时 15 秒，单张上限 8 MiB。只返回识别为 JPEG/PNG/GIF/WebP/AVIF 的内容，不返回 HTML/SVG。
- 封面按需使用两级缓存：独立热内存最多 256 项、每项最多 128 KiB（图片数据约 32 MiB 上限），磁盘最多 512 MiB / 10,000 个文件，原图和各尺寸海报合计受限。成功封面有效期 24 小时，重启可复用磁盘文件；缓存过期、损坏或被淘汰后再读取源站，不预下载整个资料库。
- 网页与 Emby 默认 Primary 共用同一海报缓存；请求先检查成品海报缓存，不先读取/下载原图。新尺寸复用已缓存原图，同一封面并发请求合并下载；派生海报沿用原图的到期时间，不因另生成一个尺寸延长旧内容的有效期。
- 封面缓存键包含媒体库 ID/连接修订、条目 ID、源图 URL、处理算法版本、尺寸及质量。源图 URL 改变后使用新缓存；同一 URL 的内容更新可能要等到 24 小时到期才显示。条目源图片 URL 元信息仍缓存 1 分钟。
- 磁盘目录为本地 SQLite 所在目录的 `.work-cache/r18-covers/<实例命名空间>/`，文件名使用哈希，不保存源 URL 或数据库凭据；通过临时文件写入后改名，校验完整性并按到期时间/LRU 清理，仅管理自己的缓存文件。此目录不是静态公开目录，仍须限制所在目录的访问权限。目录不可写或磁盘缓存故障时记录警告并退回有界内存缓存，不中断正常图片响应。
- 剧照只在进入详情后按需懒加载，不预取、不加入封面磁盘缓存；小于等于 512 KiB 的成功剧照仍复用现有进程内缓存 10 分钟。浏览器使用私有缓存及 ETag 重验证，失败响应不缓存；所有缓存命中、HEAD 与 304 前仍重新检查授权。
- 服务器需要能够访问源图片站点；如服务器出网需要代理，可在服务进程环境中设置 `HTTP_PROXY` / `HTTPS_PROXY`。这与浏览器到本服务的图片代理、媒体库的播放代理开关相互独立。

## 权限与运行边界

- PostgreSQL 连接在每个池连接上设置 `default_transaction_read_only=on`，连接超时 5 秒，单条 SQL 超时 6 秒；查询参数使用绑定值。建议仍配置专用只读数据库账号。
- 保存连接和查看连接配置只允许管理员。普通用户的媒体库授权在连接源库之前检查；接口不返回数据库密码。
- 连接密码保存在本地 SQLite 的私有连接表中，**未加密**。限制数据库、备份和所在目录的访问权限，使用 HTTPS 访问管理端；跨不可信网络时使用校验证书的 PostgreSQL SSL 模式。
- 删除此媒体库只删除本地配置与授权，不修改 PostgreSQL 表。R18 来源不参与目录扫描、监听、查重或播放代理配置。
- 源库数据变化在后续查询/刷新时读取，不在启动时做全量同步。不创建、修改源库索引。标题关键词搜索可能扫描大表并超时，优先使用番号查询。
- 升级前备份本地 SQLite；启动时升级媒体库来源约束，保留现有媒体库 ID、影片、收藏、授权及自定义索引/触发器。

## Emby / Yamby 只读浏览

R18 来源现在进入 Emby 媒体库枚举；普通用户仍须授权该库。根路径与 `/emby` 前缀、大小写路由及常见查询参数复用已有兼容注册。

- `GET /Users/{uid}/Views`、`/Library/MediaFolders`、`/Library/VirtualFolders` 及其 `/Query` 变体可列出 R18；媒体库仍使用原外部 ID，配置列表没有本地目录、源库连接或密码。
- `GET /Users/{uid}/Items`、`/Items` 带该库 `ParentId` 可浏览，支持 `StartIndex`/`Limit`、`SearchTerm`（番号/内容 ID/标题）、年份、名称范围和排序。默认使用 `PremiereDate` / `Descending`，即 `release_date DESC NULLS LAST`；同日以内容 ID/来源稳定排序。客户端显式指定 `SortName` 时按标题、`PremiereDate`/`ProductionYear` 按发行日期排序，其它排序键（包括 `DateCreated`）回退发行日期；显式 `ContentId` 可按内容 ID 排。`Latest` 在 R18 库上下文始终按发行日期倒序返回，不表示真实入库时间。
- 客户端 `TotalRecordCount` 是同条件下的实际计数，复用 30 秒计数缓存，不使用网页“源表约”的统计估计。偏移分页、标题排序/模糊搜索可能扫描大表，仍受单条 SQL 6 秒超时保护；超时返回可重试的 502，不修改源库索引。网页继续使用游标分页。
- 影片 ID 为 `r18-{libraryID}-{原复合条目ID}`，区分不同库及 `(content_id, service_code)`；从列表取得的 ID 可用于 `/Items/{id}`、`/Users/{uid}/Items/{id}` 详情及 `/Items?Ids=...` 查询。列表与详情均保留 `dvd_id` 非空过滤。
- 详情为不可播放的虚拟 Movie：`IsPlaceHolder=true`、`PlayAccess=None`、空 `MediaSources`/`MediaStreams`；不伪造播放地址、时长或入库时间。`PlaybackInfo` 返回 `NoCompatibleStream` 和空媒体源。磁力仍仅在网页详情展示。
- `/Items/{id}/Images` 提供图片标签；`/Images/Primary`、`Thumb` 及 `Backdrop/{index}` 经同一服务器代理返回，支持 GET/HEAD、ETag/304 和尺寸参数。Primary 在服务器裁成 2:3，横向封套取右侧正面，未指定尺寸时最大 `320×480`；其它图不裁剪。封面原图/派生尺寸使用上述两级缓存，剧照保留轻量内存缓存；转换复用既有像素上限及 CPU 并发限制，不支持解码的格式回退源图。
- R18 图片须携带有效访问令牌（Header、api_key 等）或登录 Cookie；原有本地媒体的匿名图片兼容不变。所有 R18 图片缓存与 304 前重新检查授权，源站不收到客户端凭据。未建模的 Logo/Banner/Art 返回 404；已存在条目的相关推荐、合集、附加分段、特殊内容及时间轴预览返回空结果。

## 暂未实现（TODO）

播放源接入、磁力下载、订阅及下载器集成均未实现。R18 不支持现有影片收藏、已看/进度和演员检索；不带 R18 `ParentId` 的跨库影片聚合、全局搜索、计数及首页推荐仍沿用本地 NFO 索引，不混入 PostgreSQL 来源。当前覆盖网页资料展示与 Emby 库内只读浏览，不把磁力当作 HTTP 视频流；具体 Yamby/Emby 客户端仍需实机确认。

## 验证方式

```bash
go test ./internal/r18 ./internal/imageutil ./internal/store ./internal/server -run '^TestR18' -count=1
go test ./internal/cache ./internal/server -run '^(TestFiles|TestMemoryLimited|TestR18LargeCover|TestR18CoverCache)' -count=1
go test ./internal/server -run '^TestR18CoverDerivedKeepsOriginalExpiry$' -count=1
npm --prefix frontend run typecheck
npm --prefix frontend run test:unit -- --run
```

2026-10-28 本地缓存验证：超出旧 512 KiB 阈值的原图在并发网页/Emby 请求中只下载一次，成品先命中、新尺寸复用原图、重启复用、过期/损坏处理、磁盘不可用回退、授权撤销及派生图不延长原图有效期均由聚焦测试覆盖。真实来源样例返回 `319×480`、45,338 字节 WebP，网页与 Emby 默认海报字节一致，HEAD 无响应体、ETag 请求返回 304；桌面 1440 和移动端 390 像素页面无横向溢出，列表不预取剧照，详情剧照为懒加载。

类型检查、354 项前端单测、Vue/embedui 构建和 Go vet 通过。Go 广泛回归通过 488 个顶层测试，但 `TestPollingMonitorDebouncesRetriesAndTracksMoves` 的目录改名断言失败，单独重复测试仍出现；此次没有改动监听模块。回归明确排除了会清理本机既有 Redis DB15 的 `TestRedisBehavior` 和已有探测测试 `TestMediaProbe` / `TestProbeCircuitBreaker`，不声称全量测试或竞态检测通过（本机 CGO 关闭且未安装 C 编译器）。用户已授权提交推送，尚未部署或进行原生 Yamby 验收。

真实来源验收需在本机设置 `EMBY_R18_TEST_HOST`、`EMBY_R18_TEST_DATABASE`、`EMBY_R18_TEST_USERNAME`、`EMBY_R18_TEST_PASSWORD` 后运行 `go test ./internal/r18 -run '^TestR18LiveReadOnlyPaginationAndResources$' -count=1 -v`，及 `go test ./internal/server -run '^TestR18LiveEmbyBrowse$' -count=1 -v`。测试仅查询源库，HTTP 验收只创建临时本地实例；端口使用 5432，未配置主机时跳过。不将真实连接信息写入仓库或测试夹具。
