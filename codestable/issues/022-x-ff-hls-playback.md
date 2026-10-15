---
kind: issue
title: 网页播放器支持 HLS 与 AES-128 视频
type: ff
status: closed
created: 2026-10-15
---

网页播放器原来只交给原生 video 加载，普通影片还将 HLS 当作 MP4。现在影片、番剧和预告片统一识别 m3u8，打开 HLS 时按需加载固定版本 hls.js 1.7.3，通过 Artplayer customType 处理清单、分片与 AES-128 密钥；无 MSE 时回退原生 HLS，不支持时提示。媒体绑定后触发自动播放，覆盖首个缓冲时间大于 0 的源。

- 改动：`frontend/src/lib/player-hls.ts` 新增 HLS 生命周期与错误接入；`player.ts`、`PlayerDialog.vue`、播放器 store、两个番剧详情入口接入类型识别和按需加载；package/lock 固定依赖；`frontend/public/licenses/hls.js.html` 分发上游声明与 Apache 2.0 文本。
- 可靠性：切源销毁旧引擎，关闭取消分片请求/worker，防止关闭后延迟创建引擎；致命错误进入原有一次代理回退与一次提示逻辑；媒体解码恢复最多一次，避免重试循环。
- 验证：类型检查通过；HLS、媒体交互与弹层单测 65/65；全量 222/223，唯一失败为未修改的 CSS 基线（37285/37284 字节，归一化 CRLF 后相同）。生产 UI 构建并发布到 `internal/server/web_dist`；Windows embedui 编译通过。
- 浏览器：真实 Chrome + 隔离 Go/Redis 临时环境，使用用户提供的 bananami AES-128 源，从普通影片入口经 `/Videos/1/stream` 302 自动播放 1920×1080；跳到 60 秒继续播放，无错误提示；关闭后视频节点清除、焦点回到播放按钮，重开成功；注入直连 403 后，真实 `/Videos/1/proxy` 返回 200 并恢复播放。原生 HLS 分支仅经单测，未在 Safari/实体移动设备验证；未部署到用户服务。
- codestable：无 spec/epic 需同步。后端代理保持原有字节透传；本次未新增分片、密钥或子清单的递归代理，源站这些资源仍需允许浏览器访问。
