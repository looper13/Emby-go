---
kind: issue
title: 详情页展示并播放 NFO 预告片，远程封面兜底
type: ff
status: closed
created: 2026-10-09
---

用户提供了一份其它刮削工具（MetaTube/JavBus 模板）产出的 NFO，其中用顶层 `<trailer>` 写预告片、`<cover>` 写来源站封面地址。查下来这是条**从没打通的链路**：`nfo.MovieMeta.TrailerURL()` 只认 `uniqueid type="trailerurl"` 与 `<trailerurlid>`，而且解析出来后没有任何调用方——不入库、不进接口、界面也不显示；`<trailer>` 与 `<cover>` 连解析都没有。

现在：预告片地址随扫描入库（`<trailer>` 作为第三种来源，优先级排在 uniqueid 与 `<trailerurlid>` 之后），详情页在剧照区块首位渲染预告片卡片，点击用内置播放器播放；`<cover>` 也入库，本地没有图片时作为封面/背景兜底（本地图始终优先），并作为预告片卡片的缩略图。

- 改动：`internal/nfo/nfo.go`（新增 `<trailer>`/`<cover>` 字段与 `CoverURL()`）、`internal/store/store.go`（新列 `trailer_url`/`cover_url`，打通 upsert 列清单、`movieCols`/`movieScan`）、`internal/scanner/scanner.go`（`applyMeta` 写入）、`internal/server/web/{app.js,style.css}`（预告片卡片、封面兜底、播放器封装抽出 `playOverlay`/`openTrailer`）与 README；无新依赖，数据库沿用启动时 `ALTER TABLE ADD COLUMN` 自动补列。
- 验证：新增三个单测——`nfo.TestTrailerAndCoverSources`（三种预告片写法优先级与 `<cover>`，含空白裁剪）、`store.TestTrailerAndCoverRoundTrip`（往返 + 覆盖更新不丢列）、`scanner.TestScanStoresTrailerAndCover`（NFO 到索引的整条写入）。`go test ./... -skip '^Test(MediaProbe|ProbeCircuitBreaker)$'` 全绿。浏览器实地走查：详情页剧照区出现预告片卡片、缩略图取 NFO 的 cover 并真实加载（`naturalWidth>0`）、点击后内置播放器打开且预告片**真的播放**（`paused=false`、`currentTime 2.46/3s`、`readyState=4`、无 media error）；另一部本地无任何图片的影片，hero 海报回退到 cover 并加载成功（400×600），剧照区因既无剧照也无预告片而不渲染。截图复核版式。
- 验证限制：预告片走浏览器直连源站，服务端没有代理端点——HTTPS 控制台里放 http 预告片地址会被混合内容拦截（本次夹具是 http 站点，未覆盖该场景）；unreachable 的预告片地址只弹「播放失败」提示，不静默失败；媒体墙卡片与 Emby 客户端的封面仍只认本地图（远程 cover 只在管理端详情页生效，客户端侧要生效需要服务端抓取缓存，本次未做，也未在 Emby DTO 里加 `RemoteTrailers`）。
- codestable：无 spec/epic 需同步；README 已同步预告片与远程封面条目。
