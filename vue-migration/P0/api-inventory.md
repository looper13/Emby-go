# 接口契约盘点（API Inventory）

P0/VUE-01 产物。来源：`internal/server/server.go` 路由表 + 各 handler 实际代码 + Go struct json tag（非猜测）。前端调用点以 `internal/server/web/app.js`、`login.js` 为准。
原始 DTO 建模、字段映射与真实 JSON 夹具（`fixtures/json/`）以本文件为基线；P1 建立 `api/contracts.ts` 时逐字对照。

## 0. 通用约定

- 管理端统一前缀 `/api/admin`，挂 `requireAuth` + `noStore`（响应 `Cache-Control: no-store`）。未认证：`401 {"error":"unauthorized"}`。
- token 来源优先级（auth.go:30-56）：`X-Emby-Token` → `X-MediaBrowser-Token` → `?api_key=` → `?ApiKey=` → `?X-Emby-Token=` → `Authorization/X-Emby-Authorization` 头里的 `Token=...`。前端仅用 `X-Emby-Token`。
- 错误响应统一 `{"error": "<消息>"}`；成功可为 200/202/204。
- **404 兜底**（server.go:475-494）：所有未注册路径 `404 {"error":"not found"}`，非 `/api/` 路径会记录进探针表（同 method+path 只记首次）。
- `ApiPrefix` 无版本号；字段大小写混合（见 Movie 结构）。

## 1. 认证与初始化

| 端点 | 请求 | 响应 | 错误/状态码 | 前端使用 | 锚点 |
|---|---|---|---|---|---|
| `GET /api/auth/status` | — | 200 `{initialized: bool}` | 500 | login.js:6-8 | auth.go:87-94 |
| `POST /api/auth/initialize` | JSON `{Username, Pw}` | **204 无 body** | 400 `账号至少 3 个字符，密码至少 9 个字符`；409 `管理员已初始化` / `初始化失败，请更换账号后重试` | login.js:54,68 | auth.go:100-124 |
| `POST /Users/AuthenticateByName` | JSON 或 form `{Username, Pw}` | 200 `{AccessToken, ServerId, User:{Id:"1",Name,ServerName,ServerId,HasPassword,…Policy{IsAdministrator:true,…}}}` | 400 `invalid credentials payload`；401 `账号或密码错误`；428 `管理员尚未初始化` | login.js:54,72-74 | auth.go:126-190 |
| `GET /Users/Me`（需认证） | — | 200 userDto（同上 User） | 401 | app.js:1986, login.js:83 | auth.go:240-242 |

说明：两者大小写变体都注册（`/users/authenticatebyname` 等，server.go:447-449）。token 为 24 字节 hex；Redis 缓存 7 天（auth.go:15）。

## 2. 媒体库

| 端点 | 请求 | 响应 | 错误 | 前端 | 锚点 |
|---|---|---|---|---|---|
| `GET /api/admin/libraries` | — | 200 `{items:[{Id:number,Name,Path}], total}` | 500 | app.js:169/218/319/992/1181/1539 | admin.go:35-42；store.go:41-45 |
| `POST /api/admin/libraries` | JSON `{Name, Path}`（无 tag，键即 Go 字段名） | 200 返回单个 Library 对象 `{Id,Name,Path}` | 400 `name and path required` / 库错误 | app.js:250 | admin.go:69-82 |
| `DELETE /api/admin/libraries/:id` | — | **204** | 404 `library not found`；500 | app.js:258 | admin.go:44-67 |

副作用：删除只删索引与播放进度/收藏（不删磁盘文件，确认文案已如此声明）；`BumpVersion` 使媒体墙缓存失效。

## 3. 扫描 / 重建

| 端点 | 请求 | 响应 | 错误 | 前端 | 锚点 |
|---|---|---|---|---|---|
| `POST /api/admin/scan`（别名 `/tasks/scan`，UI 未用） | query `library_id`（可选，0=全部） | 200 `scanner.Result`=`{success,pending,incompatible,failed,added,updated,skipped,deleted}`（全小写） | **409 `扫描正在进行中`**（含探测/刮削占用 NFO 时）；404 `library not found`；**499** `{"error":"扫描已取消"}`（客户端断开）；500 | app.js:1970 | admin.go:304-330；scanner.go:24-33 |
| `POST /api/admin/reindex` | — | 200 同上（无任何媒体库时 200 全零） | 409/499/500 | app.js:207 | admin.go:332-360 |
| `GET /api/admin/scan/progress` | — | 200 `scanStatus`：`{running,library_index,libraries,library_name,phase("walk"|"process"|""),total,done,current,success,pending,incompatible,failed,added,updated,skipped,deleted,started_at,finished_at,cancelled,error}` | — | app.js:1948/1973/2029 | server.go:107-128, admin.go:215-220 |

