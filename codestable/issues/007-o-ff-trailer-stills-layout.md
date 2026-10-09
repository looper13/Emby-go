---
kind: issue
title: 预告片与剧照分区，预告片封面优先使用剧照
type: ff
status: open
created: 2026-10-09
---

原来预告片卡片位于“剧照”标题下，卡片下方又显示“预告片”，与后续剧照网格的标题关系容易混淆。现在改为两个独立区块：预告片标题及播放缩略图在前，剧照标题、图片数量及网格在后；各自按有无内容显示。预告片缩略图优先取剧照列表第一张，保留实际 ImageIndex 和图片缓存标签；无剧照时回退现有背景图、海报或远程封面，不改变视频播放地址。

```text
预告片
[第一张剧照 + 播放按钮]

剧照 N
[剧照] [剧照] ...
```

- 改动：`internal/server/web/app.js`、`internal/server/web/style.css`，无后端或数据库改动。
- 验证：`node --check internal/server/web/app.js`、`go test ./internal/server -run '^TestAdminItemDetail' -count=1` 通过；本地 Node 夹具验证双区块、只有预告片、只有剧照、都没有、非零图片序号与标签编码、点击仍使用原预告片地址。浏览器在 380px 和 1280px 宽度检查分区及无横向溢出，确认缩略图与第一张剧照 URL 相同且图片加载成功。夹具未提供真实视频，未复验实际播放。
- codestable：无 spec/epic 需同步；本条接续 `006-x-ff-trailer-and-cover.md`，更新其中“预告片放在剧照区、使用远程 cover 作缩略图”的界面行为。实现与自动验证完成，待用户确认效果。
