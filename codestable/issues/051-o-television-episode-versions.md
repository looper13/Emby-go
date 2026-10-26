---
kind: issue
title: 同集多版本合并与 NFO 集数回退
type: feature
status: open
created: 2026-10-26
---

# 同集多版本合并与 NFO 集数回退

用户的同一动画集有长文件名和简短标题两套 STRM/NFO。剧集列表应只显示一集，在详情中选择播放版本；扫描仍保留各源文件与独立媒体信息。NFO 的 season/episode 无效时，若文件名有有效 SxxExx，则用文件名识别；有效 NFO 值仍优先。

原实现对每个 STRM 建一条剧集，且 NFO 中的无效集数会覆盖文件名中的有效集数。已在扫描器中限定 NFO 覆盖条件；TVEpisodes 按库、剧目录、季、集返回稳定的最早条目，TVEpisodeVersions 查询该集的所有可见源。详情页提供版本选择；PlaybackInfo 与条目详情提供多个 MediaSource，网页播放保留剧集进度 ID、使用所选源 ID 获取轨道和流。

验证：用用户提供的长文件名及同集简短文件名扫描，两条源入库但剧集列表只返回一集；接口测试覆盖双 MediaSource、选择版本、续播位置、流地址和非法版本 ID。前端 typecheck 与 build、相关 Go 测试和 vet。实际 NAS 数据与第三方客户端选择界面待部署后验证。

关闭前确认真实目录重扫后两套版本均能播放，并决定是否将多版本行为写入 Project Spec。按 CodeStable 约定，本事项先保持 open。
