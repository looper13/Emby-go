import type { Page } from '@playwright/test';
import { test, expect } from './fixtures';
import { startLiveEnvironment } from '../../vue-migration/tools/live-env/environment.mjs';
async function login(page: Page, live: { baseURL: string; credentials: { username: string; password: string } }, target = '/items?status=success', expected = target) {
  await page.goto(`${live.baseURL}/admin#${target}`);
  await expect(page.locator('#auth-form')).toBeVisible();
  await page.locator('#auth-user').fill(live.credentials.username);
  await page.locator('#auth-pw').fill(live.credentials.password);
  await page.locator('#auth-form button[type=submit]').click();
  await expect(page).toHaveURL(`${live.baseURL}/admin#${expected}`);
}
test('300/250 history restores focus and position without duplicate or return pagination', async ({ page, live }, info) => {
  const reads: string[] = []; const errors: string[] = [];
  page.on('pageerror', e => errors.push(e.message));
  page.on('request', r => { if (new URL(r.url()).pathname === '/api/admin/items') reads.push(new URL(r.url()).search); });
  await login(page, live);
  await expect(page.locator('.wall-card')).toHaveCount(100);
  await page.locator('.wall-card').nth(99).scrollIntoViewIfNeeded();
  await expect(page.locator('.wall-card')).toHaveCount(200);
  await page.locator('.wall-card').nth(199).scrollIntoViewIfNeeded();
  await expect(page.locator('.wall-card')).toHaveCount(300);
  const card = page.locator('.wall-card').nth(249);
  await card.scrollIntoViewIfNeeded(); await card.focus();
  const id = await card.getAttribute('data-play'); const title = await card.locator('strong').textContent();
  const position = await page.evaluate(() => ({ y: scrollY, top: document.querySelectorAll('.wall-card')[249]!.getBoundingClientRect().top }));
  const initialReads = reads.length;
  await card.press('Enter');
  await expect(page).toHaveURL(new RegExp(`/item/${id}\\?status=success$`));
  await expect(page.locator('.item-hero-title')).toHaveText(title!);
  await expect(page.locator('#page-title')).toHaveText(title!);
  expect(await page.title()).toBe('Emby-go · 控制台');
  await expect(page.locator('[data-page=items]')).toHaveAttribute('aria-current', 'page');
  await page.locator('#item-back').click();
  await expect(page.locator('.wall-card')).toHaveCount(300);
  await expect.poll(() => page.evaluate(() => (document.activeElement as HTMLElement)?.dataset.play)).toBe(id);
  await expect.poll(() => page.evaluate(y => Math.abs(scrollY - y), position.y)).toBeLessThan(3);
  expect(reads.length).toBe(initialReads);
  expect(new Set(reads).size).toBe(reads.length);
  await page.goForward(); await expect(page.locator('.item-hero-title')).toHaveText(title!);
  await page.keyboard.press('Escape'); await expect(page.locator('.wall-card')).toHaveCount(300);
  await expect.poll(() => page.evaluate(() => (document.activeElement as HTMLElement)?.dataset.play)).toBe(id);
  expect(reads.length).toBe(initialReads);
  await expect(page.locator('.panel')).toHaveCSS('opacity', '1');
  await page.screenshot({ path: info.outputPath('vue-wall-300-250.png'), animations: 'disabled',
    mask: [page.locator('.wall-path')], maskColor: '#262b31' });
  await info.attach('request-counts', { body: JSON.stringify({ pages: reads, returnPages: reads.length - initialReads, restoredCount: 300, focusOrdinal: 250, position }), contentType: 'application/json' });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect(await page.evaluate(() => history.scrollRestoration)).toBe('manual');
  expect(errors).toEqual([]);
});
test('old outer query, direct details and reload return stay inside the console', async ({ page, live }) => {
  await login(page, live, '/overview');
  const list = await live.request('/api/admin/items?status=success&limit=1'); const id = list.items[0].id;
  await page.goto(`${live.baseURL}/admin?library_id=${live.library!.Id}&search=${encodeURIComponent('深夜 & 测试')}#item/${id}`);
  await expect(page.locator('.item-hero-title')).toBeVisible();
  expect(new URL(page.url()).search).toBe('');
  expect(new URLSearchParams(new URL(page.url()).hash.split('?')[1]).get('search')).toBe('深夜 & 测试');
  await page.reload(); await expect(page.locator('.item-hero-title')).toBeVisible();
  await page.locator('#item-back').click(); await expect(page).toHaveURL(/admin#\/items\?/);
  await expect(page.locator('#item-search')).toHaveValue('深夜 & 测试');
});
test('discrete filters push, search replaces, and leaving cancels its debounce', async ({ page, live }) => {
  await login(page, live); await expect(page.locator('.wall-card')).toHaveCount(100);
  const start = await page.evaluate(() => history.length);
  await page.locator('[data-status=pending]').click(); await expect(page.locator('.wall-card')).toHaveCount(3);
  expect(await page.evaluate(() => history.length)).toBe(start + 1);
  await page.goBack(); await expect(page.locator('.wall-card')).toHaveCount(100);
  const before = await page.evaluate(() => history.length);
  await page.locator('#item-search').fill('ABF-005'); await expect(page.locator('.wall-card')).toHaveCount(1);
  expect(await page.evaluate(() => history.length)).toBe(before);
  await page.locator('#item-search').fill('draft'); await page.locator('[data-page=overview]').click();
  await expect(page).toHaveURL(/#\/overview$/);
  // Observe longer than the production debounce; no stale replace may occur.
  await expect(page.locator('#page-title')).toHaveText('总览');
  await page.waitForTimeout(500); await expect(page).toHaveURL(/#\/overview$/);
});
test('late detail and late pagination cannot replace newer pages or filters', async ({ page, live }) => {
  await login(page, live); await expect(page.locator('.wall-card')).toHaveCount(100);
  let release!: () => void;
  let started!: () => void; const pending = new Promise<void>(r => { started = r; });
  await page.route('**/api/admin/items/*/detail', async route => {
    const response = await route.fetch(); started(); await new Promise<void>(r => { release = r; });
    await route.fulfill({ response }).catch(() => {});
  });
  await page.locator('.wall-card strong').first().click(); await pending;
  await page.locator('[data-page=overview]').click(); await expect(page.locator('#page-title')).toHaveText('总览');
  release(); await page.unroute('**/api/admin/items/*/detail');
  await page.locator('[data-page=items]').click(); await expect(page.locator('.wall-card')).toHaveCount(100);
  let releasePage!: () => void; let pageStarted!: () => void; const paging = new Promise<void>(r => { pageStarted = r; });
  await page.route('**/api/admin/items?**', async route => {
    if (new URL(route.request().url()).searchParams.get('offset') !== '100') return route.continue();
    const response = await route.fetch(); pageStarted(); await new Promise<void>(r => { releasePage = r; });
    await route.fulfill({ response }).catch(() => {});
  });
  await page.locator('.wall-card').nth(99).scrollIntoViewIfNeeded(); await paging;
  await page.locator('[data-status=pending]').click(); await expect(page.locator('.wall-card')).toHaveCount(3);
  releasePage(); await page.unroute('**/api/admin/items?**');
  await expect(page.locator('.wall-card')).toHaveCount(3); await expect(page.locator('#page-title')).toHaveText('媒体墙');
});
test('invalid library is removed without losing other query parameters', async ({ page, live }) => {
  await login(page, live, '/items?library_id=999999&status=success&search=ABF-005', '/items?status=success&search=ABF-005');
  await expect(page.locator('.wall-card')).toHaveCount(1);
  await expect(page).toHaveURL(/#\/items\?status=success&search=ABF-005$/);
});
test('network failure preserves the token; concurrent 401 restores the original target after login', async ({ page, live }) => {
  await login(page, live); await expect(page.locator('.wall-card')).toHaveCount(100);
  await page.route('**/Users/Me', route => route.abort('failed'));
  await page.reload(); await expect(page.locator('.auth-error')).toContainText('无法连接');
  expect(await page.evaluate(() => Boolean(localStorage.getItem('emby_token')))).toBe(true);
  await page.unroute('**/Users/Me'); await page.getByRole('button', { name: '重试', exact: true }).click();
  await expect(page.locator('.wall-card')).toHaveCount(100);
  await page.route('**/api/admin/libraries', route => route.fulfill({ status: 401, contentType: 'application/json', body: '{"error":"登录已失效"}' }));
  await page.route('**/api/admin/status', route => route.fulfill({ status: 401, contentType: 'application/json', body: '{"error":"登录已失效"}' }));
  await page.locator('[data-page=overview]').click(); await expect(page.locator('#auth-form')).toBeVisible();
  await expect.poll(() => page.evaluate(() => localStorage.getItem('emby_token'))).toBeNull();
  await expect(page).toHaveURL(/#\/login\?/);
  expect(new URLSearchParams(new URL(page.url()).hash.split('?')[1]).get('returnTo')).toBe('/overview');
  await page.unroute('**/api/admin/libraries'); await page.unroute('**/api/admin/status');
  await page.locator('#auth-user').fill(live.credentials.username); await page.locator('#auth-pw').fill(live.credentials.password);
  await page.locator('#auth-form button[type=submit]').click(); await expect(page).toHaveURL(/admin#\/overview$/);
});
test('first initialization and subsequent login use the real Go endpoints', async ({ page, live }) => {
  const fresh = await startLiveEnvironment({ binaryPath: live.binaryPath, bootstrap: false });
  live.addSecret(fresh.credentials.username); live.addSecret(fresh.credentials.password);
  live.addSecret(fresh.root); live.addSecret(fresh.root.replaceAll('\\', '/'));
  live.addSecret(JSON.stringify(fresh.root).slice(1, -1));
  try {
    // The default fixture permits only its own origins; explicitly permit this owned instance.
    await page.context().route(`${fresh.baseURL}/**`, route => route.continue());
    await page.goto(`${fresh.baseURL}/admin#/items`); await expect(page.locator('#auth-confirm')).toBeVisible();
    await page.locator('#auth-user').fill(fresh.credentials.username); await page.locator('#auth-pw').fill(fresh.credentials.password);
    await page.locator('#auth-confirm').fill(fresh.credentials.password);
    await page.locator('#auth-form button[type=submit]').click(); await expect(page.locator('.auth-error')).toContainText('初始化完成');
    await page.locator('#auth-form button[type=submit]').click(); await expect(page).toHaveURL(/admin#\/items$/);
    live.addSecret(await page.evaluate(() => localStorage.getItem('emby_token')));
    await expect(page.locator('#wall-empty')).toContainText('没有符合条件');
    await page.reload(); await expect(page).toHaveURL(/admin#\/items$/);
  } finally { await fresh.close(); }
});
