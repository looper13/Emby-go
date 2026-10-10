import { test, expect } from './fixtures';

test('embedded assets, old entries and protocol errors remain separate', async ({ request, live }) => {
  const html = await request.get(`${live.baseURL}/admin`);
  expect(html.status()).toBe(200);
  expect(html.headers()['cache-control']).toBe('no-store');
  const text = await html.text();
  expect(text).not.toContain('/web/app.js');
  const assets = [...text.matchAll(/(?:src|href)="(\/web\/ui\/[^"?#]+)"/g)].map(match => match[1]);
  expect(assets.length).toBeGreaterThanOrEqual(3);
  for (const asset of assets) {
    const response = await request.get(live.baseURL + asset);
    expect(response.status()).toBe(200);
    const head = await request.head(live.baseURL + asset);
    expect(head.status()).toBe(200);
    expect((await head.body()).length).toBe(0);
    expect(head.headers()['content-length']).toBe(response.headers()['content-length']);
    if (asset.endsWith('.js')) {
      expect(response.headers()['content-type']).toContain('javascript');
      expect(response.headers()['cache-control']).toContain('immutable');
    }
    if (asset.endsWith('.css')) expect(response.headers()['content-type']).toContain('text/css');
    if (asset.endsWith('.ico')) expect(response.headers()['cache-control']).toBe('no-cache');
  }
  expect((await request.get(`${live.baseURL}/web/ui/assets/missing-chunk.js`)).status()).toBe(404);
  expect((await request.get(`${live.baseURL}/web/ui/build-info.json`)).status()).toBe(404);
  for (const entry of ['/', '/web', '/admin', '/web/index.html']) {
    const shell = await request.get(live.baseURL + entry);
    expect(await shell.text()).toBe(text);
    const head = await request.head(live.baseURL + entry);
    expect(head.status()).toBe(200); expect(head.headers()['cache-control']).toBe('no-store');
    expect((await head.body()).length).toBe(0);
  }
  const api = await request.get(`${live.baseURL}/api/admin/status`);
  expect(api.status()).toBe(401);
  expect(api.headers()['content-type']).toContain('application/json');
});

test('real login stays in Vue and reload reuses the stored token', async ({ page, live }, info) => {
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.goto(`${live.baseURL}/admin`);
  await expect(page.locator('#auth-form')).toBeVisible();
  await page.locator('#auth-user').fill(live.credentials.username);
  await page.locator('#auth-pw').fill(live.credentials.password);
  await page.locator('#auth-form button[type=submit]').click();
  await expect(page).toHaveURL(/\/admin#\/overview$/);
  await expect(page.locator('#page-title')).toHaveText('总览');
  await expect(page.locator('#content')).toContainText('304');
  await expect(page.locator('.card').last()).toHaveCSS('opacity', '1');
  await expect(page.locator('.panel')).toHaveCSS('opacity', '1');
  await page.reload();
  await expect(page).toHaveURL(/\/admin#\/overview$/);
  await expect(page.locator('#content')).toContainText('304');
  const dimensions = await page.evaluate(() => ({
    width: innerWidth,
    document: document.documentElement.scrollWidth,
    body: document.body.scrollWidth,
  }));
  expect(dimensions.document).toBeLessThanOrEqual(dimensions.width);
  expect(dimensions.body).toBeLessThanOrEqual(dimensions.width);
  await page.screenshot({ path: info.outputPath('vue-overview.png'), fullPage: true, animations: 'disabled' });
  expect(errors).toEqual([]);
});

test('failed login keeps input and invalid token returns to login', async ({ page, live }) => {
  await page.addInitScript(() => localStorage.setItem('emby_token', 'invalid-fixture-token'));
  await page.goto(`${live.baseURL}/admin#/overview`);
  await expect(page.locator('#auth-form')).toBeVisible();
  await expect.poll(() => page.evaluate(() => localStorage.getItem('emby_token'))).toBeNull();
  await page.locator('#auth-user').fill(live.credentials.username);
  await page.locator('#auth-pw').fill('incorrect-fixture-password');
  await page.locator('#auth-form button[type=submit]').click();
  await expect(page.locator('.auth-error')).not.toHaveText('');
  await expect(page.locator('#auth-user')).toHaveValue(live.credentials.username);
  await expect(page.locator('#auth-pw')).toHaveValue('incorrect-fixture-password');
});
