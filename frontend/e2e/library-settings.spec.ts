import { createServer } from 'node:http';
import { mkdir, readFile, stat, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { test, expect } from './fixtures';

test('多目录媒体库可编辑请求头并通过代理拖动播放', async ({ page, request, live }, info) => {
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  const fixtureRoot = path.join(live.root, `library-settings-${info.project.name}`);
  const first = path.join(fixtureRoot, 'first'), second = path.join(fixtureRoot, 'second');
  await mkdir(first, { recursive: true });
  await mkdir(second);
  const video = await readFile(fileURLToPath(new URL('../../internal/server/testdata/probe-sample.mp4', import.meta.url)));
  let expectedReferer = 'https://headers.test/watch/';
  const expectedOrigin = 'https://headers.test';
  const expectedUserAgent = 'LibraryPlaybackFixture/1.0';
  const upstreamRequests: { referer: string | undefined; origin: string | undefined; range: string | undefined }[] = [];
  const source = createServer((req, res) => {
    upstreamRequests.push({ referer: req.headers.referer, origin: req.headers.origin, range: req.headers.range });
    if (req.headers.referer !== expectedReferer || req.headers.origin !== expectedOrigin || req.headers['user-agent'] !== expectedUserAgent) {
      res.writeHead(403); res.end('请求头不匹配'); return;
    }
    const range = /^bytes=(\d+)-(\d*)$/.exec(req.headers.range ?? '');
    const start = range ? Number(range[1]) : 0;
    const end = Math.min(range?.[2] ? Number(range[2]) : video.length - 1, video.length - 1);
    if (start > end || start >= video.length) { res.writeHead(416); res.end(); return; }
    const chunk = video.subarray(start, end + 1);
    res.writeHead(range ? 206 : 200, { 'Content-Type': 'video/mp4', 'Accept-Ranges': 'bytes', 'Content-Length': chunk.length,
      ...(range ? { 'Content-Range': `bytes ${start}-${end}/${video.length}` } : {}) });
    res.end(req.method === 'HEAD' ? undefined : chunk);
  });
  await new Promise<void>(resolve => source.listen(0, '127.0.0.1', resolve));
  const upstream = `http://127.0.0.1:${(source.address() as { port: number }).port}/media.mp4`;
  try {
    for (const [index, directory] of [first, second].entries()) {
      await writeFile(path.join(directory, 'film.strm'), `${upstream}\n`);
      await writeFile(path.join(directory, 'film.nfo'), `<movie><title>多目录测试影片 ${index + 1}</title></movie>`);
    }
    await page.goto(`${live.baseURL}/admin#/libraries`);
    await page.locator('#auth-user').fill(live.credentials.username);
    await page.locator('#auth-pw').fill(live.credentials.password);
    await page.locator('#auth-form button[type=submit]').click();
    await expect(page.locator('#library-form')).toBeVisible();
    const form = page.locator('#library-form');
    await form.locator('#lib-name').fill('多目录播放测试');
    await form.locator('#lib-path').fill(first);
    await form.locator('[data-add-path]').click();
    await form.locator('#lib-path-2').fill(second);
    await form.locator('#lib-proxy').check();
    await form.locator('#lib-resolve').check();
    await form.locator('#lib-referer').fill(expectedReferer);
    await form.locator('#lib-origin').fill(expectedOrigin);
    await form.locator('[data-add-header]').click();
    await form.locator('[data-header-name]').fill('User-Agent');
    await form.locator('[data-header-value]').fill(expectedUserAgent);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.screenshot({ path: info.outputPath('library-form-multiple-paths.png'), fullPage: true, animations: 'disabled',
      mask: [page.locator('input, textarea, .lib-path, .lib-path-text, [data-secret], [data-private-path]')], maskColor: '#262b31' });
    const createdResponse = page.waitForResponse(response => new URL(response.url()).pathname === '/api/admin/libraries' && response.request().method() === 'POST');
    await form.locator('button.btn-accent').click();
    const created = await createdResponse;
    expect(created.status()).toBe(200);
    const library = await created.json();
    expect(library.Paths).toEqual([first, second]);
    expect(library.ProxyHeaders).toEqual({ Referer: expectedReferer, Origin: expectedOrigin, 'User-Agent': expectedUserAgent });
    const card = page.locator('article.library-card').filter({ hasText: '多目录播放测试' });
    await expect(card).toBeVisible();
    await expect(card).toContainText(first);
    await expect(card).toContainText(second);
    await live.request('/api/admin/scan', { method: 'POST', body: { library_id: library.Id } });
    const indexed = await live.request(`/api/admin/items?library_id=${library.Id}`);
    expect(indexed.items).toHaveLength(2);
    const movie = indexed.items.find((item: { source_path: string }) => item.source_path === path.join(second, 'film.strm'));
    expect(movie).toBeTruthy();
    const auth = await live.request('/Users/AuthenticateByName', { method: 'POST', auth: false, body: { Username: live.credentials.username, Pw: live.credentials.password } });
    live.addSecret(auth.AccessToken);
    const rangeHeaders = { 'X-Emby-Token': auth.AccessToken, Range: 'bytes=2-15' };
    const streamed = await request.get(`${live.baseURL}/Videos/${movie.id}/stream`, { headers: rangeHeaders });
    expect(streamed.status()).toBe(206);
    expect(streamed.headers()['content-range']).toBe(`bytes 2-15/${video.length}`);
    expect(await streamed.body()).toEqual(video.subarray(2, 16));
    expect(upstreamRequests.at(-1)).toEqual({ referer: expectedReferer, origin: expectedOrigin, range: 'bytes=2-15' });

    await card.locator(`[data-lib-edit="${library.Id}"]`).click();
    const edit = page.locator('#library-edit-form');
    await expect(edit.locator('#edit-lib-path-2')).toHaveValue(second);
    await expect(edit.locator('#edit-lib-referer')).toHaveValue(expectedReferer);
    expectedReferer = 'https://headers.test/updated/';
    await edit.locator('#edit-lib-referer').fill(expectedReferer);
    await edit.locator('button.btn-accent').click();
    await expect(edit).toHaveCount(0);
    const updatedStream = await request.get(`${live.baseURL}/Videos/${movie.id}/stream`, { headers: rangeHeaders });
    expect(updatedStream.status()).toBe(206);
    expect(upstreamRequests.at(-1)?.referer).toBe(expectedReferer);

    await page.getByRole('button', { name: '列表视图', exact: true }).click();
    const row = page.locator('tbody tr').filter({ hasText: '多目录播放测试' });
    await expect(row).toContainText(first);
    await expect(row).toContainText(second);
    await row.locator(`[data-lib-edit="${library.Id}"]`).click();
    await expect(edit.locator('#edit-lib-referer')).toHaveValue(expectedReferer);
    await edit.getByRole('button', { name: '移除目录 2', exact: true }).click();
    page.once('dialog', async dialog => {
      expect(dialog.message()).toContain('影片索引及其播放进度/收藏');
      expect(dialog.message()).toContain('不会删除磁盘文件');
      await dialog.accept();
    });
    await edit.locator('button.btn-accent').click();
    await expect(edit).toHaveCount(0);
    const remaining = await live.request(`/api/admin/items?library_id=${library.Id}`);
    expect(remaining.items).toHaveLength(1);
    expect(remaining.items[0].source_path).toBe(path.join(first, 'film.strm'));
    expect((await stat(path.join(second, 'film.strm'))).isFile()).toBe(true);
    expect((await request.get(`${live.baseURL}/Videos/${movie.id}/stream`, { headers: rangeHeaders })).status()).toBe(404);
    await page.screenshot({ path: info.outputPath('library-multiple-paths.png'), fullPage: true, animations: 'disabled',
      mask: [page.locator('input, textarea, .lib-path, .lib-path-text, [data-secret], [data-private-path]')], maskColor: '#262b31' });
    expect(errors).toEqual([]);
  } finally {
    source.closeAllConnections();
    await new Promise<void>(resolve => source.close(() => resolve()));
  }
});
