---
kind: issue
title: 可选下载 MetaTube 全部剧照与本地预告片
type: ff
status: closed
created: 2026-10-23
---

- 刮削配置新增独立的 `download_stills` / `download_trailer` 勾选项，默认关闭，单条/批量/定时复用；MetaTube Token 可为空。接入 `preview_images`、`preview_video_url` 和 `preview_video_hls_url`，NFO 保存远程预告片地址。
- 剧照保存为影片专属 `*-fanart1.webp` 等，按数字排序并沿用图片缓存失效/监听机制，同目录影片不串图；MP4 流式下载到 `*-trailer.mp4`，临时文件校验后才替换，详情优先播放本地副本，保留 Range/HEAD 和现有匿名媒体访问/已登录用户库权限规则。资源失败保留警告与已完成文件，可按只补缺失策略重试。
- 改动：`internal/metatube`、`internal/scraper`、`internal/imageutil`、新增 `internal/mediaassets`，服务端刮削设置/详情/预告片接口，Vue 刮削配置与反馈；README 已说明文件名、重试、MP4/HLS 边界和网络配置。
- 验证：MetaTube/图片/刮削包测试通过；服务端刮削、账号/库权限、代理/图片/详情等定向回归通过；新增并发批量、独立开关、跳过/覆盖、取消/失败保留旧视频、下载大小限制、空 Token 设置及本地 Range/HEAD 测试。`npm run typecheck`、P5 单测 10 项、前端构建及桌面/390px 手机设置页浏览器回归 2 项通过，截图复核下载选项布局；`git diff --check` 通过。
- 实测：`http://192.168.1.4:8080`、空 Token、`ALDN-001` 精确命中 FANZA/aldn00001；完整下载 20 张剧照和 42,783,977 字节 MP4，ffprobe 识别 H.264/AAC、720×404、114.948104 秒，重复只补缺失没有再次下载。样本保存在忽略目录 `.work-cache/metatube-aldn-001/`，没有写现有媒体库。
- 网络边界：本机直连 DMM 最初出现连接重置，设置 `HTTP_PROXY/HTTPS_PROXY=http://127.0.0.1:7890`、内网地址加入 `NO_PROXY` 后完整实测通过；未把代理或该 MetaTube 地址硬编码为默认配置。仅 HLS 的预告片继续代理在线播放，本期不合并 HLS 分片；未部署到现有服务，未做原生 Emby 客户端或完整人工播放验收。
- codestable：没有 spec/epic 需同步；README 已更新。
