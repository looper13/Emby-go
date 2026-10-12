---
kind: issue
title: "播放器基线哈希跨平台换行修复"
type: ff
status: closed
created: 2026-10-12
---

# 播放器基线哈希跨平台换行修复

Linux 构建使用 LF，Windows Git 检出可能使用 CRLF。原测试固定了 CRLF 文件哈希，导致 Linux 单测误报。现在计算哈希前统一为 LF，并固定对应哈希，继续校验冻结的播放器内容。

- 改动：`frontend/tests/baseline.test.ts`；播放器文件、版权头与许可证校验保持原样。
- 验证：前端单测 13 个文件、189 个用例全部通过；类型检查通过；单独验证 LF、CRLF 输入归一化后的哈希均符合基线。单测首次受沙箱 `spawn EPERM` 限制，沙箱外重跑通过。未执行 Linux 完整构建。
- codestable：无规格变化，仅新增本条快改记录。