关键语义：扫描是**同步长请求**，请求 context 取消（浏览器刷新/关闭）→ 后端中止遍历并把进度标 `cancelled:true`；`phase=walk` 时 total 是"已发现数"而非总数。

## 4. 媒体墙

| 端点 | 请求 | 响应 | 错误 | 前端 | 锚点 |
|---|---|---|---|---|---|
| `GET /api/admin/items` | query：`limit`(默认100，1-1000 外强制100)、`offset`(≥0)、`library_id`(0=全部)、`status`(空/`success`/`manual`/`pending`/`incompatible`)、`search`、`source_protocol`、`genre`、`tag`、`studio`、`person`、`collection`、`scrape`(`failed`/`confirm`)、`sort`(默认 `title`；前端默认传 `datecreated`)、`order`(`desc`=倒序) | 200 `{items:[Movie], total, limit, offset, userdata:{ "<id>": {played,favorite,position_ticks,play_count} }, image_tags:{ "<id>": {Primary?, Thumb?} }}` | 500 | app.js:556 | admin.go:362-434 |

要点：
- `scrape` 非空且 `status` 为空时，后端把 status 设为 `StatusAll`（否则刮削失败条目多是 pending 会被默认口径滤掉，admin.go:373-378）。
- **响应有 5 秒服务端缓存**（admin.go:395-396，键含库版本与刮削版本）。前端 60 秒快照叠加在这之上，写操作需保证 BumpVersion（写接口均已做）。
- `items` 元素是 `store.Movie`（store.go:47-91），json 键**混合大小写**：小写下划线 `id, library_id, source_path, source_protocol, source_container, collection, official_rating, sortname, provider_id, trailer_url, cover_url, created_at, updated_at, last_scrape_at, last_scrape_error`；其余保持 Go 字段名大写开头 `Status, NFOPath, OutputDir, Number, Title, OriginalTitle, Plot, Year, Premiere, Rating, Director, Series, Maker, Label, Genres, Tags, Studios, Taglines, PosterPath, BackdropPath, BackdropPaths, LandscapePath, RuntimeSeconds, AdditionalParts`。
- `userdata` 键为**字符串 id**、值 snake_case（与详情接口的 PascalCase userdata 不同，见 §5）。
- `image_tags` 仅含存在本地图的条目；`Primary`/`Thumb` 的值为内容 tag（mtime 派生，library.go:193）。
- `movieCovers` 会在返回前按磁盘现状解析封面（artwork.go:138）。

## 5. 影片详情

| 端点 | 请求 | 响应 | 错误 | 前端 | 锚点 |
|---|---|---|---|---|---|
| `GET /api/admin/items/:id/detail` | — | 200 见下 | 404 `not found`；500 | app.js:718 | detail.go:42-119 |

响应字段：
- `movie`: `store.Movie`（同 §4，且经 `movieArtwork` 补全封面/背景现状）。
- `playable`: bool（当前源为 http(s) 且可见，media.go:368-379）。
- `images`: `[{ImageType:"Primary"|"Thumb"|"Backdrop", Path, Filename, ImageTag, (ImageIndex 仅 Backdrop)}]`（media.go:343-365）。
- `library_name`: string（库不存在时空串）。
- `userdata`: `store.UserData` — **PascalCase**：`{PlaybackPositionTicks, PlayCount, Played, PlayedPercentage?, LastPlayedDate?, IsFavorite}`（store.go:93-103）；`has_userdata`: bool。前端当前未用此字段。
- `files`: `[{index:1..n, path, name, role:"main"|"part", size, mediainfo_path?, streams:[…], probed:bool}]`（detail.go:23-32）。`index=1` 主文件；streams 为 Emby MediaStream 风格（buildStreams，media.go:603+）。
- `actors`: `[{name, avatar_url?, image_tag?, has_image}]`（detail.go:34-40）。有 `image_tag` 即本地有头像副本。
- `modified_at`: .strm 文件 mtime（RFC3339，取不到空串）。

