---
kind: issue
title: 演员列表与人物图片批量查询优化
type: ff
status: closed
created: 2026-10-20
---

# 演员列表与人物图片批量查询优化

- 后台演员清单按媒体库与版本缓存 30 秒，翻页、搜索、状态筛选和并发请求复用；收藏实时读取。消除超过 8192 位演员后共享图片 LRU 循环淘汰导致的每页全量重读。
- 冷加载批量读取演员索引、资料分片与旧头像目录，保留本地图真实性、哈希/格式校验、损坏资料回退和只读约束。目录预筛选兼容大小写不敏感文件系统；未索引的人物不显示磁盘孤立头像。
- `/Persons` 与 Person 类型列表仅处理当前页图片，批量查询最近可见参演影片作为海报回退；禁用图片、默认分页、作用域及返回字段不变。为存量数据库自动补建演员反向索引，并优化单个人物海报查询。
- 改动：`internal/server/{actors,person_images,compat_catalog}.go`、`internal/store/{actor_summary,actor_queries,store}.go`、`internal/actorassets/{assets,batch}.go` 及对应测试；接口说明同步到 `ACTOR-ASSETS.md`。
- 性能：本机 Windows 临时库 9000 演员、18000 影片、180000 条关联，均无本地图；`BenchmarkActorQueries -benchtime=3x` 后台冷查询约 167 ms、热查询/搜索约 8 ms、`/Persons` 冷查询 1000 人约 235 ms。此前同规模复现约 3.1 s 和 8.9 s。模拟数据不等同真实媒体盘、已有大量图片或线上环境收益。
- 验证：先复现清单跨页/并发复用失败，再验证修复；演员、收藏、分页、版本失效、取消、批量分片、旧库索引升级、头像完整性与大小写定向测试通过。store/actorassets/scraper/cache 全套通过；`go test ./... -skip='^(TestMediaProbe|TestProbeCircuitBreaker)$' -count=1 -timeout=180s` 通过；最后的大小写修正另有定向回归。`go build ./...`、相关包 `go vet` 和 `git diff --check` 通过。
- 边界：上述两项历史探测异常未纳入全项目回归；Windows 无符号链接权限，越界符号链接用例跳过；CGO 关闭，未运行 race。未部署，未验证真实 Emby 客户端、SMB/NFS 或 Linux 实机；未提交或推送。
- 对 `codestable/` 的影响：补充本次 ff；仓库无相关 project spec，现行接口文档已同步，既有事项状态不变。
