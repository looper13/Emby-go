# 在线动漫功能删除验证

2026-10-22：删除方案已成功应用到 Emby-go 工作树。此补丁已更新为最终手动 SQL 版本，不再含开机自动删除数据逻辑。

- 前端类型检查通过；26 个测试文件、281 个用例通过。
- 前端正式生产构建通过，内部 UI 已发布。
- go build ./... 与 go vet ./... 通过。
- 存储层全量测试通过；实际手动 SQL 在临时数据库验证，覆盖媒体/观看数据/正常授权保留、重复执行、序号保留、异常数据回滚。
- 旧数据库启动提示手动执行 scripts/remove_anime.sql，不自动清理旧数据。
- 服务端 137 个顶层测试分组运行：136 个通过；TestMediaProbe 缺少 probeversion=1 的断言在 HEAD 临时副本复现，属于原有问题。
- 每次单元测试设 60 秒超时，未运行会清理本机 Redis DB15 的 TestRedisBehavior。

数据库由用户自行执行 scripts/remove_anime.sql。实际数据库未连接或修改；独立 anime-catalog、anime-images 及本地 TMDB 配置文件由用户另行清理。未部署、提交或推送。

普通库启动的写锁竞争已修复；TestCoreAPI 连续 5 次通过，最终存储层全量测试通过。
