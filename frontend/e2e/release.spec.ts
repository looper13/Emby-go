import { copyFile } from 'node:fs/promises';
import path from 'node:path';
import { test, expect } from './fixtures';

test('P7 root and web logins canonicalize to admin, while old bookmarks preserve the target', async ({ page, live }) => {
  const item = (await live.request('/api/admin/items?search=ABF-005')).items[0];
  for (const entry of ['/', '/web', '/web/index.html']) {
    await page.goto(`${live.baseURL}${entry}?library_id=${live.library!.Id}#item/${item.id}`);
    if (entry === '/') {
      await expect(page.locator('#auth-form')).toBeVisible();
      await page.locator('#auth-user').fill(live.credentials.username); await page.locator('#auth-pw').fill(live.credentials.password);
      await page.locator('#auth-form button[type=submit]').click();
    }
    await expect(page.locator('.item-hero-title')).toHaveText(item.Title);
    await expect(page).toHaveURL(`${live.baseURL}${entry === '/web/index.html' ? entry : '/admin'}#/item/${item.id}?library_id=${live.library!.Id}`);
    await page.reload(); await expect(page.locator('.item-hero-title')).toHaveText(item.Title);
  }
  await page.goto(`${live.baseURL}/admin-vue?library_id=${live.library!.Id}#items`);
  await expect(page).toHaveURL(`${live.baseURL}/admin#/items?library_id=${live.library!.Id}`);
  await expect(page.locator('.wall-card')).toHaveCount(100);
});

test('P7 390/768/1280 layouts and player overlays do not overflow or cover their close action', async ({ page, live }, info) => {
  await page.goto(`${live.baseURL}/admin#/items?search=ABF-005`);
  await page.locator('#auth-user').fill(live.credentials.username); await page.locator('#auth-pw').fill(live.credentials.password);
  await page.locator('#auth-form button[type=submit]').click(); await expect(page.locator('.wall-card')).toHaveCount(1);
  for (const width of [390, 768, 1280]) {
    await page.setViewportSize({ width, height: 844 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.locator('[data-quickplay]').click(); await expect(page.locator('#player-stage video')).toBeVisible();
    const close = await page.locator('#player-close').boundingBox(); expect(close).not.toBeNull();
    expect(close!.x).toBeGreaterThanOrEqual(0); expect(close!.x + close!.width).toBeLessThanOrEqual(width);
    expect(await page.locator('#player-close').evaluate(el => { const b = el.getBoundingClientRect(); return el.contains(document.elementFromPoint(b.x + b.width / 2, b.y + b.height / 2)); })).toBe(true);
    await page.screenshot({ path: info.outputPath(`player-${width}.png`), animations: 'disabled', mask: [page.locator('.wall-path')] });
    await page.locator('#player-close').click(); await expect(page.locator('video')).toHaveCount(0);
  }
});

test('P7 previous binary can log in and browse the same isolated database, then return to Vue', async ({ page, live }, info) => {
  test.skip(!process.env.EMBY_E2E_ROLLBACK_BIN, 'Run verify-release.mjs and set EMBY_E2E_ROLLBACK_BIN');
  test.setTimeout(120_000);
  const current = path.join(live.root, 'current-before-rollback.exe');
  await copyFile(live.binaryPath, current);
  try {
    await live.restart(process.env.EMBY_E2E_ROLLBACK_BIN);
    await page.goto(`${live.baseURL}/`);
    await expect(page.locator('#auth-form')).toBeVisible();
    await page.locator('#auth-user').fill(live.credentials.username); await page.locator('#auth-pw').fill(live.credentials.password);
    await page.locator('#auth-form button[type=submit]').click();
    await expect(page).toHaveURL(`${live.baseURL}/admin`);
    // The legacy shell installs navigation only after its asynchronous boot.
    await expect(page.locator('#page-title')).toHaveText('总览');
    await expect(page.locator('#content .cards')).toBeVisible();
    await page.locator('[data-page=items]').click(); await expect(page.locator('.wall-card')).toHaveCount(100);
    await page.locator('.wall-card strong').first().click(); await expect(page.locator('.item-hero-title')).toBeVisible();
    await info.attach('rollback', { body: JSON.stringify({ platform: process.platform, previous: 'pre-migration binary from EMBY_E2E_ROLLBACK_BIN', login: true, list: true, detail: true, linuxSystemd: 'not run' }), contentType: 'application/json' });
  } finally { await live.restart(current); }
  // A hash-only navigation at /admin keeps the old document alive after a binary swap.
  await page.goto(`${live.baseURL}/admin#/items`); await page.reload();
  await expect(page.locator('.wall-card')).toHaveCount(100);
  await expect(page.locator('#nav')).toBeVisible();
});
