---
kind: issue
title: 图片校验标签与缓存扫描开销优化
type: ff
status: closed
created: 2026-10-09
---

图片 ImageTag/ETag 由修改时间和磁盘版本生成，保持字母数字格式；保留时间戳的覆盖不再错误返回 304。用户状态与媒体元数据分开计版本，心跳保留图片元信息热缓存，详情响应 TTL 缩短至 1 分钟。

批量刮削收尾按批取影片、按库复用信息，去重路径并在刷新前后各统一失效一次，包含新旧图片依赖；最后再发布影片和媒体库版本。局部扫描复用遍历得到的图片名称，指纹按源前缀缩小返回范围；精确 CD 分组、删除与稳定性复核保留，Unicode 名称和超过 32 个前缀时回退目录查询。

- 改动：`internal/server/{library,media,scrape,cache_policy}.go`、`internal/store/{store,cache_version,scan}.go`、`internal/scanner/scanner.go`、相关测试及 README。无新依赖或数据库迁移。
- 验证：新增图片 200/304、20 次心跳保留图片缓存并即时刷新详情、详情寿命、批量共享路径失效与新图片选择、字面前缀转义、Unicode CD 删除及 505 个 ID 批量读取回归通过；扫描、存储及其他相关模块测试通过。排除下列两项 ffprobe 测试后服务端全套通过；前端语法与 diff 检查通过。
- 验证限制：`TestMediaProbe` 的 `<reframes>1</reframes>` 是已记录的原有失败；`TestProbeCircuitBreaker` 当前版本与 Git HEAD 临时覆盖均在 60 秒超时。全项目运行中目录重命名监听测试偶发失败，单独连续 3 次通过。未运行 race（本机 CGO_ENABLED=0）。未宣称全项目全绿，也未宣称网络盘性能收益。
- codestable：无现有 spec 需同步；新增本条快改记录，README 已同步实际行为。未提交、未推送。
