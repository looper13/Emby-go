---
kind: issue
title: 详情页选轨与外部播放器入口
type: ff
status: open
created: 2026-10-21
---

音轨/字幕选择移至详情播放区；选择随本次播放传递，不再在播放器中选择或跨详情记忆。媒体墙/总览直接播放使用默认轨道。动漫详情按条目展开设置，不在章节列表批量发现字幕。

```text
Detail: [Play] [existing actions]
        [Audio v] [Subtitles v] [External v]
                                  PotPlayer / VLC / MPV
Player: video + controls + errors + remux seek (no track selectors)
```

- 改动：`DetailPlaybackControls.vue`、`DetailPlayButton.vue`、三个详情页、`PlayerDialog.vue`、player store/track helpers；`PlaybackInfo` 增加仅鉴权播放响应可见的 `ExternalStreamUrl`，与原有源重定向一致，不混入账户凭据或共享缓存。
- 外部入口使用 `potplayer://`、`vlc://`、新版原生 `mpv://` 的 HTTP(S) URL 形式；使用客户端自身默认轨道，不传递网页选轨、服务器外挂字幕、续播/进度回报。需要用户本机安装并注册相应协议，本轮未改注册表、安装客户端或实测三款客户端唤起。
- 验证：5 个相关前端测试文件 82 项通过；类型检查、前端构建、Windows 普通/`embedui` 编译通过。外部播放/字幕/音轨定向 Go 测试通过。Go server 全量顶层测试 146 通过、1 跳过，仅原有 `TestMediaProbe` 失败（仍期待 probeversion=1/ref frames）；前端全量 279 通过，仅未改动的 style.css 字节基线失败（规范化换行后相同）。
- 提交快照复验：从暂存区导出独立目录，不包含账号/权限改动；5 个相关前端文件 81 项及前端全量 25 文件 262 项全部通过（原工作区的换行差异不在提交内），类型检查、前端构建、Windows 普通/`embedui` 编译通过；外部播放/字幕/音轨定向 Go 测试通过（18.457 秒）。上述全量 Go 及浏览器记录来自完整工作区，独立快照未重复全量 Go 或浏览器验收。
- 浏览器：独立 Go/Redis/HTTP 双音轨夹具，真实 Chrome 验证详情选择第二音轨后 HLS 正常解码 640x360、英文字幕正文显示；媒体墙直接播放恢复原流与默认中文字幕。播放器无选轨控件；390px 无横向溢出，外部菜单跨详情边界可命中；桌面/手机截图在忽略目录 `output/playwright/detail-*.png`。
- 边界：首次测试片关键帧间隔 50 秒触发原有重封装等待超时；改成 4 秒关键帧后通过，未改服务端重封装策略。字幕加载现在不会清除音轨失败提示。未验证生产片源、Safari/实体移动设备及原生外部客户端。
- codestable：无关联 spec/epic；更新 `SUBTITLES-AUDIO.md` 并在 025 标明交互替代。保留 open 等待原生客户端验收；本轮按用户授权单独提交、推送本功能，已有账号/权限等未提交改动保留。未部署。
