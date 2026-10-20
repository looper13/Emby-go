---
type: ff
status: closed
date: 2026-10-19
---

# Emby 兼容收藏

- 改动：补齐人物、类型、标签、厂商、合集、电视节目与季、已绑定番剧文件夹的收藏；保留影片 userdata 和播放进度。启动自动创建独立实体收藏表，状态与子项互不继承，不写 NFO。
- API：复用 `POST/DELETE /Users/1/FavoriteItems/{id}` 和 `/Delete` 别名，返回 UserItemDataDto；支持根与 `/emby`、小写路径、`Filters=IsFavorite` 和 `IsFavorite=true/false`。命名实体列表先筛选再分页；根正向收藏合并媒体与虚拟项并去重。仍采用已有的单管理员模型。
- 界面：新增收藏页、详情与演员/剧集心形按钮、媒体墙和演员的仅收藏筛选；包含请求失败提示、重复点击保护、过期响应隔离、取消末页项后的回退，以及 Bangumi 外部 ID 跳转。
- 变更范围：`internal/store`、`internal/server`、`frontend/src`、对应测试和 README；无现存 spec/epic 需要回写，未改其他 issue。
- 验证：收藏持久化/老库升级、媒体状态保留、身份与鉴权、跨库缓存、正负筛选、混合分页、导演作品、暂隐恢复、番剧别名、文件夹独立状态、常用排序和不触发字幕发现的 Go 测试通过；store 全量及 server 回归通过，沿用排除 `TestMediaProbe|TestProbeCircuitBreaker` 的范围。
- 界面验收：使用项目临时媒体库与独立 Redis，在 1440x1000、390x844 浏览器中检查影片/演员/类型收藏、刷新保持、取消后列表更新、实体跳转、媒体墙仅收藏、电视剧/季/单集按钮；截图在忽略目录 `output/playwright/`，图片已加载、按钮无嵌套且手机无横向溢出。
- 构建与静态检查：Go vet、embedui 构建、Vue 类型检查和前端生产构建通过。前端全量测试仅已有 `baseline.test.ts` 的 `style.css` 字节一致性检查失败；没有为掩盖该既有问题而修改旧样式或基线测试。
- 边界：上述证据来自本地隔离环境，不代表真实 Emby 客户端、Linux、用户媒体库或生产部署已验收。本功能未提交、推送或部署。
