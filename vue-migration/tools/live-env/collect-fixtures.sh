#!/usr/bin/env bash
# collect-fixtures.sh — VUE-01 真实服务 JSON 夹具采集（阶段一初始化 + 阶段二 curl 采集）。
#
# 前提：
#   - 临时 Go 服务已在 $BASE 运行（临时 SQLite + 专用 Redis + 临时媒体目录，见 live-env-notes.md）
#   - 媒体数据集已由 make-fixtures.sh 生成并（服务端）加入媒体库根目录
#   - 环境变量：LIVE_USER / LIVE_PW（一次性凭据，不写入仓库）
#     BASE（默认 http://127.0.0.1:18099）、MEDIA_ROOT、OUT（夹具输出目录）
#
# 用法（仓库根目录）:
#   LIVE_USER=... LIVE_PW=... MEDIA_ROOT=<temp>/media OUT=vue-migration/fixtures/json \
#     bash vue-migration/tools/live-env/collect-fixtures.sh
#
# 说明：AccessToken 在写入 auth-login.json 前替换为 "TOKEN"；原始响应文件在退出时删除。
# 每个请求打印 "<path> -> <http code> [fixture]" 供人工核对。

set -euo pipefail

BASE="${BASE:-http://127.0.0.1:18099}"
: "${OUT:?set OUT to the fixtures output dir}"
: "${MEDIA_ROOT:?set MEDIA_ROOT to the temp media root}"
: "${LIVE_USER:?set LIVE_USER (one-off admin)}"
: "${LIVE_PW:?set LIVE_PW (one-off password)}"
TOOLS="$(cd "$(dirname "$0")" && pwd)"
RAW="$(mktemp)"

mkdir -p "$OUT/errors"
trap 'rm -f "$RAW" "$OUT"/.raw-*.tmp 2>/dev/null || true' EXIT

fmt() { node "$TOOLS/jsonfmt.mjs"; }         # stdin -> pretty stdout
fmt_mask() { node "$TOOLS/jsonfmt.mjs" --mask-token; }

TOKEN=""

# save <method> <path> <fixture> [curl args...] — 打印状态码并把美化后的 body 写入夹具
save() {
  local method="$1" epath="$2" fixture="$3"; shift 3
  local code
  code=$(curl -sS -X "$method" "$BASE$epath" ${TOKEN:+-H "X-Emby-Token: $TOKEN"} -o "$RAW" -w '%{http_code}' "$@")
  fmt < "$RAW" > "$OUT/$fixture"
  printf '%-6s %-58s -> %s  [%s]\n' "$method" "$epath" "$code" "$fixture"
}

pick_id() { # pick_id <items.json> <title substring>
  node -e 'const fs=require("fs");const d=JSON.parse(fs.readFileSync(process.argv[1],"utf8"));const items=d.items||[];const it=items.find(m=>String(m.Title||"").includes(process.argv[2]))||items[0];if(!it){console.error("pick_id: no item for "+process.argv[2]);process.exit(1)}process.stdout.write(String(it.id))' "$1" "$2"
}

echo "== 1. 初始化链路（auth status -> initialize -> login）=="
code=$(curl -sS "$BASE/api/auth/status" -o "$RAW" -w '%{http_code}')
fmt < "$RAW" > "$OUT/auth-status-before.json"
echo "GET    /api/auth/status (初始化前)                          -> $code  [auth-status-before.json]"

code=$(curl -sS -X POST "$BASE/api/auth/initialize" -H 'Content-Type: application/json' \
  --data "{\"Username\":\"$LIVE_USER\",\"Pw\":\"$LIVE_PW\"}" -o "$RAW" -w '%{http_code}')
echo "POST   /api/auth/initialize                                  -> $code  (204=新建; 409=已初始化，重跑脚本时正常)"

code=$(curl -sS "$BASE/api/auth/status" -o "$RAW" -w '%{http_code}')
fmt < "$RAW" > "$OUT/auth-status.json"
echo "GET    /api/auth/status (初始化后)                          -> $code  [auth-status.json]"

code=$(curl -sS -X POST "$BASE/Users/AuthenticateByName" -H 'Content-Type: application/json' \
  --data "{\"Username\":\"$LIVE_USER\",\"Pw\":\"$LIVE_PW\"}" -o "$RAW" -w '%{http_code}')
TOKEN=$(node -e 'const fs=require("fs");process.stdout.write(JSON.parse(fs.readFileSync(process.argv[1],"utf8")).AccessToken||"")' "$RAW")
[ -n "$TOKEN" ] || { echo "login failed, no AccessToken" >&2; exit 1; }
fmt_mask < "$RAW" > "$OUT/auth-login.json"
echo "POST   /Users/AuthenticateByName                             -> $code  [auth-login.json, AccessToken=TOKEN]"

echo "== 2. 媒体库（空则创建，采集 0/1 库形状）=="
save GET /api/admin/libraries libraries.json
total=$(node -e 'const fs=require("fs");console.log(JSON.parse(fs.readFileSync(process.argv[1],"utf8")).total)' "$OUT/libraries.json")
if [ "$total" -eq 0 ]; then
  save POST /api/admin/libraries libraries-created.json -H 'Content-Type: application/json' \
    --data "{\"Name\":\"Live Fixtures\",\"Path\":\"$MEDIA_ROOT\"}"
  save GET /api/admin/libraries libraries.json
