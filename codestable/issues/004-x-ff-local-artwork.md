---
kind: issue
title: 按 Emby 命名扫描宽图与多张本地剧照
type: ff
status: closed
created: 2026-10-09
---

用户目录已有 poster.jpg、fanart.jpg、thumb.jpg 和 extrafanart/fanart1..10.jpg，但原扫描器只认单张 fanart/landscape，图片编号路由也忽略 index，导致宽图和多张剧照不能展示。

参考 Emby 官方 https://emby.media/support/articles/Movie-Naming.html#video-images，补齐 thumb/landscape、影片同名和 cover/default/movie 海报、backdrop/fanart/background/art 的数字命名及 extrafanart。支持 jpg/jpeg/png/gif/tbn，保留项目的 webp 与影片专属图片优先规则；数字按 1、2、10 排序。服务生成的专属背景不混入目录公共背景，但仍包含 extrafanart。

数据库自动补 backdrop_paths JSON 列，保留旧 backdrop_path 第一张兼容字段。扫描指纹升级 v2，把全部背景图纳入依赖；首次增量扫描补齐存量图片。目录图片目录表在扫描内复用，大目录按批复核清单，扫描中途增删图片时不保存稳定指纹。未转换或删除用户图片。

Emby 图片清单包含全部 Backdrop 的 ImageIndex/ImageTag，影片 BackdropImageTags 与编号取图一致，越界或非法编号返回 404。管理端详情展示剧照网格，懒加载带图片标签的缩略图并支持点击原图。上传背景同步多图索引；实时/轮询监控把 extrafanart 文件或目录事件定位到父影片目录，正确失效图片缓存。

- 改动：imageutil、scanner、store、librarywatch、server 图片/详情/缓存/上传、管理端详情样式及 README；无新依赖。
- 验证：截图结构的 11 张背景与 thumb 的扫描、数字排序、编号取图、清单与标签、保留时间戳覆盖、删除及目录移除通过；实时和轮询增删刷新通过；旧数据库迁移与关闭重开后的图片持久化通过；大目录中途新增图片的指纹重试通过；前端 node --check 与 git diff --check 通过。
- 全项目验证：go test ./... -skip '^Test(MediaProbe|ProbeCircuitBreaker)$' -count=1 -timeout 180s 通过。两项跳过的 ffprobe 测试为前一条快改记录中的已知环境/基线问题，本次未复验其失败原因。Go 构建缓存权限问题通过正常用户权限执行解决，未更改缓存权限。未运行真实用户媒体库或真实客户端。
- codestable：无 spec/epic 需同步，README 已同步规则与 v2 升级行为。未提交、未推送。
