import { test, expect } from './fixtures';
import { login } from './p5-fixtures';

test('configuration draft, explicit save, live name, restart state and responsive layout', async ({ page, live }, info) => {
  const original = await live.request('/api/admin/config');
  await login(page, live, '/settings');
  await expect(page.locator('#cfg-name')).toHaveValue(original.config.server_name);
  await page.locator('#cfg-name').fill('Unsaved fixture');
  expect((await live.request('/System/Info/Public')).ServerName).toBe(original.config.server_name);
  await expect(page.locator('#cfg-dirty')).toContainText('未保存');
  await page.locator('#cfg-restore').click();
  await expect(page.locator('#cfg-name')).toHaveValue(original.config.server_name);
  await page.locator('#cfg-name').fill('Settings acceptance');
  await page.locator('#cfg-debug').check();
  await page.locator('#cfg-add-domain').click();
  await page.locator('#cfg-domain-name-0').fill('Fixture');
  await page.locator('#cfg-domain-url-0').fill('https://example.invalid/emby');
  await page.locator('#cfg-save').click();
  await expect(page.locator('#cfg-dirty')).toContainText('与已保存配置一致');
  await expect(page.getByText('已保存，等待重启：调试模式。当前服务继续使用原值。')).toBeVisible();
  const saved = await live.request('/api/admin/config');
  expect(saved.config.debug).toBe(true); expect(saved.effective.debug).toBe(false);
  expect((await live.request('/System/Info/Public')).ServerName).toBe('Settings acceptance');
  expect(saved.config).not.toHaveProperty('redis_password');
  await page.reload(); await expect(page.locator('#cfg-name')).toHaveValue('Settings acceptance');
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({ path: info.outputPath('system-config.png'), fullPage: true, animations: 'disabled', mask: [page.locator('[data-private-path], [data-secret]')] });
  const current = await live.request('/api/admin/config');
  await live.request('/api/admin/config', { method: 'PUT', body: { revision: current.revision, config: {
    server_name: original.config.server_name, debug: original.config.debug, server_domains: original.config.server_domains,
  } } });
});
