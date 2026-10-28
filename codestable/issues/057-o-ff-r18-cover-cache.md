---
kind: issue
title: "R18 列表海报与按需两级封面缓存"
type: ff
status: open
created: 2026-10-28
---

# R18 列表海报与按需两级封面缓存

实现完成，用户已授权提交推送，待实际环境使用确认；未部署。

- 改动：网页默认封面使用最大 320×480、质量 82 的 WebP 海报，与 Emby 默认 Primary 共用。缓存先查成品再读原图；同键并发合并，新的尺寸复用原图，派生图不延长原图有效期。剧照仍只在详情按需懒加载。
- 文件：`internal/cache/files.go` 提供有界哈希文件缓存，`cache.go` 增加单项热缓存字节上限；`internal/server/r18_cover_cache.go`、`r18_images.go`、`r18_artwork.go` 和 `server.go` 接入。热缓存最多 256 项/每项 128 KiB，磁盘最多 512 MiB/10,000 文件，有效期 24 小时；过期/损坏后重新加载，磁盘故障回退有界内存。源库连接修订、图片 URL 和处理参数进入缓存键，所有缓存/HEAD/304 前检查授权，不转发凭据、不预下载全库。
- 验证：缓存/图片聚焦测试连续 3 轮通过，覆盖大于旧 512 KiB 阈值的原图、网页/Emby 共享、并发、重启、过期、完整性、容量、磁盘故障和授权撤销；补充真实 HTTP 派生图有效期测试。类型检查、35 文件/354 前端单测、Vue/embedui 构建、Go vet 与格式检查通过。
- 页面：真实来源样例返回 319×480/45,338 字节 WebP，网页和 Emby 海报字节一致，HEAD 为 200 且无响应体、ETag 为 304；1440/390 像素页面、详情返回、图片隐藏及剧照懒加载通过浏览器检查。此证据不是原生 Yamby、Linux 或大规模生产验收。
- 回归边界：广泛 Go 回归通过 488 个顶层测试，监听测试 `TestPollingMonitorDebouncesRetriesAndTracksMoves` 的目录改名断言失败且单独重复仍出现，监听模块未改动。排除 `TestRedisBehavior`（清理既有 localhost Redis DB15）、`TestMediaProbe` / `TestProbeCircuitBreaker`；未跑 race（CGO 关闭且无 C 编译器）。先前 Windows 刮削并发改名失败在本轮独立 3 次及广泛回归中通过，刮削模块保持未改动。
- codestable：没有现有 spec/notes；已同步 `R18-CATALOG.md` 和 README，不改写此前图片代理、尺寸、筛选和浏览记录的历史边界。此次提交包含上述 R18 工作及其共享卡片控件/窄屏样式依赖，不包含临时预览、凭据或编译产物；不扩展为部署或事项关闭。