## 6. 写入接口（详情/卡片/手动补录）

| 端点 | 请求 | 响应 | 错误 | 前端 | 锚点 |
|---|---|---|---|---|---|
| `POST /api/admin/items/manual` | JSON：`{library_id?, source_path, source_url, title, year, plot, number, original_title, director, series, maker, label, poster_path, backdrop_path, genres[], tags[], studios[]}` | 200 `{id, status:"success"}` | 400 `source_path、title 和 http(s) source_url required` / `library required` / 写文件错误 | app.js:1035 | admin.go:456-529 |
| `PUT /api/admin/items/:id`（**UI 未用**） | JSON `{title?, plot?, year?}`（指针，缺省=不改） | 200 `{status:"success"}` | 400 `invalid json`/`nfo not found`/`没有可写入的字段`；404；500 | — | admin.go:531-601 |
| `POST /api/admin/items/:id/reread` | — | 200 `{status, protocol, playable}` | 404 `not found`/`library not found`/`source no longer indexed`；500 | app.js:456/856 | admin.go:603-650 |
| `DELETE /api/admin/items/:id` | — | **204** | 404 `not found`；500 | app.js:464 | admin.go:436-454 |
| `POST /api/admin/items/:id/probe` | — | 200 `{id, nfo_path, files:[{kind,index,path,mediainfo_path,info}], info, failures?}`（info=probeInfoJSON：`{duration_seconds,size_bytes,bitrate,format,video{…},audio{…}}`） | 404；**409 无 NFO** `该影片没有 NFO…`；500 ffprobe；502 全失败 `全部文件探测失败: …` | app.js:1887 | probe.go:222-307 |
| `POST /api/admin/items/:id/images/:kind`（**UI 未用**） | multipart 字段 `file`；kind ∈ poster/primary/backdrop/fanart/landscape | 200 `{status:"success", path}` | 400 `file required`/`unsupported image kind`；404；500 | — | admin.go:661-719 |

副作用注意：
- 以上全部占用 NFO 写入通道（互斥）；被占用时返回 409 `媒体库正在处理其他任务，请稍后再试`（scrape.go:70-76）。
- manual 会**写盘**：创建 .strm（内容为 URL+\n）与同名 .nfo（admin.go:494-513）。
- reread 走 `scanner.RescanOne`（单文件重扫，不触发整库 DeleteMissingSources，admin.go:634-637）。
- 成功写操作均 `BumpVersion`（前端 wallState.dirty 之外的服务端失效）。

## 7. 图片服务（Emby 端点，前端直接用于 <img src>）

| 端点 | 请求 | 响应 | 错误 | 前端 | 锚点 |
|---|---|---|---|---|---|
| `GET /Items/:id/Images/:kind[/:index]` | query：`maxWidth`/`width`、`maxHeight`/`height`（>4000 截断）、`quality`(1-100)、`tag`(仅用于 URL 变化，服务端用 ETag) | 200 图片（请求了尺寸→webp 缩略；否则原图）；带 `ETag` 与 `Cache-Control: public, max-age=300, must-revalidate`；`If-None-Match` 命中 → **304** | 404 | app.js imageURL/演员头像 | media.go:46-115, 171-216 |

要点：
- **无鉴权**（挂不上 token，客户端直接引用；route auth=false，server.go:396-397）。
- kind：`Primary/Poster`→海报；`Thumb`（缺 landscape 回退海报）；`Backdrop/Fanart` 按下标取背景图列表；`Landscape`。`index` 仅对 Backdrop 有意义，其它 kind index≠0 → 404（media.go:72-80）。
- id 还支持：媒体库外部 id（库封面）、`boxset:<b64>`、虚拟实体 id（`person:…` 等，见 §10）——都会回代表图。
- 前端 `imageURL(id,type,width,tag,index)` 的参数顺序固定 `tag` 在前 `maxWidth` 在后（app.js:101-106），P4 测试按其断言。

## 8. 播放

