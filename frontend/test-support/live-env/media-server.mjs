import fs from 'node:fs';
import http from 'node:http';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

export async function startMediaServer(samplePath) {
  const sample = fs.readFileSync(samplePath);
  const requests = [];
  const timers = new Set();
  const server = http.createServer((req, res) => {
    const url = new URL(req.url, 'http://127.0.0.1');
    if (!['GET', 'HEAD'].includes(req.method)) return res.writeHead(405).end();
    if (url.pathname === '/health') return res.end('ok');
    if (url.pathname === '/stats') {
      res.writeHead(200, { 'Content-Type': 'application/json' });
      return res.end(JSON.stringify(requests));
    }
    const entry = { method: req.method, path: url.pathname, range: req.headers.range || null, status: null };
    requests.push(entry);
    const reply = (status, headers = {}, body) => {
      entry.status = status;
      res.writeHead(status, { 'Access-Control-Allow-Origin': '*', 'Cache-Control': 'no-store', ...headers });
      res.end(req.method === 'HEAD' ? undefined : body);
    };
    if (url.pathname === '/redirect.mp4') return reply(302, { Location: '/sample.mp4' });
    if (url.pathname === '/failure.mp4') return reply(503, { 'Content-Type': 'text/plain' }, 'injected media failure');
    if (url.pathname === '/disconnect.mp4') { entry.status = 'disconnect'; return req.socket.destroy(); }
    if (!['/sample.mp4', '/delay.mp4'].includes(url.pathname)) return reply(404);
    const send = () => {
      if (res.destroyed) return;
      const headers = { 'Content-Type': 'video/mp4', 'Accept-Ranges': 'bytes' };
      if (!req.headers.range) return reply(200, { ...headers, 'Content-Length': sample.length }, sample);
      const match = /^bytes=(\d*)-(\d*)$/.exec(req.headers.range);
      let start = 0;
      let end = sample.length - 1;
      if (match?.[1]) { start = Number(match[1]); if (match[2]) end = Number(match[2]); }
      else if (match?.[2]) start = Math.max(0, sample.length - Number(match[2]));
      if (!match || (!match[1] && !match[2]) || !Number.isSafeInteger(start) || !Number.isSafeInteger(end) || start > end || start >= sample.length) {
        return reply(416, { 'Content-Range': `bytes */${sample.length}` });
      }
      end = Math.min(end, sample.length - 1);
      reply(206, { ...headers, 'Content-Range': `bytes ${start}-${end}/${sample.length}`, 'Content-Length': end - start + 1 }, sample.subarray(start, end + 1));
    };
    if (url.pathname === '/delay.mp4') {
      const value = Number(url.searchParams.get('ms') || 400);
      const delay = Number.isFinite(value) ? Math.min(5000, Math.max(0, value)) : 400;
      const timer = setTimeout(() => { timers.delete(timer); send(); }, delay);
      timers.add(timer);
      res.once('close', () => { clearTimeout(timer); timers.delete(timer); });
    } else send();
  });
  await new Promise((resolve, reject) => { server.once('error', reject); server.listen(0, '127.0.0.1', resolve); });
  return { port: server.address().port, requests, close: async () => {
    for (const timer of timers) clearTimeout(timer);
    server.closeAllConnections();
    await new Promise(resolve => server.close(resolve));
  } };
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const source = await startMediaServer(process.argv[2]);
  process.send?.({ port: source.port });
  console.log(`media listening on 127.0.0.1:${source.port}`);
  let closing = false;
  const close = async () => { if (!closing) { closing = true; await source.close(); if (process.connected) process.disconnect(); } };
  process.once('SIGINT', close);
  process.once('SIGTERM', close);
  process.once('disconnect', close);
}
