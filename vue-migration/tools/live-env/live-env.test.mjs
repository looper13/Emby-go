import assert from 'node:assert/strict';
import test from 'node:test';
import path from 'node:path';
import fs from 'node:fs/promises';
import { startMediaServer } from './media-server.mjs';
import { makeRedactor, repoRoot, startLiveEnvironment } from './environment.mjs';
import { canonicalRequest, countRequests, parseRequestLog } from './request-counter.mjs';

test('counter parses actual Gin format and canonicalizes query ordering', () => {
  const requests = parseRequestLog('[GIN] 2026/10/10 - 10:42:04 | 200 | 15.9646ms | 127.0.0.1 | GET     "/api/admin/items?offset=0&limit=100"\n[GIN] 2026/10/10 - 10:42:04 | 200 | 0s | ::1 | GET "/api/admin/items?limit=100&offset=0"');
  assert.equal(requests.length, 2);
  assert.deepEqual(countRequests(requests), { 'GET /api/admin/items?limit=100&offset=0': 2 });
  assert.equal(canonicalRequest('GET', '/api/admin/items?offset=100&limit=100'), 'GET /api/admin/items?limit=100&offset=100');
  assert.deepEqual(parseRequestLog('not a log line'), []);
});

test('redactor covers secrets and native, JSON and URL path variants', () => {
  const redact = makeRedactor('C:\\Temp\\fixture', ['one-time-token', 'one-time-password']);
  for (const value of ['C:\\Temp\\fixture/media', 'C:/Temp/fixture/media', JSON.stringify({ path: 'C:\\Temp\\fixture\\media' })]) assert(!redact(value).includes('Temp'));
  assert.equal(redact('/cygdrive/c/Temp/fixture/media'), 'TEMP_LIVE/media');
  assert.equal(redact('0123456789abcdef0123456789abcdef0123456789abcdef'), 'TOKEN');
  assert.equal(redact('one-time-token one-time-password /img?api_key=secret&tag=1'), 'REDACTED REDACTED /img?api_key=TOKEN&tag=1');
});

test('media supports HEAD, bounded/open/suffix ranges, errors and redirect', async () => {
  const media = await startMediaServer(path.join(repoRoot, 'internal/server/testdata/probe-sample.mp4'));
  const base = `http://127.0.0.1:${media.port}`;
  try {
    const head = await fetch(`${base}/sample.mp4`, { method: 'HEAD' });
    const size = Number(head.headers.get('content-length'));
    assert(size > 32);
    assert.equal((await head.arrayBuffer()).byteLength, 0);
    for (const [range, status, length] of [['bytes=0-9', 206, 10], ['bytes=-5', 206, 5], [`bytes=${size - 4}-`, 206, 4], ['bytes=-0', 416, 0], ['bytes=', 416, 0], ['bytes=10-1', 416, 0], ['bytes=1-2,4-5', 416, 0]]) {
      const response = await fetch(`${base}/sample.mp4`, { headers: { Range: range } });
      assert.equal(response.status, status, range);
      assert.equal((await response.arrayBuffer()).byteLength, length);
    }
    assert.equal((await fetch(`${base}/redirect.mp4`, { redirect: 'manual' })).status, 302);
    assert.equal((await fetch(`${base}/failure.mp4`)).status, 503);
    assert.equal((await fetch(`${base}/../config.yaml`)).status, 404);
  } finally { await media.close(); }
});

test('startup fault after root removes only its newly created directory', async () => {
  let removed;
  await assert.rejects(startLiveEnvironment({ faultAfter: 'root' }), error => {
    assert.equal(error.message, 'Injected startup failure after root');
    assert.equal(error.cleanup.rootRemoved, true);
    removed = error.cleanup.root;
    return true;
  });
  await assert.rejects(fs.stat(removed), { code: 'ENOENT' });
});
