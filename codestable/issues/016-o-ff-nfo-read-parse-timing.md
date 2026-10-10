---
kind: issue
title: 拆分扫描中的 NFO 读取与解析计时
type: ff
status: open
created: 2026-10-09
---

真实扫描进展中 prepare_ms 占约 87%，现有日志无法区分 NFO 文件读取与 XML 解析。按用户要求在扫描性能进展与汇总增加 nfo_read_ms（os.ReadFile，包含打开、读取、关闭和等待）、nfo_parse_ms（xml.Unmarshal）、metadata_ms（影片字段转换与演员引用生成），以及 nfo_reads、nfo_parses、nfo_bytes。它们是 prepare_ms 的子项，不额外加入总阶段统计；读取/解析失败与 CD 回退尝试也累计，未变化跳过项为 0。

- 改动：internal/nfo/nfo.go 增加 ReadWithStats，复用同一读取与解析实现；原 Read 接口保持原行为且不采集计时。scanner 在准备阶段累加子项，RescanOne 不增加扫描汇总。未改变指纹、重读规则、事务或每文件日志策略。
- 验证：NFO 与扫描模块全套测试通过；新增普通/计时读取结果与错误一致、有效/空/损坏/缺失文件计数、主 NFO 缺失/损坏后的回退计数、失败不落库和无变化零读取、子项不重复计入总耗时回归。服务端扫描进度、NFO 探测版本、监听/轮询/局部失败相关测试通过（48.840 秒）。扫描/NFO go vet、全项目 go build 与 git diff --check 通过。
- 文档：README 更新字段、累计范围、子项关系与平均每次耗时算法，并修正 walk_ms 仍描述 STRM 属性读取的旧文字；仓库没有 spec/epic 需同步。

实现与本地验证完成，保留 open 等待真实媒体库日志。2026-10-10 用户要求与 017 接口补齐一起提交并推送；未部署。
