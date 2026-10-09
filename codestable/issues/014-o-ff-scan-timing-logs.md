---
kind: issue
title: 扫描阶段与落库批次耗时日志
type: ff
status: open
created: 2026-10-09
---

整库扫描默认输出开始、阶段切换、约每 5 秒的进展与结束汇总，用 scan_id/library_id 关联。文件遍历、指纹查询、源属性/图片选择、STRM/NFO 处理、稳定性复核、批次落库、清理、版本更新分别计时；落库继续拆分事务获取、SQL 准备、影片、演员、特征、指纹及提交，显示落库占比与最慢批次。

正常批次仅 debug 输出，至少 1 秒的慢批次在默认级别每轮最多每 5 秒一条，失败批次始终告警。快速正常局部刷新开始与汇总仅 debug 可见。取消与失败也记录最终耗时；失败批次不计成功影片数。数据库调用计时包含可能的连接/锁等待，不能单凭某阶段慢证明锁竞争；进展只在循环检查点输出，RescanOne 不纳入汇总。

- 改动：`internal/scanner/{scanner,performance,performance_test}.go`、`internal/store/{scan_write,scan_write_test}.go`、README。复用现有 slog/debug 开关；批次事务、读取前后稳定性检查、全量重读 NFO 与原有持久化设置保留。
- 验证：扫描/存储/图片模块全套、服务端扫描/重建/监听/取消相关测试、扫描与存储 go vet、全项目 go build 通过；新增真实扫描日志、无变化零批次、取消保留已写入结果、外键失败回滚及提交状态回归。阶段切换日志补充后重跑新日志测试通过，git diff --check 通过。Windows 极短计时可能为 0，已避免用耗时为 0 判断阶段未执行。
- 实测：本机 300 部同目录合成影片，单次首次扫描 total_ms=324.881、db_ms=147.376（45.36%）、db_features_ms=61.846、db_commit_ms=16.979；无变化扫描 batches=0、db_ms=0、skipped=300。样本不代表真实媒体库或 SMB/NFS，不用于宣称提速。
- codestable：仓库无 spec/epic 需同步，README 已补充字段范围、默认日志位置与判断方法。本地实现与验证完成，保留 open 等待真实媒体库运行观察。本条随日志改动提交，未推送或部署。