| 端点 | 请求 | 响应 | 错误 | 前端 | 锚点 |
|---|---|---|---|---|---|
| `GET/HEAD /Videos/:id/stream[.ext]`（大小写两套 + 可选扩展名，server.go:465-473） | — | **302 Location: 源地址**（HEAD 只回 302+Location） | 404 `not found`/`source not found`；**415 `unsupported source`**（非 http(s)） | app.js:973 | media.go:969-990 |
| `GET/HEAD /Videos/:id/proxy[.ext]` | 透传 `Range`、`User-Agent`；`Accept-Encoding: identity` | 透传上游状态码（通常 206/200）与 `Content-Type/Content-Length/Content-Range/Accept-Ranges/Last-Modified/ETag`；缺省 `Accept-Ranges: bytes` | 404；415；502 连接失败 | app.js:975（回退） | media.go:998-1053 |

- 两者均 `noStore`；`/stream` 服务端零带宽（302），`/proxy` 只服务网页播放器（混合内容/防盗链兜底）。
- 预告片走 NFO 远程 URL，不经这两个端点（无代理回退，与计划一致）。

## 9. 媒体信息探测（批次任务）

| 端点 | 请求 | 响应 | 错误 | 前端 | 锚点 |
|---|---|---|---|---|---|
| `POST /api/admin/probe/media` | JSON `{library_id, status, only_missing, limit}`（query 亦可）；`status` 默认 `"success"`；`only_missing` 默认 true | 202 `{task_id, total(文件数), movies}` | 400 invalid json；**409 `媒体信息探测正在进行中`**（或 NFO 被扫库/刮削占用）；500 ffprobe 缺失/自检失败；200 `{total:0, movies:0, message:"没有可探测的影片（探测需要影片已有 NFO）"}` | app.js:1868 | probe.go:152-199 |
| `GET /api/admin/probe/media/progress` | — | 200 `probeStatus`：`{running,total,done,success,skipped,failed,current,started_at?,finished_at?,error?,cancelled,aborted?,failures?[]}` | — | app.js:1842/2040 | probe.go:53-71, 201-206 |
| `POST /api/admin/probe/media/cancel` | — | 200 `{status:"cancelling"}` | 409 `当前没有探测任务` | UI 未用（页面无取消按钮） | probe.go:209-220 |

语义要点：
- 任务异步（goroutine + 常驻 context），**切页/刷新浏览器不影响任务**；刷新后靠 progress 恢复。
- `aborted:true` = 连续失败熔断（≥20），`error` 带中文说明；前端 renderProbeProgress 只区分 running/cancelled，`aborted` 会落入 error 文案展示。
- total 是**文件数**（分集逐个 .strm），非影片数；`only_missing` 按 NFO `<fileinfo><probeversion>` 判定。
- 结束 8 秒自动隐藏面板仅在 `failed==0` 时（app.js:1852-1855）；单条探测见 §6。

## 10. 刮削

### 10.1 配置与测试

| 端点 | 请求 | 响应 | 错误 | 前端 | 锚点 |
|---|---|---|---|---|---|
| `GET /api/admin/scrape/settings` | — | 200 `{metatube_url, metatube_token(masked "abcd****"), timeout_seconds, concurrency, download_images, image_quality, avatars_dir, overwrite, configured, defaults(完整 Config), translate:{title,summary,target_lang,api_url,api_key(masked),timeout_seconds,configured}}` | — | app.js:1181 | scrape_settings.go:103-126 |
| `PUT /api/admin/scrape/settings` | JSON 指针字段 `{metatube_url?,metatube_token?,timeout_seconds?(1-600),concurrency?(1-8),download_images?,image_quality?(1-100),avatars_dir?,overwrite?,translate?:{title?,summary?,target_lang?,api_url?,api_key?,timeout_seconds?(1-300)}}` | 200 返回与 GET 相同结构 | 400 `invalid json` / `X 必须在 min~max 之间`；500 | app.js:1277 | scrape_settings.go:128-224 |
| `POST /api/admin/scrape/test` | JSON `{target: "metatube"|"translate"}` | 200 `{ok:true, detail:"MetaTube 可达"|"翻译返回：…", elapsed_ms}` 或 `{ok:false, error, elapsed_ms?}` | 永远 200（结果在 ok 字段） | app.js:1283 | scrape.go:647-680 |

密钥语义（重要）：token/api_key **留空或含 `****` 一律不写库**（isMasked，scrape_settings.go:226-230）；GET 回显掩码，前端把掩码填进输入框再原样提交即"不修改"（app.js:1200-1201 同款行为，Vue 必须保持）。

