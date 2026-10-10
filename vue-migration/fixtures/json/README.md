# 真实 JSON 夹具（VUE-01 / P0）

来源：**真实 Go 服务**（临时隔离环境）用 curl 采集，非手工构造。历史采集脚本：`vue-migration/tools/live-env/collect-fixtures.sh`。2026-10-10 保留这 29 份契约夹具，只对 11 份中的本机临时路径作机械脱敏（前缀 `TEMP_LEGACY`）；重复运行应使用新的 Node `runner.mjs`，环境和证据见 `vue-migration/P0/live-env-notes.md`。补采 JSON 位于忽略的 `vue-migration/artifacts/live-env/`，不覆盖本目录。

- 服务：`emby-go-live.exe`（当前 main HEAD `5c45789` 构建）端口 **18099**
- 数据：临时 SQLite + 专用 Redis(6391) + 临时媒体目录 307 个条目（`make-fixtures.sh` 生成）
- **所有请求经 `X-Emby-Token` 认证；`auth-login.json` 中的 AccessToken 已替换为 `"TOKEN"` 占位，其余文件不含凭据。**
- 复现：见根 README 描述的阶段一/二命令，或直接重跑 `collect-fixtures.sh`（`LIVE_USER`/`LIVE_PW` 用一次性凭据）。

## 夹具清单

| 文件 | 请求 | 状态码 | 场景要点 |
|---|---|---|---|
| `auth-status-before.json` | `GET /api/auth/status` | 200 | 初始化**前**：`{"initialized": false}` |
| `auth-status.json` | `GET /api/auth/status` | 200 | 初始化**后**：`{"initialized": true}` |
| `auth-login.json` | `POST /Users/AuthenticateByName`（JSON `{Username,Pw}`） | 200 | `AccessToken`/`ServerId`/`User`（User.Policy 完整，tsukimi 严格反序列化依赖）；token 已置 `"TOKEN"` |
| `libraries.json` | `GET /api/admin/libraries` | 200 | 1 个库：`{items:[{Id,Name,Path}], total}` |
| `libraries-created.json` | `POST /api/admin/libraries`（`{Name,Path}`，键即 Go 字段名） | 200 | 创建返回单个 Library 对象（非包装）；配合 libraries.json 覆盖 0/1 库形状 |
| `scan-result.json` | `POST /api/admin/scan`（无 library_id=全部） | 200 | 首扫全量：`success:304, pending:3, incompatible:0, added:307`（同步长请求，见 notes 的耗时） |
| `scan-progress-running.json` | `GET /api/admin/scan/progress`（扫描中轮询） | 200 | `running:true, phase:"process", total:307, done:99`（walk/process 语义） |
| `scan-progress.json` | `GET /api/admin/scan/progress` | 200 | 完成态：`running:false, phase:"process", done=total=307, finished_at` 有值 |
| `status.json` | `GET /api/admin/status` | 200 | 计数：`{success:304, manual:0, pending:3, incompatible:0}` |
| `settings.json` | `GET /api/admin/settings` | 200 | 只读设置 + `cache_stats`/`redis_online`/`disable_library_monitor:true` |
| `tasks.json` | `GET /api/admin/tasks` | 200 | 内存任务表：1 条 scan `success`（含 started_at/ended_at） |
| `items-wall.json` | `GET /api/admin/items?limit=100&offset=0&sort=datecreated&order=desc` | 200 | 100/304 条；`Movie` **混合大小写键**；`userdata`（空）与 `image_tags`（含 217/299 两条，值为内容 tag） |
| `items-wall-filtered.json` | `GET /api/admin/items?limit=100&offset=0&status=pending` | 200 | 3 条待补录（id 301..303），标题空串 |
| `items-wall-search.json` | `GET /api/admin/items?limit=100&offset=0&search=深夜`（URL 编码 `%E6%B7%B1%E5%A4%9C`） | 200 | 中文关键词命中 9 条（含"有图"的 ABF-005） |
| `item-detail.json` | `GET /api/admin/items/5/detail` | 200 | **有图+中文标题**（ABF-005 深夜的测试影片）：`images` Primary(poster.png)/Backdrop(fanart.png)、`files`/`actors`（含稀有演员）、`playable`、`library_name`、`modified_at` |
| `item-detail-pending.json` | `GET /api/admin/items/301/detail` | 200 | 待补录：`Status:"pending"`、`NFOPath:""`、无图无演员、`files` 1 条未探测 |
| `similar.json` | `GET /Items/5/Similar?Limit=12` | 200 | 8 条 Emby Item（`Id/Name/ImageTags/RunTimeTicks/UserData/People/Studios/GenreItems/TagItems/…`），靠数据集里的稀有共享特征（深夜系列/演员乙）召回 |
| `probe-media-item.json` | `POST /api/admin/items/307/probe` | 200 | 成功单条探测：`files[].info`（ffprobe 完整 JSON：video/audio/format）与顶层 `info`、`mediainfo_path` |
| `probe-media-item-error.json` | `POST /api/admin/items/306/probe` | **502** | 死链（`http://127.0.0.1:1/x.mp4`）全失败错误体：`{"error":"全部文件探测失败: …"}` |
| `scrape-settings.json` | `GET /api/admin/scrape/settings` | 200 | 未配置态：`configured:false`、token 掩码为空、`defaults`（Go 字段名，如 `BaseURL`/`TimeoutSeconds`）与 snake_case 视图字段并存 |
| `scrape-unconfigured-error.json` | `GET /api/admin/items/5/scrape/preview` | **400** | 未配置 MetaTube：`{"error":"尚未配置 MetaTube 地址或 token"}` |
| `scrape-progress.json` | `GET /api/admin/scrape/progress` | 200 | 空闲态：`running:false, kind:"", total:0, cancelled:false` |
| `scheduled.json` | `GET /api/admin/scheduled` | 200 | 空列表 + `types`（probe/reindex/scan/scrape/scrape_avatars 中文名） |
| `scheduled-validate-ok.json` | `POST /api/admin/scheduled/validate`（`{"cron":"*/5 * * * *"}`） | 200 | `{valid:true, next_runs:[RFC3339 ×5]}` |
| `scheduled-validate-error.json` | `POST /api/admin/scheduled/validate`（`{"cron":"not-a-cron"}`） | 200 | **校验失败也是 200**：`{valid:false, error:"expected exactly 5 fields…"}` |
| `apikeys.json` | `GET /api/admin/apikeys` | 200 | 空列表形状 |
| `probe.json` | `GET /api/admin/probe` | 200 | 未知路径探针记录（先请求 `/no-such-path` 产生）：`{id,method,path,query,body,created_at}` |
| `errors/401.json` | `GET /api/admin/status`（**不带 token**） | 401 | `{"error":"unauthorized"}` |
| `errors/404.json` | `GET /no-such-path` | 404 | `{"error":"not found"}` |

