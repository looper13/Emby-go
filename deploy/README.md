# Linux amd64 部署

适用于使用 systemd 的 Linux x86-64 主机。以下命令在项目根目录执行，编译不需要 root。

## 1. 准备依赖

- 构建机器：Go 1.25 或更新版本、Bash。运行机器不需要 Go，也不需要 Node/npm。
- 运行机器：可连接的 Redis，连接失败时程序会退出，systemd 每 5 秒重试。
- 使用媒体信息探测时需安装 FFmpeg 中的 `ffprobe`；HTTPS 媒体源需要系统 CA 证书。

Debian/Ubuntu 使用本机 Redis 的示例（其他发行版按其包管理器安装；使用远程 Redis 时不用安装本机 Redis）：

```bash
sudo apt update
sudo apt install redis-server ffmpeg ca-certificates
sudo systemctl enable --now redis-server
redis-cli ping
```

应返回 `PONG`。发行版提供的 Go 可能低于 1.25，编译前用 `go version` 确认版本。

## 2. 编译

```bash
bash build.sh
# 可选：指定发布版本和输出目录
VERSION=1.0.0 OUT_DIR=dist bash build.sh
```

固定生成 `dist/emby-go-linux-amd64`，包含版本、Git 提交及 UTC 构建时间。未指定版本时使用 Git 描述，无 Git 信息时为 `dev`。关闭 CGO，使用 amd64 v1 指令基线，不进行 UPX 压缩。脚本也可在其他架构主机或 Windows Git Bash 中交叉编译；失败时保留上一次成功产物。

交叉编译后把二进制、`config.example.yaml` 和 `deploy/emby-go.service` 传到 Linux 主机，安装时保持下列命令使用的相对路径。

## 3. 首次安装

```bash
getent group emby-go >/dev/null || sudo groupadd --system emby-go
id -u emby-go >/dev/null 2>&1 || sudo useradd --system --gid emby-go \
  --home-dir /var/lib/emby-go --no-create-home --shell /usr/sbin/nologin emby-go

sudo install -o root -g root -m 0755 dist/emby-go-linux-amd64 /usr/local/bin/emby-go
sudo install -d -o root -g emby-go -m 0750 /etc/emby-go
sudo install -d -o emby-go -g emby-go -m 0750 /var/lib/emby-go

# 保留已存在的私人配置，避免覆盖 Redis 密码和 server_id。
if ! sudo test -e /etc/emby-go/config.yaml; then
  sudo install -o emby-go -g emby-go -m 0640 config.example.yaml /etc/emby-go/config.yaml
fi
sudoedit /etc/emby-go/config.yaml
```

核对配置中的 `port`、`redis_addr`、`redis_password`、`redis_db`，推荐将 `db_path` 设为 `/var/lib/emby-go/emby-go.db`。保留示例的相对路径 `emby-go.db` 也会写入同一目录。配置文件需要允许 `emby-go` 用户读写，因为程序首次启动会回写 `server_id`；已有配置迁移时也需检查其所有者和权限。

服务端默认端口为 **18080**。服务以非 root 用户运行，监听 80/443 时请使用反向代理，不要改成 root 运行。首次管理员初始化前，不要将服务端口直接暴露到公网。

`ffprobe_path` 留空时从 `/usr/local/bin:/usr/bin:/bin` 查找；安装在其他位置时填写绝对路径。SMB/NFS 等远程挂载建议设置 `library_monitor_mode: polling`，媒体库路径使用 Linux 挂载路径，不能直接沿用 Windows 盘符。

## 4. 安装并启动服务

```bash
sudo install -o root -g root -m 0644 deploy/emby-go.service /etc/systemd/system/emby-go.service
sudo systemd-analyze verify /etc/systemd/system/emby-go.service
sudo systemctl daemon-reload
sudo systemctl enable --now emby-go
sudo systemctl status emby-go --no-pager
```

浏览器访问 `http://服务器IP:18080` 完成管理员初始化。按需要开放防火墙或配置反向代理。

| 内容 | 路径 |
| --- | --- |
| 程序 | `/usr/local/bin/emby-go` |
| 配置 | `/etc/emby-go/config.yaml` |
| 工作目录、默认 SQLite 数据 | `/var/lib/emby-go` |
| 程序和请求文件日志 | `/var/lib/emby-go/log/` |
| systemd 日志 | `journalctl -u emby-go` |

文件日志按天生成，但不会自动清理，需自行安排保留策略。

## 5. 媒体目录权限与挂载

`emby-go` 用户需要媒体目录及父目录的读取、遍历权限。刮削、探测、手动补录还需要媒体目录写权限，才能写入 NFO、图片和 `mediainfo.json`。可将它加入现有媒体共享组并配置目录组权限；不要对整个媒体库执行递归 `chmod 777` 或无差别改变所有者。修改用户组后重启服务生效。

服务没有隐藏 `/home`，但使用了独立的 `/tmp`，且 `/usr`、`/boot`、`/etc` 默认只读（配置文件除外）。媒体库和自定义演员头像目录建议放在 `/srv`、`/mnt` 或其他专用数据目录，不要放在临时目录或这些受保护的系统目录中。

网络挂载应由系统管理（例如 `/etc/fstab`），不能只挂载在某个桌面登录会话中。需要在服务启动前等待挂载时，运行 `sudo systemctl edit emby-go`，按实际路径添加：

```ini
[Unit]
RequiresMountsFor=/mnt/media
```

本机 Redis 也可在上述 `[Unit]` 中添加 `Wants=redis-server.service` 和 `After=redis-server.service`；某些发行版名称为 `redis.service`。默认服务文件不绑定某个 Redis 单元名，兼容远程 Redis。

修改后执行 `sudo systemctl daemon-reload` 和 `sudo systemctl restart emby-go`。挂载路径若含空格，需要按 systemd 的路径转义规则填写。

## 6. 日常管理与升级

```bash
sudo systemctl restart emby-go
sudo systemctl stop emby-go
sudo systemctl start emby-go
sudo systemctl disable --now emby-go
sudo journalctl -u emby-go -n 100 --no-pager
sudo journalctl -u emby-go -f
/usr/local/bin/emby-go -version
```

上面是各自独立的管理命令，不需要依次执行。配置改动需要重启；服务文件改动还需 `daemon-reload`。连续重启时先检查 Redis 连通性、配置文件权限、数据目录权限和端口占用。

升级前等待扫描、刮削、探测任务结束。当前程序没有信号驱动的优雅停机，`TimeoutStopSec` 不表示应用会等待写入完成；停止服务也会终止它启动的 `ffprobe` 子进程。

```bash
bash build.sh
sudo systemctl stop emby-go

# 停服备份，包含配置、SQLite 及可能存在的 WAL/shm；备份目录仅 root 可访问。
sudo install -d -o root -g root -m 0700 /var/backups/emby-go
sudo tar -C / -czf "/var/backups/emby-go/$(date -u +%Y%m%dT%H%M%SZ).tar.gz" \
  etc/emby-go var/lib/emby-go

sudo cp -a /usr/local/bin/emby-go /usr/local/bin/emby-go.bak
sudo install -o root -g root -m 0755 dist/emby-go-linux-amd64 /usr/local/bin/emby-go
sudo systemctl start emby-go
sudo systemctl status emby-go --no-pager
```

升级只替换二进制，不覆盖私人配置、数据库或媒体文件。自定义 `db_path`、演员头像目录和媒体库不在默认备份范围时，应另外备份；发生数据库迁移后，回滚不能只替换旧二进制，还要恢复对应版本的数据备份。
