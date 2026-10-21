---
kind: issue
title: 多用户管理与普通用户浏览
type: ff
status: closed
created: 2026-10-21
---

# 多用户管理与普通用户浏览

- 授权：用户要求对照 Emby 检查现有多用户后，确认修改。本次在已有多用户后端工作区上补齐管理 UI、普通用户路径与权限边界；不提交、推送或部署。028 保留此前单账号范围的历史记录。
- 实现：用户列表、创建普通用户、重置密码、启用/禁用、删除、按库授权；账号与授权分开保存，失败保留草稿、未保存离开确认。原管理员不可禁用/删除；普通用户默认无库授权，只保留浏览与自身账号入口。
- 修复：旧授权重复迁移、字幕/HLS 错用管理员授权、跨用户媒体会话取消、前端固定 UserId=1、普通用户读取管理库列表、动漫季/集转发前提前缓存查询参数。切换账号清理媒体墙/播放器/任务，阻止旧进度或旧账号修改结果影响新会话。
- 主要文件：`internal/store/users.go`，`internal/server/{auth,users,account,server,playback_tracks,audio_playback}.go`，Vue `UsersPage`/`AccountPage`、认证/路由/媒体与动漫视图，`api/{users,user-context,catalog,favorites,television}.ts`。新增 store/server 用户回归及前端用户/角色/播放器/延迟回调回归；旧动漫夹具补建真实账号。
- 界面布局：桌面为左侧用户列表、右侧账号与媒体库授权表单；手机为上方两列用户选择、下方纵向表单。编辑区不使用嵌套卡片，宽度不足时字段换行。

```text
用户管理                            刷新  新增
用户列表 | 用户名 / 禁用
         | 密码 / 确认密码 / 保存账号
         | 全部库 / 指定库 / 保存权限
         | 删除用户
```

- 验证：`go test ./... -count=1 -skip '^Test(RedisBehavior|MediaProbe|ProbeCircuitBreaker)$' -timeout 240s` 通过；危险 Redis 测试和已知异常探测测试没有执行。新增用户/账号/动漫收藏定向回归另行执行。`go vet ./internal/store ./internal/server` 通过。
- 前端：全套 268/269 通过，原有 CSS 字节冻结差异保留；类型检查与正式构建通过，保留现有 HLS 大块提示。没有修改全局冻结 CSS。
- 浏览器：隔离 Go/Redis/307 源文件环境，创建用户默认零可见；授权后 304 条可见，测试视频达到 ended/readyState=4，收藏与播放状态只改变普通用户。维护路由重定向，禁用后旧登录失效，启用后重新登录成功。1440x1000 和 390x844 用户管理截图及溢出检查通过，证据位于 `output/playwright/multiuser-*.png`。
- 未验项：真实 Emby 客户端、Linux、生产、长时间多设备播放与 race。高级家长控制、设备/远程访问策略及新增管理员不在本次范围；资源交付与独立 API 密钥边界已写入 `ACCOUNT-ACCESS.md`。
- 制度记忆影响：新增本记录；同步 README 与 `ACCOUNT-ACCESS.md` 当前实现说明，不关闭或改写 028。

## 2026-10-21 交付补记

- 用户授权提交账号功能，账号设置、媒体库权限与多用户管理单独提交、推送；此前详情页选轨与外部播放器保持独立提交。临时凭据、测试日志、截图及构建产物不进入 Git。
- 从暂存区导出独立快照复验：前端 27 文件 280 项全部通过，类型检查与正式构建通过；原工作区 CSS 换行差异不在提交内。
- `go test ./... -count=1 -skip '^Test(RedisBehavior|MediaProbe|ProbeCircuitBreaker)$' -timeout 240s` 全部包通过，server 包 136.188 秒；三项排除原因沿用上文。`go vet ./internal/store ./internal/server`、Windows 普通与 `embedui` 构建通过。
- 本轮未重新执行浏览器、真实 Emby 客户端、Linux、race 或生产验收；未部署。029 保持 closed，028 仅补记交付，状态不变。
