export function canonicalRequest(method, target) {
  const url = new URL(target, 'http://127.0.0.1');
  url.searchParams.sort();
  return `${method} ${url.pathname}${url.search}`;
}

export function parseRequestLog(log) {
  const requests = [];
  for (const line of log.replace(/\x1b\[[0-9;]*m/g, '').split(/\r?\n/)) {
    const match = /^\[GIN\]\s+\S+\s+-\s+\S+\s+\|\s*(\d+)\s*\|[^|]*\|[^|]*\|\s*(\w+)\s+"([^"]+)"/.exec(line);
    if (match) requests.push({ status: Number(match[1]), method: match[2], target: match[3], key: canonicalRequest(match[2], match[3]) });
  }
  return requests;
}

export const isBusiness = target => /^\/(api\/|Users\/|Items\/\d+\/Similar)/.test(new URL(target, 'http://127.0.0.1').pathname);
export const isPagination = request => {
  const url = new URL(request.target, 'http://127.0.0.1');
  return url.pathname === '/api/admin/items' && url.searchParams.get('limit') === '100' && url.searchParams.has('offset');
};
export function countRequests(requests) {
  const result = {};
  for (const request of requests) {
    const key = canonicalRequest(request.method, request.target);
    result[key] = (result[key] || 0) + 1;
  }
  return Object.fromEntries(Object.entries(result).sort(([a], [b]) => a.localeCompare(b)));
}
