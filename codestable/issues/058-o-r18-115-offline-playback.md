---
kind: issue
title: "R18 磁力到 115 离线任务、文件选择与 302 播放"
type: feature
status: open
created: 2026-10-28
---

# R18 磁力到 115 离线任务、文件选择与 302 播放

## 目标与边界

承接中断聊天中已确认的流程：管理员在 R18 详情选择 article 资源或手动磁力，创建本地任务，后台向 115 提交和对账，完成后确认视频文件并绑定播放源；网页或 Emby 播放时按实际 User-Agent 获取直链，返回 302。

PostgreSQL 仍为只读资料来源，任务和绑定只写本地 SQLite。不生成 STRM，不扫描整个网盘，不引入 WebDAV/rclone、视频代理或转码；R18 收藏、已看/进度、跨库聚合仍不在本次范围。仅管理员写任务，普通账号按既有媒体库授权播放就绪文件。2026-10-29 用户明确追加 Cookie-only 配置、统一 `缓存-24h自动删除` 目录及到期自动删除；此前“不自动删除网盘文件”边界被收窄为“只删除本服务创建的任务缓存，不动外部目录或旧任务文件”。

## 当前实现

- `internal/cloud115` 隔离 SDK：BTIH 十六进制/Base32 规范化、逐请求限流、受控刷新并持久化 Token、带取消的客户端串行门、离线列表分页与单项提交错误、目录扫描及直链。明确修正 SDK 的 GET 表单分页和提交单项错误被忽略的问题。
- `internal/store/cloud115.go` 新建连接、任务和 R18 绑定表。账号和 InfoHash 唯一去重，任务更新采用版本 CAS；绑定包含媒体库配置修订，防止源库换库后复用旧条目。重新打开 SQLite 保留凭证、提交中状态和绑定，旧客户端刷新不能覆盖新配置。
- `internal/offline` 独立于 cron 的常驻队列；先查远端再提交，并在远端写之前持久化 `submitting`。提交超时和重启先对账，绝不盲目重复提交。取消阻止旧版本发布结果，不删除网盘任务/文件。完成单个视频自动绑定，多个候选待选；凭证失效、有限退避、24 小时等待上限和阶段重试均保留配置及文件身份。
- `internal/server/cloud115.go` 管理员配置/资源核验/文件选择接口、用户可见播放源及 302。源身份、授权、用户启用状态、媒体库修订在每次播放前检查；FileID/SHA1 确认后使用最新 PickCode 与真实 UA 获取 URL。匿名旧本地流行为不变，115 必须有账号认证或 6 小时的条目/来源/用户绑定签名票据，重启票据失效。HEAD 和 302 不传正文，不缓存或记录签名地址。
- 凭证使用 AES-GCM，数据目录 `.115.key` 使用 Unix 0600 / Windows 受保护 DACL；接口不回显凭证，错误和日志脱敏。设置页及保存 API 改为只填 Cookie，验证成功后自动创建/复用命名缓存根目录，无 Token/文件夹输入；当前仅支持同账号更新，不支持切换共享账号。旧加密 Token 保持可解密，既有任务不自动搬动或删除。
- R18 详情增加任务状态、正片选择、默认版本、播放和 PotPlayer 入口；磁力复制保留。页面每 5 秒只读本地状态，离开页面终止页面轮询但不取消后台任务，刷新失败保留已有内容。设置页 Cookie 为密码框，保存后清空。

## 2026-10-29 增量设计与执行

- Cookie HTTP 适配器隔离在 `internal/cloud115/cookie.go`；保留原 Provider 主接口，缓存写操作通过 CacheProvider 承担。仅引用 `115driver v1.3.5` 的 RSA 编码模块，避免无取消的整套客户端；HTTP 限流、超时、重定向禁止、响应大小限制、混合数字/字符串 ID 和 Cookie 完整值/字段值脱敏在适配器内处理。
- 本地任务增加缓存子目录 ID、绝对到期时间。首次 mkdir 之前持久化清理意图，目录名称由任务 ID 确定，mkdir 超时/取消也能重新定位并清理；提交仍持久化 submitting 后远端写，重启不盲目重提。缓存从开始创建计时 24 小时，重试/播放/取消不续期。
- 清理独立于活动任务轮询，覆盖就绪、失败和取消状态，先持久化 cleanup_pending，使旧 CAS 结果不能复活；核对命名根目录/父子关系/专属子目录名字，停止写入该子目录的远端任务后删除子目录，保留统一根目录、外部同磁力任务和其它文件。错误退避重试并在重启后继续。到期来源列表、文件选择和已有播放票据均立即拒绝，清理完成后重提交可重新下载同一磁力。
- 质量目标：可靠性以真实 SQLite 关闭重开、mkdir/delete 超时、取消/到期清理和重新提交测试验证；信息安全性/数据保护以 Cookie 不回显、到期票据拒绝、目录移动/改名/外部同磁力不删除测试验证；交互能力以 Cookie-only 表单、保存后清空、清理状态/时间提示和桌面/手机浏览器夹具验证。

目标局部界面（沿用原有设置面板与详情位置，不重设计其它区域）：

