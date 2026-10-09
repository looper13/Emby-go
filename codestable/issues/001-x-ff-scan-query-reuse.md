---
kind: issue
title: 扫描查询复用与批量入库优化
type: ff
status: closed
created: 2026-10-08
---

同目录影片复用本轮图片选择与属性快照，更新后使用新查询复核稳定性；单文件事件在取得 STRM 属性前筛选影片/CD 分组。批次复用演员 SQL，关系不变时不重写；相似特征直接使用已解析数据，与普通更新共用规则。

- 改动：`internal/scanner/{scanner,directory,fingerprint}.go`、`internal/store/{store,scan_write}.go`、对应测试及 README。
- 验证：扫描器和存储全模块测试通过；监听模块及服务端扫描、监听、轮询和演员头像链相关测试通过；`git diff --check` 通过。未宣称服务端全套测试通过，上一轮全套失败未在本次排查。
- 基准：本机 Windows、300 部合成影片、`BenchmarkLibraryScan` 每场景 3 次。大目录首次 446→309 ms、全量 403→239 ms、无变化 142→13 ms；逐影片目录首次 220→212 ms、全量 221→153 ms、无变化 33→35 ms。文件系统耗时有波动，不能外推为 SMB/NFS 收益。
- codestable：无现有 spec 需同步；仅新增本条快改记录，未初始化其他工作区入口。指纹 v1、批次事务与全量重读语义保留。