fi

echo "== 3. 扫描（同步长请求；期间轮询一次进度）=="
rm -f "$OUT/scan-progress-running.json"   # 每次运行重新捕获扫描中样本
curl -sS -X POST "$BASE/api/admin/scan" -H "X-Emby-Token: $TOKEN" -o "$OUT/.raw-scan-result.tmp" -w '%{http_code}' > "$OUT/.raw-scan-code.tmp" &
SCAN_PID=$!
for _ in $(seq 1 60); do
  curl -sS "$BASE/api/admin/scan/progress" -H "X-Emby-Token: $TOKEN" -o "$OUT/.raw-progress.tmp" 2>/dev/null || true
  if [ ! -f "$OUT/scan-progress-running.json" ] && \
     node -e 'const fs=require("fs");const d=JSON.parse(fs.readFileSync(process.argv[1],"utf8"));process.exit(d.running?0:1)' "$OUT/.raw-progress.tmp" 2>/dev/null; then
    fmt < "$OUT/.raw-progress.tmp" > "$OUT/scan-progress-running.json"
    echo "       (捕获扫描中进度) scan-progress-running.json"
  fi
  kill -0 "$SCAN_PID" 2>/dev/null || break
  sleep 0.25
done
wait "$SCAN_PID" || true
SCAN_CODE=$(cat "$OUT/.raw-scan-code.tmp" 2>/dev/null || echo "?")
fmt < "$OUT/.raw-scan-result.tmp" > "$OUT/scan-result.json"
echo "POST   /api/admin/scan                                       -> $SCAN_CODE  [scan-result.json]"

save GET /api/admin/scan/progress scan-progress.json
save GET /api/admin/status status.json
save GET /api/admin/settings settings.json
save GET /api/admin/tasks tasks.json

echo "== 4. 媒体墙与详情 =="
save GET '/api/admin/items?limit=100&offset=0&sort=datecreated&order=desc' items-wall.json
save GET '/api/admin/items?limit=100&offset=0&status=pending' items-wall-filtered.json
save GET '/api/admin/items?limit=100&offset=0&search=%E6%B7%B1%E5%A4%9C' items-wall-search.json   # search=深夜
save GET '/api/admin/items?limit=10&search=ABF-005' .raw-items-005.tmp
save GET '/api/admin/items?limit=10&status=pending' .raw-items-pending.tmp

DETAIL_ID=$(pick_id "$OUT/.raw-items-005.tmp" "深夜的测试影片")
PENDING_ID=$(pick_id "$OUT/.raw-items-pending.tmp" "")
# 死链 / 本地源两条：用标题精确挑（search=ABF-306 / ABF-307）
save GET '/api/admin/items?limit=10&search=ABF-306' .raw-items-306.tmp
save GET '/api/admin/items?limit=10&search=ABF-307' .raw-items-307.tmp
DEAD_ID=$(pick_id "$OUT/.raw-items-306.tmp" "探测失败样例")
OK_ID=$(pick_id "$OUT/.raw-items-307.tmp" "本地可探测样例")
rm -f "$OUT"/.raw-items-*.tmp
echo "       ids: detail=$DETAIL_ID pending=$PENDING_ID dead=$DEAD_ID probe-ok=$OK_ID"

save GET "/api/admin/items/$DETAIL_ID/detail" item-detail.json
save GET "/api/admin/items/$PENDING_ID/detail" item-detail-pending.json
save GET "/Items/$DETAIL_ID/Similar?Limit=12" similar.json

echo "== 5. 探针：单条探测（失败 502 / 成功 200）=="
save POST "/api/admin/items/$DEAD_ID/probe" probe-media-item-error.json
save POST "/api/admin/items/$OK_ID/probe" probe-media-item.json

echo "== 6. 刮削（未配置 MetaTube 的 400 错误体）=="
save GET /api/admin/scrape/settings scrape-settings.json
save GET "/api/admin/items/$DETAIL_ID/scrape/preview" scrape-unconfigured-error.json
save GET /api/admin/scrape/progress scrape-progress.json

echo "== 7. 计划任务与校验 =="
save GET /api/admin/scheduled scheduled.json
save POST /api/admin/scheduled/validate scheduled-validate-ok.json -H 'Content-Type: application/json' --data '{"cron":"*/5 * * * *"}'
save POST /api/admin/scheduled/validate scheduled-validate-error.json -H 'Content-Type: application/json' --data '{"cron":"not-a-cron"}'

echo "== 8. 其他列表与错误体 =="
save GET /api/admin/apikeys apikeys.json
save GET /no-such-path errors/404.json                 # 未注册路径：404 + 计入探针表
code=$(curl -sS "$BASE/api/admin/status" -o "$RAW" -w '%{http_code}')   # 不带 token
fmt < "$RAW" > "$OUT/errors/401.json"
echo "GET    /api/admin/status (无 token)                         -> $code  [errors/401.json]"
save GET /api/admin/probe probe.json

echo "== 完成 =="
echo "夹具目录: $OUT"
node -e 'const fs=require("fs");const d=JSON.parse(fs.readFileSync(process.argv[1],"utf8"));console.log("status.json =",JSON.stringify(d))' "$OUT/status.json"