```text
[115 Connection]
Cookie [password input]    [Validate and save]
Cache: fixed root / owned task child / delete after 24h
Error or saved status

[115 Playback Sources]
Task status | progress
Absolute deletion time / cleanup failure reason
File choice + play (only while unexpired)
Resubmit magnet after cleanup completes
```

加载失败保留原面板并提供重载；保存中禁用控件。手机表单变单列。清理中无取消/续期动作，已到期文件不显示候选。真实 115 凭证、实际加密成功响应及客户端视频播放仍需现场验收，不以 HTTP 响应夹具替代。

## 有界取舍

只保存文件身份，不持久化临时直链；第一版播放时重新获取直链，不增加签名 URL 缓存。旧 OAuth 连接仍使用 SDK 的接口拒绝后刷新，新 Cookie 失效后由管理员更新，不自动获取 Cookie。账号统一至少 1 秒间隔，队列串行处理到期任务，每次读取最多 50 个任务，另有每批最多 50 个缓存清理任务；目录最多 10000 项/16 层，任务对账最多 100 页，超限不能发布部分结果。大量并发离线任务尚未做吞吐量验收；如果队列规模增长导致播放等待或重复列表查询成本显著，应升级账号级任务快照和调度，而非让网页直接访问 115。

旧 `embybackup` 只备份 SQLite/NFO，不包含 `.115.key`；需另行保密备份密钥与 SQLite。当前无真实凭证，不能声称真实 115 闭环、出口 IP/额外请求头兼容或原生 Yamby 验收完成。

## 验证记录

2026-10-29 增量验证（不覆盖真实网盘验收）：

- 最终 `go test ./internal/cloud115 ./internal/offline ./internal/store ./internal/server -run 'Test(Magnet|Vault|SDK|RateLimiter|Discover|Offline|Cloud115|Cache|Cookie)' -count=3` 通过。覆盖 Cookie HTTP 请求/分页/字段/UA/配额拒绝、无效 RSA 响应防崩溃、自动目录、到期/失败/取消清理、外部任务保护、目录移动/改名拒删、mkdir 超时后的无 ID 孤立缓存恢复、真实 SQLite 关闭重开与删除失败重试、清理后重提交及已有播放票据到期拒绝。
- 最终类型检查、36 文件/360 前端单测通过；Vue 构建并发布本地 embedui 资源通过；Go vet、`git diff --check` 和 `go build -tags embedui -o output/emby-go-cookie24-check.exe ./cmd/metatube` 通过。构建只有既有 Vite 大分块提示，diff 只有 Windows 行尾转换警告。
- 1440/390 像素浏览器响应夹具验证 Cookie-only 保存请求（只有 cookie/revision）、保存后密码框清空、旧 Token/目录输入不存在、44px 保存按钮、无横向溢出、清理/已删除状态与到期无播放/续期动作。截图在 `output/playwright/cookie24/`；仅隔离 115 面板，设置页其它模块的响应格式错误来自简化夹具，不作为全设置页验收。未提供真实 Cookie，未验证成功加密响应、真实离线完成、网盘删除或客户端 Range 播放。

2026-10-28 原 OAuth 版本验证记录（历史证据，不能代替 Cookie 实测）：

- 聚焦 Go 测试覆盖：规范化、凭证加密重开/篡改、SDK 自动刷新与分页、单项拒绝、真实 UA、目录候选/循环、并发去重、提交超时恢复、取消 CAS、超时/重新授权、单文件自动绑定、SQLite 关闭重开、旧刷新写冲突、HTTP 权限、伪造资源/文件、票据篡改/撤权、HEAD/302 和远端删除。新流程测试连续 3 轮通过；后续新增 SQLite 重开测试和最终修改再做聚焦检查。
- 前端类型检查及 36 文件/359 单测通过；Vue 构建通过并发布本地 embedui 资源。Go vet、格式/diff 检查和最终 embedui 构建通过，不发布产物。最终最新 Go 聚焦测试再重复 3 轮通过，包含真实 SQLite 关闭重开与客户端排队取消。
- 浏览器使用明确的隔离响应夹具，验证 1440/390 像素、多个候选选择、默认播放版本、播放器打开/关闭、Token 保存后清空和设置页无横向溢出；主要操作按钮 44px。播放器夹具无真实视频，不把打开对话框当作视频成功播放。证据在忽略目录 `output/playwright/115/`。
- 广泛 Go 回归 496 个顶层测试通过；旧模块 `TestEpisodeNFOReadSaveAndProbePreserveRootAndNumbers`、`TestDownloadImageConcurrentUniqueTemp` 出现 Windows rename Access denied，独立 3 轮复测通过；`TestPollingMonitorDebouncesRetriesAndTracksMoves` 仍有目录归并/改名断言失败。三模块未改动。排除会清理本机 Redis DB15 的 `TestRedisBehavior` 及既有 `TestMediaProbe`/`TestProbeCircuitBreaker`；CGO=0 未跑 race。后续最新增量由聚焦测试验证，不声称全量通过。

实现与本地验证完成，按用户要求提交作为测试交付，待真实 115 和客户端验收；未部署，事项保持 open。临时 Vite 和浏览器已关闭。没有现有 Project Spec/Notes，本次同步 R18-CATALOG.md 与 README；后续关闭可毕业任务权限、持久化边界、票据和密钥备份要求。
