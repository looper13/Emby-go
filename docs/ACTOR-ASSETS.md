# 演员头像与资料

在左侧「演员」页面（`/admin#/actors`）打开「头像来源与存储设置」，选择来源并显式保存，然后点击顶部「一键批量补齐头像」。GFriends 模式不需要 MetaTube 地址或 Token。影片刮削仍使用 MetaTube；新版本默认继续使用原来的 MetaTube 头像模式，不会静默启用外部数据源。

## 演员管理页面

- 顶部选择「全部媒体库」或指定媒体库，一键处理该范围内全部演员；不是逐人发起任务。按钮使用已保存来源，配置未保存、尚未配置或其他写入任务运行时不可重复启动。
- 统计显示范围内演员总数、已有本地头像和缺图数量；支持姓名搜索、头像状态筛选、每页 48 位。卡片显示姓名和范围内参演影片数，点击进入带演员筛选的媒体墙。
- 搜索、状态筛选和分页只影响列表，不缩小顶部批量任务范围。已有头像和非空 NFO thumb 保留；待恢复的资料/NFO 操作也会进入任务，所以候选数不一定等于缺图数。
- 页面显示任务进度、失败样例并可中止；切页不取消，刷新恢复进度。任务结束自动刷新列表。服务重启不会自动恢复任务，可再次发起补齐。
- 头像来源、GFriends 索引、旧目录与复制导入集中在演员页；MetaTube 连接和通用请求参数仍在「刮削」页。两页只提交各自编辑的字段。演员目录在「设置」页修改，重启生效。

## 列表接口约定

`GET /api/admin/actors` 需要管理端登录。接受 `library_id`（0 表示全部）、`search`（大小写不敏感的姓名子串）、`status=all|missing|ready`、`limit`（默认 48，范围 1–200）、`offset`（默认 0）。返回 `items`（`name/movie_count/has_image/image_tag`）、筛选后的 `total` 和所选媒体库全部演员的 `stats.total/ready/missing`。

只列有可见影片（success/manual）的演员，不显示孤立历史记录。统计在分页前计算，以现有本地图读取规则为准，不把远程 URL 或影片海报占位算作已有头像。读取不下载、导入或写入资料。

演员清单按媒体库作用域和版本缓存 30 秒，搜索、状态筛选、翻页共享同一份清单，并发加载会合并；收藏状态仍逐次读取，不必重建头像清单。冷加载一次聚合演员与影片数，批量检查资料分片和旧头像目录，避免每个缺图演员单独查库、打开不存在的资料文件。有资料的头像仍校验身份、路径、图片全文哈希及格式，损坏资料仍可回退旧头像。人物图片短缓存（有图 5 分钟、缺图 30 秒）继续复用；任务完成、扫描或保存目录设置会使清单失效，外部直接增删文件受图片缓存和清单缓存共同影响，可能延迟反映。

客户端 `/Persons` 及通过 `IncludeItemTypes=Person` 查询的列表只解析当前页图片，批量查询缺头像演员的最近可见影片海报；`EnableImages=false` 不查询图片，默认 `Limit=1000` 等分页约定不变。SQLite 启动时自动为已有数据库补建 `movie_actors(actor_name,movie_id)` 索引，无需手工执行迁移。

## 存储与备份

```yaml
db_path: "/var/lib/emby-go/emby-go.db"
actors_dir: ""  # 数据库同级 actors/；也可指定相对路径或绝对路径
```

相对 `actors_dir` 以数据库所在目录为基准；`db_path` 的相对路径以启动工作目录为基准。资料目录在实例配置中修改，重启生效，切换路径不会搬迁数据。

```text
actors/
  layout.json
  people/<姓名摘要前两位>/<完整姓名摘要>/
    actor.json
    images/<最终 WebP 内容摘要>.webp
    pending.json                        # 仅有未完成操作时存在
  cache/gfriends/<来源摘要>/
    Filetree.json
    http.json
  migrations/<旧来源目录摘要>.json
```

姓名摘要为修剪首尾空白后的原始姓名 UTF-8 SHA-256，保留大小写、全角和 Unicode 差异。目录不直接使用人物姓名。本地演员 ID、影片关系不变。索引里的姓名归一化只用于找图，不会合并本地人物。

`actor.json` 保存已选头像的尺寸、内容摘要、来源及已核验别名。来源可为 `gfriends`、`metatube`、`legacy_local` 或 `nfo`；`nfo` 表示从影片已有 URL 恢复，不能据此推断原始提供者。只保存无凭据的可重复访问地址，带私有查询参数的 URL 不写入资料清单。GFriends 的 `t` 图片版本参数保留。没有依据的生日、身高、简介等字段不创建。

别名按 `{ "name": "别名", "source": "gfriends/来源组" }` 或 `metatube/提供者` 保存，只采用唯一匹配记录中的姓名关系，不合并人物。早期字符串别名仍可读取，来源标为 `legacy_manifest`，表示证据来自旧清单。GFriends 头像另存 `source_version`（图片 URL 的 `t`）、`index_version`（索引内容摘要）及 `index_timestamp`（上游原始时间戳字符串）。网络头像有 `downloaded_at`，本地导入不编造下载时间；修复下载保留人物 `created_at` 和别名来源，更新下载时间。