### 10.2 批量任务

| 端点 | 请求 | 响应 | 错误 | 前端 | 锚点 |
|---|---|---|---|---|---|
| `GET /api/admin/scrape/candidates` | query `library_id`、`only_missing`（默认 true，仅 `"false"` 为假） | 200 `{total, library_id, only_missing}` | 500 | app.js:1293 | scrape.go:635-644 |
| `POST /api/admin/scrape/run` | JSON `{library_id, only_missing?, limit?, overwrite?}`（overwrite 缺省=全局配置） | **202** `{total}` | 400 `invalid json`/`尚未配置 MetaTube 地址或 token`；409 `<占用者>正在进行中，请稍后再试`/`刮削任务正在进行中`；200 `{total:0, message:"没有符合条件的影片"}`；500 | app.js:1308 | scrape.go:188-240 |
| `POST /api/admin/scrape/avatars` | JSON `{library_id, limit?}` | **202** `{total:演员数}` | 400/409 同上；200 `{total:0, message:"没有缺头像的演员（头像任务只补缺失，是幂等的）"}` | app.js:1315 | scrape.go:369-407 |
| `GET /api/admin/scrape/progress` | — | 200 `scrapeStatus`：`{running,kind:"scrape"|"scrape_avatars",total,done,success,skipped,failed,current,started_at?,finished_at?,error?,cancelled,failures?[]}` | — | app.js:1374/1185 | scrape.go:32-46,167-172 |
| `POST /api/admin/scrape/cancel` | — | 200 `{status:"cancelling"}` | 409 `当前没有刮削任务` | app.js:1321 | scrape.go:174-184 |

语义：批量/头像任务挂 `rootCtx`（后台执行，刷新不取消）；与扫描/探测/单条刮削/编辑**共用 NFO 互斥**（有占用时 409，占用者中文名见 scrape.go:87-103）。失败样例最多留 20 条（maxScrapeFailureSamples）。

### 10.3 单条预览 → 确认（无状态两步）

| 端点 | 请求 | 响应 | 错误 | 前端 | 锚点 |
|---|---|---|---|---|---|
| `GET /api/admin/items/:id/scrape/preview` | — | 200 `PreviewResult`：`{movie_id, query, expected_number?, candidates:[{provider,id,number,title,score,release_date?,thumb?,exact}], recommended(下标，-1=无), exact}`；候选≤5 条 | 404；400 未配置；502 上游错误 | app.js:1399 | scraper.go:126-147,177-204；scrape.go:443-461 |
| `POST /api/admin/items/:id/scrape/inspect` | JSON `{provider, id}` | 200 `scrapeInspect`：`{provider,id,number,title,summary,poster(本服务代理URL),backdrop,score,release_date?,runtime?,actors[],fields:[{key,label,old,next,change}],images:[{name,kind,exists,preview}],overwrite(=当前全局默认)}` | 400 `provider 与 id 必填`/未配置；404；502 | app.js:1419 | scrape_inspect.go:22-135；scrape.go:464-491 |
| `POST /api/admin/items/:id/scrape`（确认写入） | JSON `{provider, id, overwrite?}`；**overwrite 缺省=全局配置**，显式 false/true 生效 | 200 `{status:"success", provider, number, title, images(张数), warnings}` | 400；404；409 NFO 占用；502 上游/写入失败（失败会 SetScrapeResult 记录） | app.js:1499 | scrape.go:494-543 |

- `thumb`/`poster`/`backdrop`/`preview` 均为 `/api/admin/scrape/image?kind=…&provider=…&id=…`（本服务代理，**无鉴权**；scrape.go:549-606）。`fields` 里列表字段（genre/studio/actor）只有 next 无 old，`change:true`。
- 预览/inspect 全程零副作用（不翻译、不下图、不写盘）；确认才写。

## 11. 其他管理端点