## 采集命令模板

```bash
BASE=http://127.0.0.1:18099
TOKEN=...   # POST /Users/AuthenticateByName 获得
curl -sS -H "X-Emby-Token: $TOKEN" "$BASE/api/admin/status" | node <repo>/vue-migration/tools/live-env/jsonfmt.mjs
```

完整逐条命令、状态码输出与执行顺序见 `collect-fixtures.sh`（脚本即文档）。

## 数据集与关键语义（与夹具的对应关系）

- `L001..L300` 常规条目（`.strm`+`.nfo`），其中 5 条带 `poster.png`+`fanart.png`（ABF-005/088/142/217/299），ABF-042 标题含 `《》&"'<>`；`L301..303` 无 NFO（pending）；`L304..305` `.strm` 为 `ftp://`；`L306` 死链；`L307` 指向本地媒体源（18098）。
- **`incompatible` 实测为 0**：扫描不读取 STRM 内容判定协议（README §增量扫描），`ftp://` 条目因有 NFO 计入 success，`incompatible` 仅是保留字段。这不是采集失败，见 `P0/live-env-notes.md`。
- 媒体墙默认（无 status 参数）= success+manual 口径；pending 需显式 `status=pending`。
- `GET /api/admin/items` 响应有 5 秒服务端缓存、`Similar` 20 秒；夹具为首次请求结果。
