# 动漫库

动漫主入口以 Bangumi Archive 为目录，蜜柑补充发布资源。目录不依赖 NFO；原有媒体目录仍以 NFO 为元数据来源，播放必须关联已入库的可用 STRM。

本版本尚未发布，不提供旧动漫数据库结构的升级兼容或回退目录。正常使用中的目录更新仍保留当前绑定、章节身份和观看记录。

## 使用

1. 从侧栏“动漫”进入，点击“下载全量目录”。后台自动检查官方 manifest、下载全量 ZIP、验证长度和 SHA256，再将动画及关联资料导入独立快照；成功后才切换。不需要手工下载或上传 ZIP，首次点击前不会自动下载。
2. 按中文名、原名、首播年份和首播季度检索。这里的首播季度不同于蜜柑的放送季度。
3. 打开作品查看章节、角色与制作人员、每集剧照、发布资源。“刷新 Bangumi 资料”通过官方 API 补充较新的主体和章节信息。
4. 在章节或剧场版中“关联媒体”，选择已入库的单文件 HTTP(S) STRM，检查成功后播放。合集、多行 STRM 不自动拆分。
5. 蜜柑入口位于“蜜柑发布”，保留季度发布与搜索。种子、磁力只作为发布资料，不触发下载或订阅，不改写 NFO、STRM 和媒体文件。

“动漫”首页和“来源与设置 → 资料同步”均提供目录操作。已导入后按钮变为“更新目录”；每次先比较官方归档摘要，没有新版本时显示“目录已是最新版本”，不重复下载。下载时显示已下载/总大小及百分比，导入时显示当前阶段与已处理条数。支持取消、失败重试；刷新或重新进入页面仍能查询后台任务。失败/取消的下载不支持断点续传，重试会重新下载。

## 三类图片

| 类别 | 来源 | 存储与展示 |
|---|---|---|
| 动漫封面 | Bangumi Subject API | 多尺寸按需读取，列表与详情分开缓存 |
| 角色、制作人员头像 | Bangumi Character / Person API | character 与 person 使用不同身份，即便数字 ID 相同也不混用 |
| 每集剧照 | TMDB Episode Images | 仅使用确认章节返回的 stills，不拿作品封面冒充 |

Archive 不包含图片文件或完整图片 URL，需要 API 增强。图片字节在独立文件目录，SQLite 只记录来源、类型、摘要、尺寸、时间及状态。

按需加载最多 4 个抓取、128 个在途/等待键，合并相同图片请求。单图最多 4 MiB、2400 万像素；已归档图片复用 7 天，缺图冷却 24 小时、失败冷却 5 分钟，失败保留上次成功文件。远程请求限于明确的 Bangumi/TMDB 域名并校验公网 IP。

“归档本页封面”和“归档本作封面与头像”每批最多 50 部作品、500 张图片，展示完成、缺图、失败数量。可取消并重新执行，已完成图片复用。取消停止后续调度，共享的在途单图可以完成；服务退出会取消所有网络工作。重启不自动恢复批量任务，也不自动下载全量图片。

## TMDB API Key

在“来源与设置 → TMDB 剧照”配置 v3 API Key，可显式保存、恢复草稿、清除和测试已保存连接。接口只返回是否配置及版本号，不回传密钥；页面不会预填完整密钥。

新建配置的初始值按以下优先级读取：

1. 环境变量 `TMDB_API_KEY`。
2. 主 YAML 配置的 `tmdb_api_key`。
3. 主配置同级 `anime-tmdb.local.yaml` 的 `tmdb_api_key`。

本机已将用户提供的默认值放入 Git 忽略的 `anime-tmdb.local.yaml`，不会进入源码、前端或构建产物。部署时需要自行提供该本地配置或在管理页保存。初始化后以数据库保存值为准，主动清除后重启不会恢复默认。

先关联 TMDB 剧集，再为每个 Bangumi 章节选择明确的季和集。服务核验远端章节 ID，检查过期修改和重复目标，最多保留每集 20 张剧照。已存在章节映射时更换剧集需先解除相关章节映射；解除后旧剧照地址失效。没有 Key、没有映射或上游没有剧照，都不影响基础目录和已有媒体播放。

This product uses the TMDB API but is not endorsed or certified by TMDB.

## 目录与同步

