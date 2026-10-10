import path from 'node:path';
import { mkdir, readFile, rm, stat } from 'node:fs/promises';
import type { Page } from '@playwright/test';
import { test, expect } from './fixtures';
async function login(page: Page, live: { baseURL: string; credentials: { username: string; password: string } }) {
  await page.goto(`${live.baseURL}/admin#/overview`);
  await page.locator('#auth-user').fill(live.credentials.username); await page.locator('#auth-pw').fill(live.credentials.password);
  await page.locator('#auth-form button[type=submit]').click(); await expect(page.locator('#reindex')).toBeVisible();
}
async function navigate(page: Page, name: string) { await page.locator(`[data-page=${name}]`).click(); await expect(page).toHaveURL(new RegExp(`#/${name}$`)); }
test('management pages preserve layout, navigation, settings read-only and history refresh', async ({ page, live }, info) => {
  const errors: string[] = []; page.on('pageerror', e => errors.push(e.message));
  await login(page, live);
  for (const name of ['overview', 'libraries', 'manual', 'settings', 'apikeys', 'tasks', 'probe']) {
    await navigate(page, name); await expect(page.locator('.panel').first()).toHaveCSS('opacity', '1');
    await expect(page.locator(`#nav [data-page=${name}]`)).toHaveAttribute('aria-current', 'page');
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    if (name === 'settings') { await expect(page.locator('#content')).toContainText('Redis 在线'); expect(await page.locator('#content input, #content form').count()).toBe(0); }
    if (name === 'tasks') { await expect(page.locator('#content')).toContainText('增量扫描媒体库'); await page.locator('#task-refresh').click(); await expect(page.locator('#content')).toContainText('成功'); }
    await page.screenshot({ path: info.outputPath(`vue-${name}.png`), fullPage: true, animations: 'disabled',
      mask: [page.locator('input, textarea, [data-private-path], [data-secret], .lib-path')], maskColor: '#262b31' });
  }
  expect(errors).toEqual([]);
});
test('library create survives navigation; cancellation and deletion only remove the index', async ({ page, live }) => {
  const directory = path.join(live.root, `extra-${test.info().project.name}`); await mkdir(directory);
  await login(page, live); await navigate(page, 'libraries');
  let release!: () => void; let started!: () => void; const submitted = new Promise<void>(r => { started = r; });
  await page.route('**/api/admin/libraries', async route => {
    if (route.request().method() !== 'POST') return route.continue();
    started(); await new Promise<void>(r => { release = r; }); await route.continue();
  });
  await page.locator('#lib-name').fill('P3 新媒体库'); await page.locator('#lib-path').fill(directory);
  await page.locator('#library-form button').click(); await submitted; await navigate(page, 'settings');
  release(); await expect(page.locator('#toasts')).toContainText('已添加媒体库'); await page.unroute('**/api/admin/libraries');
  await navigate(page, 'libraries'); const row = page.locator('tr').filter({ hasText: 'P3 新媒体库' }); await expect(row).toBeVisible();
  page.once('dialog', dialog => dialog.dismiss()); await row.locator('[data-lib-delete]').click(); await expect(row).toBeVisible();
  page.once('dialog', async dialog => { expect(dialog.message()).toBe('删除该媒体库及其影片索引？不会删除磁盘文件，但该库影片的播放进度/收藏会一并清除。'); await dialog.accept(); });
  await row.locator('[data-lib-delete]').click(); await expect(row).toHaveCount(0); expect((await stat(directory)).isDirectory()).toBe(true);
});
test('API key create, clipboard failure, client access and revocation use real endpoints', async ({ page, request, live }) => {
  await login(page, live); await navigate(page, 'apikeys');
  await page.locator('#key-name').fill('P3 测试客户端');
  const created = page.waitForResponse(r => r.url().endsWith('/api/admin/apikeys') && r.request().method() === 'POST');
  await page.locator('#apikey-form button').click(); const response = await created; const key = (await response.json()).key; live.addSecret(key);
  const row = page.locator('tr').filter({ hasText: 'P3 测试客户端' }); await expect(row).toBeVisible();
  const access = await request.get(`${live.baseURL}/Users/Me`, { headers: { 'X-Emby-Token': key } }); expect(access.status()).toBe(200);
  await page.evaluate(() => Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: () => Promise.reject(new Error('denied')) } }));
  await row.locator('[data-copy]').click(); await expect(page.locator('#toasts')).toContainText('复制失败，请手动选择');
  page.once('dialog', dialog => dialog.dismiss()); await row.locator('[data-key-delete]').click(); await expect(row).toBeVisible();
  page.once('dialog', async dialog => { expect(dialog.message()).toBe('删除该 API 密钥？使用它的客户端将立即失效。'); await dialog.accept(); });
  await row.locator('[data-key-delete]').click(); await expect(row).toHaveCount(0);
  expect((await request.get(`${live.baseURL}/Users/Me`, { headers: { 'X-Emby-Token': key } })).status()).toBe(401);
});
test('manual failure preserves draft; success writes strm/NFO and enters the media wall', async ({ page, live }) => {
  await login(page, live); await navigate(page, 'manual');
  const directory = path.join(live.root, `p3-manual-${test.info().project.name}`); await mkdir(directory);
  const source = path.join(directory, 'P3-001.strm'); const missing = path.join(directory, 'missing', 'P3-001.strm');
  try {
  await page.locator('#m-title').fill('P3 手动补录影片'); await page.locator('#m-path').fill(missing);
  await page.locator('#m-url').fill(`${live.mediaURL}/sample.mp4`); await page.locator('#m-year').fill('2024');
  await page.locator('#m-genres').fill('剧情，测试'); await page.locator('#m-tags').fill('P3, test'); await page.locator('#m-studios').fill('Studio');
  const failed = page.waitForResponse(r => r.url().endsWith('/api/admin/items/manual') && r.status() === 400);
  await page.locator('#manual-submit').click(); await failed;
  await expect(page.locator('#manual-submit')).toBeEnabled(); await expect(page.locator('#m-title')).toHaveValue('P3 手动补录影片');
  await page.locator('#m-path').fill(source); await page.locator('#manual-submit').click(); await expect(page.locator('#m-title')).toHaveValue('');
  expect(await readFile(source, 'utf8')).toBe(`${live.mediaURL}/sample.mp4\n`);
  const nfo = await readFile(path.join(directory, 'P3-001.nfo'), 'utf8'); expect(nfo).toContain('<year>2024</year>'); expect(nfo).toContain('<genre>剧情</genre>'); expect(nfo).toContain('<genre>测试</genre>');
  await navigate(page, 'items'); await page.locator('#item-search').fill('P3 手动补录影片'); await expect(page.locator('.wall-card')).toHaveCount(1);
  await expect(page.locator('.wall-card strong')).toHaveText('P3 手动补录影片');
  } finally {
    const indexed = await live.request('/api/admin/items?search=' + encodeURIComponent('P3 手动补录影片'));
    for (const item of indexed.items ?? []) {
      if (item.source_path === source) await live.request(`/api/admin/items/${item.id}`, { method: 'DELETE', expected: 204 });
    }
    const relative = path.relative(path.resolve(live.root), path.resolve(directory));
    if (!relative || relative.startsWith('..') || path.isAbsolute(relative)) throw new Error('Manual fixture cleanup must stay inside the owned environment');
    await rm(directory, { recursive: true, force: true });
  }
});
test('unknown protocol requests appear in probe and 204 clears the records', async ({ page, request, live }) => {
  await login(page, live); await request.get(`${live.baseURL}/P3UnknownClientEndpoint`);
  await navigate(page, 'probe'); await expect(page.locator('#content')).toContainText('/P3UnknownClientEndpoint');
  const cleared = page.waitForResponse(r => r.url().endsWith('/api/admin/probe') && r.request().method() === 'DELETE');
  await page.locator('#clear-probes').click(); expect((await cleared).status()).toBe(204); await expect(page.locator('#content')).toContainText('暂无探针记录');
});
test('a scan request stays alive across navigation, locks all scan buttons and refreshes current task history', async ({ page, live }) => {
  await login(page, live); let started!: () => void; let release!: () => void;
  const submitted = new Promise<void>(r => { started = r; });
  await page.route('**/api/admin/scan', async route => { started(); await new Promise<void>(r => { release = r; }); await route.continue(); });
  await page.locator('#scan').click(); await submitted; await navigate(page, 'tasks');
  await expect(page.locator('#scan')).toBeDisabled(); await expect(page.locator('#task-scan')).toBeDisabled(); await expect(page.locator('#scan-progress')).toBeVisible();
  const prior = (await live.request('/api/admin/tasks')).items.length;
  release(); await expect(page.locator('#toasts')).toContainText('增量扫描完成'); await expect(page.locator('#scan')).toBeEnabled();
  await expect.poll(() => page.locator('#content tbody tr').count()).toBe(prior + 1); await expect(page).toHaveURL(/#\/tasks$/);
  await page.unroute('**/api/admin/scan');
  await navigate(page, 'overview'); await page.locator('#reindex').click(); await expect(page.locator('#toasts')).toContainText('重建完成');
  await navigate(page, 'tasks'); await expect(page.locator('#content')).toContainText('全量重建索引');
  let wallStarted!: () => void; let wallRelease!: () => void;
  const wallSubmitted = new Promise<void>(r => { wallStarted = r; });
  await page.route('**/api/admin/scan', async route => { wallStarted(); await new Promise<void>(r => { wallRelease = r; }); await route.continue(); });
  await page.locator('#scan').click(); await wallSubmitted; await navigate(page, 'items');
  await page.locator('[data-status=success]').click(); await expect(page.locator('.wall-card')).toHaveCount(100);
  await page.locator('.wall-card').nth(99).scrollIntoViewIfNeeded(); await expect(page.locator('.wall-card')).toHaveCount(200);
  await page.locator('.wall-card').nth(199).scrollIntoViewIfNeeded(); await expect(page.locator('.wall-card')).toHaveCount(300);
  const card = page.locator('.wall-card').nth(249); await card.scrollIntoViewIfNeeded(); await card.focus();
  const id = await card.getAttribute('data-play'); const position = await page.evaluate(() => scrollY);
  let reads = 0; page.on('request', r => { if (new URL(r.url()).pathname === '/api/admin/items') reads++; });
  wallRelease(); await expect.poll(() => reads).toBe(3); await expect(page.locator('.wall-card')).toHaveCount(300);
  await expect.poll(() => page.evaluate(() => (document.activeElement as HTMLElement)?.dataset.play)).toBe(id);
  await expect.poll(() => page.evaluate(y => Math.abs(scrollY - y), position)).toBeLessThan(3);
  await page.unroute('**/api/admin/scan'); await card.press('Enter'); await expect(page.locator('.item-hero-title')).toBeVisible();
  let detailReads = 0; page.on('request', r => { if (new URL(r.url()).pathname === `/api/admin/items/${id}/detail`) detailReads++; });
  await page.locator('#scan').click(); await expect.poll(() => detailReads).toBe(1); await expect(page.locator('.item-hero-title')).toBeVisible();
});
