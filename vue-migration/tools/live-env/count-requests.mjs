// count-requests.mjs — 统计服务请求日志中一段行号区间的端点调用次数（VUE-01/P0）。
//
// 用法: node count-requests.mjs <request-log-file> <startLine> <endLine>
//   （行号来自 shots.mjs 打印的 SEQ_LOG_START / SEQ_LOG_END，区间为 [start, end)）
// 日志行样例:
//   [GIN] 2026/10/10 - 09:40:00 | 200 |      339.1µs |       127.0.0.1 | GET     "/api/admin/items?limit=100&offset=0"
import fs from 'node:fs';

const [, , file, startArg, endArg] = process.argv;
if (!file || startArg === undefined || endArg === undefined) {
  console.error('usage: count-requests.mjs <logfile> <startLine> <endLine>');
  process.exit(2);
}
const lines = fs.readFileSync(file, 'utf8').split('\n');
const start = Number(startArg);
const end = Number(endArg);
const slice = lines.slice(start, end);

const exact = new Map(); // "METHOD path?query" -> count
const byPath = new Map(); // "METHOD path" -> count
const statusByReq = new Map();
let parsed = 0;

const re = /^\S+ \S+ - [\d:]+ \|\s*(\d+)\s*\|[^|]*\|\s*[\d.]+\s*\|\s*(\w+)\s+"([^"]*)"/;
for (const line of slice) {
  const m = re.exec(line.trim());
  if (!m) continue;
  parsed++;
  const [, status, method, target] = m;
  exact.set(`${method} ${target}`, (exact.get(`${method} ${target}`) || 0) + 1);
  const p = target.split('?')[0];
  byPath.set(`${method} ${p}`, (byPath.get(`${method} ${p}`) || 0) + 1);
  statusByReq.set(`${method} ${target} -> ${status}`, (statusByReq.get(`${method} ${target} -> ${status}`) || 0) + 1);
}

const dump = (title, map) => {
  console.log(`\n== ${title} ==`);
  [...map.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0])).forEach(([k, v]) => console.log(`${String(v).padStart(4)}  ${k}`));
};
console.log(`lines [${start}, ${end})  total=${slice.length}  parsed=${parsed}`);
dump('按精确请求（含 query）', exact);
dump('按路径（忽略 query）', byPath);
dump('按请求+状态码', statusByReq);
