# 范围内模块

| 模块 | 当前职责 | 变化及复杂度 |
|---|---|---|
| internal/anime | 上游客户端、Mikan 解析、模型、资源检查 | 增加 Archive 目录与分类型图片获取；高 |
| internal/store/anime*.go | 本地实体、章节、人工映射、媒体绑定 | 保留 ID；补目录资料同步/来源元数据；高 |
| internal/server/anime*.go | 同步、管理 API、播放与 Emby 展示 | 增加主目录和独立任务；高 |
| frontend/src/pages/Anime*.vue | 季度、详情、设置 | 切换目录入口，增加人物/图片/同步状态；中 |
| frontend/src/api/anime.ts | API 契约验证 | Bangumi 主入口与可选蜜柑入口分工；中 |
| internal/server/media.go | 本地图片缩略图、播放、用户状态 | 复用现有行为，不改播放模型；低 |

不涉及扫描器重写、缓存后端更换、通用演员库迁移或部署方式调整。
