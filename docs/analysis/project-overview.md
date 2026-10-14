# 项目现状

现有 Go 服务使用 Gin、SQLite、Redis，管理端为 Vue。启动入口初始化业务库与系统动漫库；动漫浏览当前以 Mikan ID 为入口，打开详情后获取 Bangumi Subject/Episode，实体与原媒体通过 anime_media_bindings 关联。原 movie_id 拥有观看状态。

当前封面仅有有界内存缓存，没有角色、人物或 TMDB 章节图库。现有本地图片缩略图和 ETag 响应可以复用。

目标：独立、可替换的 Bangumi 目录快照负责全量检索；业务数据库继续持有稳定实体 ID 和人工关系。打开条目时按 Bangumi ID 将目录资料映射到现有本地主体/章节，不覆盖人工关系或原媒体。图片模块独立于目录更新。

参考 nz3u/bangumi-data 的 Archive/图片数据库分工、流式导入与按需图片思路，不复制 AGPL 源码。不采用删除旧数据库后再重命名的更新方式。正式上游为 bangumi/Archive、Bangumi v0 和可选 TMDB。
