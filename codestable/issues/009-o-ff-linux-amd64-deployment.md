---
kind: issue
title: Linux amd64 编译脚本与 systemd 部署模板
type: ff
status: open
created: 2026-10-09
---

- 内容：按用户确认的 Linux amd64 环境新增 build.sh，固定 CGO_ENABLED=0、GOAMD64=v1，注入版本、提交和构建时间；先编译临时文件，成功后替换产物。新增独立 emby-go 用户运行的 systemd 服务，固定配置及数据目录，异常退出自动重启。
- 改动：build.sh、deploy/emby-go.service、deploy/README.md、.gitattributes、README.md。部署说明覆盖 Redis、ffprobe、配置回写权限、媒体目录权限、挂载、日志及停服备份升级；未修改应用启动逻辑。
- 验证：文件已写入；本机 Bash 启动受 Windows 权限限制，未完成脚本执行或 systemd 验证。用户明确要求只写脚本，由其自行在服务器测试，已停止本地测试。
- codestable：无 spec/epic 需同步；保留 open，等待服务器验证。未执行服务器安装、服务管理、Git 提交或推送。
