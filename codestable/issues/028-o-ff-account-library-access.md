---
kind: issue
title: 单账号登录与媒体库访问权限
type: ff
status: open
created: 2026-10-20
---

# 单账号登录与媒体库访问权限

- 范围：用户明确不做多用户；继续现有单管理员实现，参照 Emby 认证与 `EnableAllFolders` / `EnabledFolders` 内容访问策略。
- 实现：账号设置页面、退出并撤销当前令牌、验证当前密码后修改账号并撤销所有密码会话；权限持久化、SQL 分页前过滤、详情/图片/播放入口检查、浏览 Cookie 与写请求令牌分离；保留完整后台管理与扫描范围。
- 本轮补齐：修复未使用导入导致的构建失败、认证头中首项 Token 解析、账号/权限并发保存、动漫及刮削图片公共缓存、权限改变后合集名称缓存未失效；新增账号前后端回归。
- 改动：`internal/server/{auth,account,library_access,server,library,cache_policy,media,anime_*,scrape}.go`、`internal/store/{account,store,catalog,television,anime_projection,actor_queries,actor_summary}.go`、Vue 账号页/API/认证状态/导航及相关测试；说明见 `docs/ACCOUNT-ACCESS.md`。
- 验证：修复前复现首项 Token 被拒绝、合集缓存包含撤销库；修复后账号 server/store 回归通过。前端账号与认证 31 项通过；全套 258/259，通过外的 CSS 字节冻结用例为未修改文件的既有问题。类型检查、正式 Vite 构建及相关包 go vet 通过。
- 完整 Go 回归：最终合集缓存修复后，`go test ./... -skip '^Test(RedisBehavior|MediaProbe|ProbeCircuitBreaker)$' -count=1 -timeout 240s` 全部包通过；排除项分别为清理本机 Redis DB15 的测试及已记录异常的两个媒体探测测试。Windows polling 监听首轮偶发失败，单独与后续全套重跑通过。账号、剧集、Emby/核心接口、缓存及人物定向回归也通过；最终账号/认证 31 项与相关包 go vet 重跑通过。
- 正式构建集成：包含嵌入式前端的最新二进制已在隔离 Go/Redis 环境通过权限重启持久化、指定库恢复 304 部影片、修改账号后旧会话失效、旧凭据失败、新凭据成功及退出令牌撤销；验证临时环境已清理。
- 浏览器：临时 Go/Redis/307 源文件，真实登录、权限清空与刷新保留、指定库恢复 304 部可见影片、退出后旧令牌 401；1440px 桌面与 390px 手机截图检查，无横向溢出。未使用用户真实媒体目录。
- 边界：CGO 关闭，未跑 race；真实 Emby 客户端、Linux 与生产未验收。未提交、推送或部署。已有上游直拉 URL 无法撤回；字幕/HLS 交付凭据与管理员维护权限见功能说明。
- 对 `codestable/` 的影响：新增本 ff；仓库无相关 project spec，README 与功能说明已同步，既有事项状态不变。

2026-10-21 交付补记：用户授权提交账号功能，本记录对应的账号与媒体库权限改动随 `029-x-ff-multi-user-management.md` 中的多用户功能一并单独提交、推送。保留上面的单账号阶段历史及本事项状态；当前多用户范围、提交快照复验与未验边界见 029 和 `docs/ACCOUNT-ACCESS.md`。未部署。
