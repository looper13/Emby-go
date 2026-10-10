# 基线环境与命令结果（P0/VUE-01）

固化日期：2026-10-10。代码基线：`main` 分支 HEAD `5c45789`（本文件与 P0 其他产物均为未跟踪新文件；`README.md` 的本地修改发生在本次盘点之前，非迁移工具所改）。
本机：Windows 11 Pro 10.0.26200，amd64。

> 2026-10-10 P1 安全补充：下表保留 P0 历史执行记录，不代表当前可安全复跑。既有 `TestRedisBehavior` 使用本机 6379 DB15 并清理数据，本机禁止直接运行无排除项的全包 `go test`。后续安全命令见 §4，当前环境与验证结果见 [P1/progress.md](../P1/progress.md)。

## 1. 环境清单

| 组件 | 实测 | 备注 |
|---|---|---|
| Node | **v24.14.1** | ⚠ 计划 §3.1 建议 22.x（≥22.12）；本机为 24.x。P1 固定版本时二选一：统一到 24.x（写入 engines + CI）或安装 22.x；选定后本地与 CI 用同一版本 |
| npm | 11.11.0 | |
| Go | **1.26.4 windows/amd64** | release workflow 固定 1.25.x —— 交叉编译产物一致性在 P7 验证 |
| ffprobe | 8.1.2-full_build（gyan.dev） | 在 PATH 中；探测功能依赖它 |
| Redis | 本机 6379 有实例，`redis-cli ping` → PONG | Scoop 安装；`redis-server` 可执行文件在 PATH（`/d/Develop/Scoop/shims/redis-server`），P1-E 可用它起**专用测试实例**（自定义端口 + 临时 dir） |
| Docker | 29.4.0 | 备用（临时 Redis/媒体源容器化，如需） |
| Playwright | 未安装 | P1-E 明确准备（浏览器下载） |

## 2. 基线命令与结果（命令原文）

| # | 命令 | 结果 | 耗时 |
|---|---|---|---|
| 1 | `node --test internal/server/web_test/app.test.cjs` | ✅ **17/17 通过**，fail 0 | 112ms |
| 2 | `go vet ./...` | ✅ 干净（无输出） | 1.5s |
| 3 | `go build -o "$TEMP/emby-go-baseline.exe" ./cmd/metatube` | ✅ 成功，26,896,896 B（≈25.7 MB）；仓库内无残留产物 | — |
| 4 | `go test ./...` | ⚠ 15 包 ok，**internal/server FAIL**（2 个既有失败，见 §3） | 72–77s |

`go test ./...` 通过包：avatar、cache、config、imageutil、librarywatch、logging、metatube、nfo、probe、scanner、scheduler、scraper、store、translate、server 之外的其余包均 ok。

README 记录的命令（`go build -o metatube ./cmd/metatube`、`go vet ./...`、`go test ./...`、`node --test …`）已全部按原文执行；`bash build.sh`（Linux amd64 交叉编译）不在 P0 基线内，P7 验证。

## 3. 既有失败清单（迁移前已存在，P6 不得当迁移回归）

### F1. `TestMediaProbe` —— 稳定复现（3/3 次全量运行失败）

- 位置：`internal/server/probe_test.go:135`（断言集合含 `<reframes>1</reframes>`）。
- 失败输出：`NFO 缺少 "<reframes>1</reframes>"`；实际 NFO 的 `<streamdetails><video>` 有 codec/profile/level/pixelformat/bitdepth/color\*/bitrate/width/height…，**没有 reframes**。
- 诊断（已核实）：本机 ffprobe 8.1.2 对测试样本 `internal/server/testdata/probe-sample.mp4`（h264，5 帧）不输出 `refs` 流字段：
  `ffprobe -v error -select_streams v:0 -show_entries stream=codec_name,refs,nb_frames -of json …` → 仅返回 `codec_name=h264, nb_frames=5`。`<reframes>` 由 `RefFrames` 映射（probe.go streamDetailsFromProbe），缺失属 **ffprobe 版本行为差异**。
- 影响：与本次前端迁移无关（媒体探测后端功能）。复现：`go test ./internal/server/ -run TestMediaProbe -count=1`。
- 处置建议（不在迁移内）：条件化断言或换样本；另开任务。

### F2. `TestLibraryWatchEndToEnd` —— 间歇性（3 次全量运行 1 次失败）

- 位置：`internal/server/library_watch_test.go`（14.19s 用例）。
- 表现：全量 `go test ./...` 并行负载下偶发 FAIL（首次记录 08:59–09:00）；**单独运行 `-run TestLibraryWatchEndToEnd -count=2` 2/2 通过**。
- 归因：时序敏感（文件监视/轮询 + 高负载），Windows 下刷新延迟抖动。
- 影响：迁移无关；P6 若复现，先与全量负载对比再定性。

## 4. 每次阶段验证复用的基线命令组

```powershell
node --test internal/server/web_test/app.test.cjs     # 期望 17/17
go vet ./...                                          # 期望干净
npm --prefix frontend run test:go-regression            # 明确排除危险 Redis 用例并保留失败报告
npm --prefix frontend run test:go-build                 # 临时目录内验证默认/真实嵌入构建并清理
```

禁止在本机原样复跑计划 §9.3 或上方历史表中的 `go test ./...` / `go test -tags embedui ./...`。排除 `TestRedisBehavior` 不是通过该用例；P1 记录明确列出排除项。它若需要验证，必须先有经过审核的独立 Redis 测试改造。

对照规则：F1 稳定存在属环境已知；F2 偶发需记录出现频率；**除 F1/F2 外任何新失败都按迁移回归候选处理，定位后再定性**。P1 又观察到 NFO/目录监视的 Windows rename 与事件时序失败，未当作通过，也未改后端断言；详见阶段报告。

## 5. 本机不可验证项（明确列出，不得写成通过）

- Linux systemd 部署冒烟与版本切换/回滚演练（P7，需要 Linux 环境）。
- 五个平台交叉编译中 darwin/linux 产物的实际运行（可交叉编译，无法本机运行）。
- 真实手机浏览器（自动播放/全屏/Range）与 Chrome/Edge 真机播放（P6）。
- P0 固化时尚未安装 Playwright；P1-E 已补齐 Edge 移动视口/桌面测试，范围见 P1 报告，不等于真机或全场景通过。

## 6. 供 P1-E 使用的环境要点

- 服务启动：`./emby-go-baseline.exe -c <temp>/config.yaml`（配置键见 README §配置：port/db_path/server_name/server_id/redis_addr/redis_password/redis_db/disable_library_monitor/library_monitor_mode/ffprobe_path/…）。
- `server.New` **强制 Redis**：redis_addr 为空或连不上直接拒绝启动（server.go:138-146）。P1-E 已实现动态端口、随机密码和独立临时目录的 Redis 配方；使用 [live-env-notes.md](live-env-notes.md) 命令，不复用旧测试固定端口。
- 绝对不要读取/修改仓库或用户私有 `config.yaml`；临时环境全部用独立 temp 目录 + 一次性凭据。
- 媒体目录结构约定：每个条目一个文件夹（`ABF-018/ABF-018.strm` + 同名 `.nfo`），.strm 内容为一行 http(s) URL（README §目录约定）。
