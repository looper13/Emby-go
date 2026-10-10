import path from 'node:path';
import { copyFile, readFile, rm, stat, writeFile } from 'node:fs/promises';
import type { Page } from '@playwright/test';
import { test, expect } from './fixtures';
async function login(page: Page, live: { baseURL: string; credentials: { username: string; password: string } }, target: string) {
  await page.goto(`${live.baseURL}/admin#${target}`); await page.locator('#auth-user').fill(live.credentials.username); await page.locator('#auth-pw').fill(live.credentials.password); await page.locator('#auth-form button').click(); await expect(page).toHaveURL(`${live.baseURL}/admin#${target}`);
}
async function movie(live: { request: (path: string, options?: object) => Promise<any> }, code = 'ABF-005') { return (await live.request('/api/admin/items?search=' + code)).items.find((m: { Number: string }) => m.Number === code); }
test('full detail sections, real reread and failed refresh preserve expansion, recommendations and scroll', async ({ page, live }, info) => {
  const m = await movie(live); const directory = path.dirname(m.source_path); const relative = path.relative(path.resolve(live.root), path.resolve(directory));
  expect(relative.startsWith('..') || path.isAbsolute(relative)).toBe(false);
  const nfo = path.join(directory, 'ABF-005.nfo'); const original = await readFile(nfo, 'utf8'); const extra = path.join(directory, 'fanart1.png');
  await copyFile(path.join(directory, 'fanart.png'), extra);
  await writeFile(nfo, original.replace(/<plot>.*?<\/plot>/s, '<plot>' + '长简介测试。'.repeat(90) + '</plot>').replace('</movie>', `<trailer>${live.mediaURL}/sample.mp4</trailer><actor><name>P4 Actor</name></actor></movie>`));
  await live.request(`/api/admin/items/${m.id}/reread`, { method: 'POST' });
  try {
    const similarReads: string[] = []; page.on('request', r => { if (r.url().includes(`/Items/${m.id}/Similar`)) similarReads.push(r.url()); });
    await login(page, live, `/item/${m.id}?library_id=${live.library!.Id}`);
    await expect(page.locator('#detail-play')).toBeVisible(); await expect(page.locator('#item-similar .similar-card').first()).toBeVisible();
    await expect(page.locator('.detail-artwork-grid a')).toHaveCount(2);
    expect((await page.request.get(`${live.baseURL}/Items/person:UDQgQWN0b3I/Images/Primary?maxWidth=200`)).status()).toBe(200);
    const trailer = page.locator('#item-trailer img'); const first = page.locator('.detail-artwork-grid img').first();
    expect(new URL((await trailer.getAttribute('src'))!, live.baseURL).pathname).toBe(new URL((await first.getAttribute('src'))!, live.baseURL).pathname);
    expect(await page.locator('.detail-artwork-grid a').nth(1).getAttribute('href')).toMatch(/\/Backdrop\/1\?tag=/);
    expect(await page.locator('.detail-artwork-grid a').first().getAttribute('href')).not.toContain('maxWidth');
    await page.locator('#plot-toggle').click(); await page.locator('.item-details summary').click(); await expect(page.locator('.item-details')).toHaveAttribute('open', '');
    await page.locator('#detail-reread').scrollIntoViewIfNeeded(); const y = await page.evaluate(() => scrollY);
    await page.locator('#detail-reread').click(); await expect(page.locator('#toasts')).toContainText('状态已更新'); await expect(page.locator('#detail-reread')).toBeEnabled();
    await expect(page.locator('#plot-toggle')).toHaveAttribute('aria-expanded', 'true'); await expect(page.locator('.item-details')).toHaveAttribute('open', '');
    await expect.poll(() => page.evaluate(expected => Math.abs(scrollY - expected), y)).toBeLessThan(3); expect(similarReads.length).toBe(1);
    let fail = true; await page.route(`**/api/admin/items/${m.id}/detail`, route => { if (fail) { fail = false; return route.fulfill({ status: 503, json: { error: 'P4 injected refresh failure' } }); } return route.continue(); });
    await page.locator('#detail-reread').click(); await expect(page.locator('#toasts')).toContainText('P4 injected refresh failure');
    await expect(page.locator('.item-hero-title')).toHaveText(m.Title); await expect(page.locator('#plot-toggle')).toHaveAttribute('aria-expanded', 'true'); await expect(page.locator('#detail-reread')).toBeEnabled();
    await page.locator('#detail-reread').click(); await expect(page.locator('#detail-reread')).toBeEnabled(); expect(similarReads.length).toBe(1);
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.screenshot({ path: info.outputPath('vue-detail-full.png'), fullPage: true, animations: 'disabled', style: '.detail-path, .item-details dd { color: transparent !important; text-shadow: none !important; }' });
    await page.locator('#item-trailer').click(); await expect(page.locator('#player-overlay')).toBeVisible(); await expect(page.locator('#player-title')).toContainText('预告片'); await expect(page.locator('#player-stage video')).toBeVisible();
    await page.keyboard.press('Escape'); await expect(page.locator('#player-overlay')).toHaveCount(0); await expect(page.locator('#item-trailer')).toBeFocused(); await expect(page).toHaveURL(new RegExp(`/item/${m.id}`));
  } finally { await writeFile(nfo, original); await rm(extra, { force: true }); await live.request(`/api/admin/items/${m.id}/reread`, { method: 'POST' }); }
});
test('entity chips preserve library, clear all entities, scrape filters toggle and recommendations navigate through history', async ({ page, request, live }) => {
  const m = await movie(live); await login(page, live, `/item/${m.id}?library_id=${live.library!.Id}&search=ignored`);
  await page.locator('.tag-chip[data-entity-value="Rare series"]').click(); await expect(page).toHaveURL(/#\/items\?tag=Rare\+series&library_id=/);
  await expect(page.locator('#entity-clear')).toBeVisible(); await expect(page.locator('.wall-card')).toHaveCount(10); await expect(page.locator('#page-title')).toHaveText('媒体墙');
  await page.locator('#entity-clear').click(); await expect(page).not.toHaveURL(/[?&](tag|search)=/); await expect(page.locator('.wall-card')).toHaveCount(100);
  await page.locator('[data-scrape=failed]').click(); await expect(page).toHaveURL(/scrape=failed/); await page.locator('[data-scrape=failed]').click(); await expect(page).not.toHaveURL(/scrape=/);
  await page.goto(`${live.baseURL}/admin#/item/${m.id}`); await expect(page.locator('#item-similar .similar-card').first()).toBeVisible();
  const recommendation = page.locator('#item-similar .similar-card').first(); const id = await recommendation.getAttribute('data-similar'); await recommendation.click(); await expect(page).toHaveURL(new RegExp(`/item/${id}$`));
  await page.locator('#item-back').click(); await expect(page).toHaveURL(new RegExp(`/item/${m.id}$`));
});
test('card actions are separate from navigation; real single probe writes NFO and deletion keeps source files', async ({ page, live }, info) => {
  const m = await movie(live, 'ABF-307'); await login(page, live, '/items?search=ABF-307'); const card = page.locator('.wall-card'); await expect(card).toHaveCount(1);
  await card.focus(); await expect(card.locator('.wall-actions')).toHaveCSS('opacity', '1');
  await expect(page.locator('.panel')).toHaveCSS('opacity', '1');
  const bounds = await card.boundingBox(); await card.hover(); expect(await card.boundingBox()).toEqual(bounds);
  await card.locator('[data-reread]').focus(); await page.keyboard.press('Enter'); await expect(page.locator('#toasts')).toContainText('状态已更新'); await expect(page).toHaveURL(/#\/items/);
  await card.locator('[data-quickplay]').click(); await expect(page.locator('#player-stage video')).toBeVisible(); await page.keyboard.press('Escape'); await expect(page.locator('#player-overlay')).toHaveCount(0); await expect(card.locator('[data-quickplay]')).toBeFocused(); await expect(page).toHaveURL(/#\/items/);
  await card.locator('[data-probe]').click(); await expect(page.locator('#toasts')).toContainText('已写入 NFO');
  expect(await readFile(path.join(path.dirname(m.source_path), 'ABF-307.nfo'), 'utf8')).toContain('<streamdetails>'); await expect(page).toHaveURL(/#\/items/);
  await card.focus(); await expect(card.locator('.wall-actions')).toHaveCSS('opacity', '1');
  await page.screenshot({ path: info.outputPath('vue-card-focus.png'), animations: 'disabled', style: '.wall-path { color: transparent !important; text-shadow: none !important; }' });
  page.once('dialog', dialog => dialog.dismiss()); await card.locator('[data-delete]').click(); await expect(card).toHaveCount(1);
  page.once('dialog', async dialog => { expect(dialog.message()).toBe('仅删除数据库索引，不删除源文件。继续？'); await dialog.accept(); });
  await card.locator('[data-delete]').click(); await expect(card).toHaveCount(0); expect((await stat(m.source_path)).isFile()).toBe(true);
  await live.request('/api/admin/scan', { method: 'POST' });
});
test('real Artplayer plays, closes and traps focus; one failed direct request falls back to muted proxy', async ({ page, live }, info) => {
  const m = await movie(live, 'ABF-299'); await login(page, live, `/item/${m.id}`);
  const requests: string[] = []; page.on('request', r => { if (r.url().includes(`/Videos/${m.id}/`)) requests.push(new URL(r.url()).pathname); });
  await page.route(`**/Videos/${m.id}/stream`, route => route.abort('failed'));
  await page.locator('#detail-play').click(); await expect(page.locator('#player-overlay')).toBeVisible(); await expect(page.locator('#player-stage video')).toBeVisible();
  await expect.poll(() => page.locator('#player-stage video').evaluate((el: HTMLVideoElement) => ({ ready: el.readyState >= 2, muted: el.muted, source: el.currentSrc.includes('/proxy') }))).toEqual({ ready: true, muted: true, source: true });
  expect(requests.filter(p => p.endsWith('/stream')).length).toBe(1); expect(requests.filter(p => p.endsWith('/proxy')).length).toBe(1);
  await page.locator('#player-close').focus(); await page.keyboard.press('Shift+Tab'); expect(await page.evaluate(() => Boolean(document.activeElement?.closest('[role=dialog]')))).toBe(true);
  await page.screenshot({ path: info.outputPath('vue-player-proxy.png'), animations: 'disabled' });
  await page.keyboard.press('Escape'); await expect(page.locator('#player-overlay')).toHaveCount(0); await expect(page.locator('#detail-play')).toBeFocused(); await expect(page).toHaveURL(new RegExp(`/item/${m.id}$`));
  await page.unroute(`**/Videos/${m.id}/stream`); await page.locator('#detail-play').click(); await expect(page.locator('#player-stage video')).toBeVisible();
  await expect.poll(() => page.locator('#player-stage video').evaluate((el: HTMLVideoElement) => el.readyState >= 2)).toBe(true);
  await page.locator('#player-close').click(); await expect(page.locator('#player-stage video')).toHaveCount(0); expect(await page.evaluate(() => document.body.classList.contains('player-open'))).toBe(false);
});
test('broken artwork becomes a placeholder, similar timeout does not block detail, and double player failure reports once', async ({ page, live }) => {
  const m = await movie(live); let release!: () => void; let started!: () => void; const pending = new Promise<void>(r => { started = r; });
  await page.route(`**/Items/${m.id}/Similar?*`, async route => { started(); await new Promise<void>(r => { release = r; }); await route.continue().catch(() => {}); });
  await page.route('**/Items/*/Images/**', route => route.fulfill({ status: 404, body: '' }));
  await login(page, live, `/item/${m.id}`); await pending; await expect(page.locator('.item-hero-title')).toBeVisible(); await expect(page.locator('.item-hero-poster.image-placeholder')).toBeVisible();
  await page.waitForTimeout(5200); await expect(page.locator('#item-similar')).toBeHidden(); release(); await page.unroute(`**/Items/${m.id}/Similar?*`);
  await page.route(`**/Videos/${m.id}/stream`, route => route.abort('failed')); await page.route(`**/Videos/${m.id}/proxy`, route => route.abort('failed'));
  await page.locator('#detail-play').click(); await expect(page.locator('#toasts .toast.error')).toHaveCount(1); await expect(page.locator('#player-overlay')).toBeVisible();
  await page.locator('#player-close').click(); await page.locator('[data-page=items]').click(); await expect(page.locator('#player-stage video')).toHaveCount(0);
});