- 独立目录位于数据库同级 `anime-catalog/`，`current.json` 指向已验证的 `catalog-*.db`。
- 下载上限 1 GiB，解包总量上限 16 GiB；必须具有八个核心 JSONL 文件，流式解析并只保留动画与相关章节/人物。
- 失败、取消、配置变更或禁用动漫库不会发布半成品。当前快照保持可用，旧快照文件不自动删除。
- 首次成功导入后创建每周三 05:30 的“Bangumi 目录更新”计划任务，使用服务器时区。计划任务页可调整 cron 或停用。
- 蜜柑的资料同步单独管理；仍采用原有缓存、来源限流与范围限制，不负责定义 Bangumi 全量主目录。
- 官方 API 新资料和归档资料分别记录来源时间，较旧归档不会覆盖较新的 API 增强。

## 播放与 Emby

只有已有媒体且检查通过的条目进入 Emby 动漫库。TV、OVA、WEB 映射为 Series → Season → Episode；正片使用 Season 1，特别篇和小数编号使用 Season 0；剧场版为 Movie，未知媒介不冒充 TV。

章节原始编号与稳定 Emby 整数编号分开保存。目录更新不会重建当前业务 ID，不再出现的章节标记为非活动并保留关系。动漫和原媒体共用原 movie_id 的进度、已看与收藏状态；替换绑定不复制历史。

作品/季封面与章节剧照分开处理。章节优先使用人工确认的 TMDB 剧照，否则保留原媒体图片，不用作品海报伪装每集剧照。具体 Emby 客户端尚待实机验证。

## 存储与备份

| 数据 | 位置 |
|---|---|
| 媒体、观看记录、人工关联、TMDB 设置与映射 | 主 SQLite |
| 可替换的全量目录快照 | `anime-catalog/current.json` 和其引用的目录 DB |
| 图片索引 | `anime-images/images.db` |
| 图片文件 | `anime-images/files/{subject,character,person,episode}/{id}/{sha256}` |
| 本机默认凭据 | 主配置同级 `anime-tmdb.local.yaml`，敏感文件 |

停服并确认数据库连接关闭后备份主库、两个资料目录、本地凭据及配置；仍存在的 WAL/SHM 文件也属于同一备份集。在线备份使用 SQLite Backup API 或 VACUUM INTO，不只复制活跃 .db 文件。仅备份 NFO 无法恢复关联和观看状态。

恢复时先保留当前资料副本，再恢复一致的备份集和媒体路径。图片索引和文件目录一起恢复；缺失文件会在下次请求重取。目录快照可以重新导入，主库中的人工关系不可由归档重建。旧快照的人工清理应在停服和备份之后进行，不能删除 current.json 引用的文件。

## 验证边界

2026-10-14 已完成定向 Go 测试、前端类型检查、213 个单元测试、11 个构建测试、Vue 构建、桌面和手机 E2E。浏览器使用隔离数据库、Redis、媒体及合成图片，覆盖主目录、分类头像、剧照、媒体绑定、播放、设置保存和重启。

下载入口追加验证：服务端通过合成归档测试完整 POST 下载/导入、重复点击、摘要相同不下载、请求/读流取消及重试；前端覆盖首次导入、状态查询失败后继续轮询、进度、取消、失败重试和服务重启后的中断提示。桌面/手机截图包含模拟下载进度，不代表真实全量包已经下载。

全量 Go 回归排除了会清空本机 Redis DB15 的 TestRedisBehavior；仍有既有 TestMediaProbe 的 NFO reframes 断言失败。本次未修改该无关测试。TMDB 实网验证发生连接失败，未获得鉴权结果，尚不能确认用户 Key 有效。真实全量 Archive 的体量与耗时、真实图片批量归档、Linux 部署和具体 Emby 客户端仍待运行环境验收。

本机 Go 未启用 CGO，竞态检测未运行成功；普通并发测试不等同于 race 检测。

```powershell
go test ./internal/config ./internal/anime/... ./internal/store ./internal/server -run 'TestTMDB|TestArtwork|TestAnime|TestCatalog|TestArchive' -count=1
npm --prefix frontend run test:go-regression
npm --prefix frontend run typecheck
npm --prefix frontend run test:unit -- --run
npm --prefix frontend run test:build
npm --prefix frontend run build
$env:PW_CHANNEL='msedge'
npm --prefix frontend run test:e2e -- anime.spec.ts
```

## 来源

- 官方数据合同：bangumi/Archive、Bangumi API、bangumi/common 的平台常量。
- 剧照合同：TMDB v3 的 TV Episode Images 和 API Key 鉴权。
- 设计参考：nz3u/bangumi-data，核对版本 bb747dfc3dcb9b5240549263d8e788054cc60e46。参考其独立目录/图片索引思路，不复制其 AGPL 实现到本 MIT 项目，也不假设它包含 TMDB 或图片文件下载。