| 端点 | 响应 | 前端 | 锚点 |
|---|---|---|---|
| `GET /api/admin/apikeys` | 200 `{items:[{key,name,created_at}], total}` | app.js:1074 | admin.go:790-797 |
| `POST /api/admin/apikeys` | JSON `{name}` → 200 单个 key 对象 | app.js:1105 | admin.go:799-814 |
| `DELETE /api/admin/apikeys/:key` | 204；404 `key not found` | app.js:1114 | admin.go:816-833 |
| `GET /api/admin/status` | 200 `{success, manual, pending, incompatible}`（计数） | app.js:170 | admin.go:758-769 |
| `GET /api/admin/settings` | 200 `{listen, db_path, debug, cache:"redis", redis_addr, redis_db, redis_online, redis_failures, cache_stats:{…}, library_monitor_mode, disable_library_monitor}` | app.js:1046 | admin.go:721-742 |
| `GET /api/admin/tasks` | 200 `{items:[{id,type,status,started_at,ended_at?,error?}], running}`（内存最近 100 条，进程重启即空） | app.js:1122 | admin.go:744-756 |
| `GET /api/admin/probe` | 200 `{items:[…]}`（最近 100 条探针记录） | app.js:1156 | admin.go:771-778 |
| `DELETE /api/admin/probe` | 204 | app.js:1172 | admin.go:780-786 |

## 12. 计划任务

| 端点 | 请求 | 响应 | 错误 | 前端 | 锚点 |
|---|---|---|---|---|---|
| `GET /api/admin/scheduled` | — | 200 `{items:[{id,name,type,cron,params(JSON字符串),enabled,last_run_at?,last_status?,last_message?,created_at,updated_at,next_run?}], total, types:{scan,reindex,probe,scrape,scrape_avatars 的中文名}}` | 500 | app.js:1539 | scheduled.go:201-216；store.go:1827-1839 |
| `POST /api/admin/scheduled` | JSON `{name, type, cron, params{}, enabled?}` | 200 返回创建的记录 | 400 `名称不能为空`/`不支持的任务类型 "x"`/`cron 表达式不能为空`/`cron 表达式非法: …` | app.js:1679 | scheduled.go:255-274 |
| `PUT /api/admin/scheduled/:id` | 同上 | 200 更新后记录 | 400 同上；404 `task not found` | app.js:1675 | scheduled.go:276-305 |
| `POST /api/admin/scheduled/:id/toggle` | — | 200 切换后的记录 | 404 `task not found`；500 | app.js:1691 | scheduled.go:308-331 |
| `POST /api/admin/scheduled/:id/run` | — | **202** `{status:"started"}` | 404 `task not found` | app.js:1696 | scheduled.go:353-369 |
| `DELETE /api/admin/scheduled/:id` | — | 204 | 404 `task not found`；500 | app.js:1705 | scheduled.go:333-350 |
| `POST /api/admin/scheduled/validate` | JSON `{cron}` | 200 `{valid:true, next_runs:[RFC3339 × 5]}` 或 `{valid:false, error}` | 400 invalid json（校验失败也是 200） | app.js:1627 | scheduled.go:372-390 |

参数规范化（normalizeParams，scheduled.go:155-197）：scan→library_id；reindex→无参；probe→library_id/status/limit/only_missing；scrape→library_id/limit/only_missing/overwrite；scrape_avatars→library_id/limit。`params` 在列表响应里是 JSON **字符串**（前端 JSON.parse 回填，app.js:1547）。
注意：`types` 来自服务端字符串表，而前端页头另有内置 TASK_TYPE_TEXT（含 watch/poll，服务端 types 没有）——前端优先用 `data.types`（app.js:1542）。

## 13. Emby 兼容端点（仅列前端用到的）

| 端点 | 请求 | 响应 | 前端 | 锚点 |
|---|---|---|---|---|
| `GET /Items/:id/Similar`（需认证） | query `Limit`（默认 12，1-50 外强制 12） | 200 `{Items:[embyItem], TotalRecordCount, StartIndex}`；**20 秒服务端缓存** | app.js:717 | library.go:520-596 |

`Items` 元素（embyItem*，前端仅用其中几个）：`Id`(字符串)、`Name`、`ImageTags:{Primary?}`、`ProductionYear`、`RunTimeTicks`、`CommunityRating`、`UserData`、`Actors` 等 Emby 风格键。前端兼容点：`item.ImageTags.Primary` → `imageURL(item.Id,'Primary',320,tag)`；时长用 `RunTimeTicks/1e7`。

## 14. UI 已用 / 后端已支持但 UI 未使用（迁移不得创造或遗漏）