NFO 决定影片演员、角色和影片元数据；资料清单决定本地头像。补全只写空的 NFO thumb，非空地址保留。与已选头像不同的地址会在清单的 `nfo_conflicts` 中记录，影片路径仅保存摘要，私有 URL 不记录。之后扫描 NFO 不会切换已经选择的本地图。

停服或等待全部写入任务结束后，备份配置、SQLite（含必要 WAL）、`actors/`、仍在使用的旧 `avatars/` 和媒体库 NFO。默认 systemd 数据目录为 `/var/lib/emby-go`，默认演员目录已在其中；自定义目录需单独纳入备份。`cache/gfriends` 可删除重建；`people`、`migrations`、未完成的 `pending.json` 应保留。一个根目录只由一个服务实例写入。

## 旧头像导入

旧设置 `avatars_dir` 继续用于兼容读取与导入，新写入统一进入资料目录。读取先找有效的新资产，再回退旧头像。

1. 保存「旧头像来源目录」。默认数据库同级 `avatars/`。
2. 点击「检查可导入头像」，查看待导入、已存在、冲突、损坏和缺失数量。预览不修改文件。
3. 点击「开始复制导入」。沿用任务进度与取消，离开页面不取消任务。

只复制能与当前演员唯一关联的图片，不移动或删除源文件、不改影片 NFO。不同演员共享旧摘要文件名时跳过并报告冲突。已有完整新头像优先。再次运行会保留已完成资产，继续未完成工作；导入记录保留来源、尝试次数和逐人结果。

回滚到旧版本前备份数据库、NFO 和资料目录。旧源头像仍在，因此旧版本可继续读取它们；旧版本不认识本次新增的 `actors/`，新下载的头像不会自动出现在旧版本中，回滚二进制也不会撤销已经补写的 NFO。

## 来源策略与故障

三种模式为 MetaTube、GFriends、GFriends 优先并回退 MetaTube。回退只用于未匹配或索引不可用；同名歧义直接跳过，不能通过另一来源自动消除。已有有效本地图不替换；旧图可唯一归属时优先导入；已有一致 NFO URL 时优先恢复该 URL，冲突时跳过。

默认索引为 `https://raw.githubusercontent.com/gfriends/gfriends/master/Filetree.json`；图片根地址为 `https://raw.githubusercontent.com/gfriends/gfriends/master/`，实际图片拼接 `Content/<来源>/<映射值>`。镜像必须显式填写对应索引与仓库根地址，程序不自动切换公共 CDN。

保留索引来源顺序，在确认是同一人物后优先靠后的高质量来源，再按主图、多图编号排序。最多尝试三个同人候选。图片下载并发为 2；最多重试一次 5xx，404/429 等直接报告并允许下次任务再试。索引刷新默认 24 小时，按需发生；每轮任务共享同一快照。测试来源强制检查索引，但不下载演员照片、不写 NFO。

索引解压后上限 24 MiB，图片上限 32 MiB，解码前像素上限 2400 万，重定向最多 5 次且拒绝 HTTPS 降级。公开请求不携带 MetaTube Token。索引刷新失败保留同一来源的旧缓存；更换来源不会串用旧数据。

发布先写完整图片，再以原子替换切换清单。任务串行提交人物资料、NFO 和索引；`pending.json` 保留中断位置。取消、NFO 写入或重扫失败时可能已有可显示头像，任务明确显示「部分完成」。再次运行会重新核对当前影片与路径，补齐剩余工作，不因数据库 URL 非空而漏掉修复。

选择某媒体库仅限制候选演员；该演员的资产全局共享，NFO 补写可能涉及其他库。这里没有人物跨库隔离，也不自动辨认同名的现实人物。

## 接口

| 接口 | 用途 |
|---|---|
| `GET /api/admin/actors` | 分页演员列表、头像状态和范围统计 |
| `GET/PUT /api/admin/scrape/settings` | 来源和旧目录配置；`actor_storage_dir` 只读 |
| `POST /api/admin/scrape/test`，`target=gfriends` | 测试并刷新已保存来源的索引 |
| `POST /api/admin/scrape/avatars` | 补全及恢复；支持 `library_id`、`limit` |
| `POST /api/admin/actors/import/preview` | 只读检查旧头像 |
| `POST /api/admin/actors/import` | 异步复制导入 |
| `GET /api/admin/scrape/progress` | 进度；导入类型为 `actor_import` |
| `POST /api/admin/scrape/cancel` | 请求取消当前头像或导入任务 |

上述管理接口沿用管理端鉴权。图片通过现有 Person 图片端点读取，不暴露整个资料目录。`configured` 仍表示影片刮削就绪，`avatar_configured` 独立表示头像来源就绪。

来源条款及分发边界见 [THIRD-PARTY-DATA.md](THIRD-PARTY-DATA.md)，实现与验收状态见 [actor-assets-plan.md](actor-assets-plan.md)。