**当前 UI 已使用**：§1（status/initialize/authenticate/Me）、§2 全部、§3 scan/reindex/progress、§4 items、§5 detail、§6 manual/reread/delete/probe（PUT items 与 images 上传**未用**）、§7 图片、§8 stream/proxy、§9 probe/media+progress（cancel **未用**）、§10 全部（单条两步 + 批量 + 头像 + image 代理）、§11 全部、§12 全部、§13 Similar。

**后端已支持但 UI 未使用**（Vue 首轮同样不暴露，除非计划另行批准）：
- `PUT /api/admin/items/:id`（影片标题/简介/年份编辑）
- `POST /api/admin/items/:id/images/:kind`（图片上传）
- `POST /api/admin/probe/media/cancel`（探测取消按钮）
- `POST /api/admin/tasks/scan`（scan 别名）
- Emby 兼容层其余端点（Views、Items、PlaybackInfo、收藏/已看/评分、Sessions、System 等）

## 15. 与计划 §5.1/§2 的差异与补充

1. 计划表"写入 `/items/manual`、`/items/:id`、`/items/:id/reread`"——`/items/:id` 的 PUT 存在但 UI 未用（见 §14），基线中区分正确。
2. 计划未提到、Vue 必须知道的服务端行为：
   - 媒体墙 items 响应有 **5 秒服务端缓存**（键含库/刮削版本；写接口均 BumpVersion，失效链完整）。
   - Similar 有 **20 秒服务端缓存**。
   - 探测有**熔断**（连续 20 失败 → abort + cancelled=false + aborted=true + error 中文说明）；前端旧实现未专门展示 aborted。
   - 刮削批量与头像任务互斥（同占用 NFO 通道）；影片编辑/上传/重读/手动补录也会短暂占用，占用期批量任务返回 409。
   - `/api/admin/scrape/image` 与 `/Items/:id/Images/*` **无鉴权**且带 `public, max-age=300` 缓存——前端不得自行给图片 URL 拼 token（计划 §5.1 已注明）。
   - `/api/auth/initialize` 成功返回 **204 无 body**（前端不读 body，Vue 的 api client 必须处理 204）。
   - 登录失败/未初始化分别 401/428；`/Users/Me` 401 与网络错误在旧版同路处理（计划 §5.2 的改进项）。
3. `GET /api/admin/scheduled` 的 `types` 与前端内置 TASK_TYPE_TEXT 不同源（服务端多 scrape/scrape_avatars，无 watch/poll）——Vue 绑定服务端值即可。

## 16. 真实 JSON 夹具待采集清单（fixtures/json/）

后续在临时真实服务（临时 SQLite + 专用 Redis + 临时媒体目录）用 curl 采集并入库：

| 文件 | 端点与参数 | 目的 |
|---|---|---|
| auth-status.json | GET /api/auth/status（初始化前/后各一次） | initialized 分支 |
| auth-login.json | POST /Users/AuthenticateByName | AccessToken/User 结构 |
| libraries.json | GET /api/admin/libraries（0/1/2 个库） | Id/Name/Path |
| status.json / settings.json | GET /api/admin/status、/settings | 计数与设置字段 |
| items-wall.json | GET /api/admin/items?limit=100&offset=0&sort=datecreated&order=desc 及 status/search/library_id/scrape 各一 | Movie 混合大小写键、userdata、image_tags |
| item-detail.json | GET /api/admin/items/:id/detail（有图/无图/有分段/未探测各一） | movie/images/files/actors/modified_at |
| similar.json | GET /Items/:id/Similar?Limit=12 | Emby Item 键 |
| scan-progress.json | GET /api/admin/scan/progress（walk/process/完成/取消） | phase 语义 |
| probe-progress.json | GET /api/admin/probe/media/progress（running/取消/熔断） | aborted/cancelled |
| scrape-settings.json | GET /api/admin/scrape/settings | 掩码与 defaults |
| scrape-preview.json / inspect.json / confirm.json | 需 MetaTube 测试实例或本地桩（P1-E 的本地媒体源同法可桩上游） | 候选/diff/确认结构 |
| scheduled.json / validate.json | GET /api/admin/scheduled、POST /validate | params 字符串、next_run、types |
| apikeys.json / probe.json / tasks.json | 各 GET | 列表形状 |
| errors/ | 401/404/409/415/499 各一条原始 body | 错误形状与状态码 |

采集脚本与场景进入 P1-E（VUE-17）的 fixture；本表是输入清单。
