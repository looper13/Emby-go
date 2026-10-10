import { readFile, readdir } from 'node:fs/promises';
import { test, expect, login, navigate, disk } from './p5-fixtures';

test('scrape settings retain drafts and masked secrets; real connectivity and layout', async ({ page, live, p5 }, info) => {
  await login(page, live);
  await expect(page.locator('#sc-token')).toHaveValue(''); await expect(page.locator('#sc-token')).toHaveAttribute('type', 'password');
  await page.locator('#sc-concurrency').fill('2');
  let fail = true; await page.route('**/api/admin/scrape/settings', route => {
    if (route.request().method() === 'PUT' && fail) { fail = false; return route.fulfill({ status: 503, json: { error: 'P5 save failed' } }); }
    return route.continue();
  });
  await page.locator('#scrape-save').click(); await expect(page.locator('#toasts')).toContainText('P5 save failed'); await expect(page.locator('#sc-concurrency')).toHaveValue('2');
  await page.locator('#scrape-save').click(); await expect(page.locator('#toasts')).toContainText('配置已保存'); await expect(page.locator('#sc-token')).toHaveValue('');
  await page.locator('#test-metatube').click(); await expect(page.locator('#toasts')).toContainText('MetaTube 可达');
  await page.locator('#test-translate').click(); await expect(page.locator('#toasts')).toContainText('P5 translated');
  await page.locator('#sc-lib').selectOption(String(p5.libraryId)); await page.locator('#sc-only-missing').uncheck(); await expect(page.locator('#scrape-count')).toHaveText('本次将处理 2 条');
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({ path: info.outputPath('vue-scrape.png'), fullPage: true, animations: 'disabled', style: 'input {color:transparent!important}' });
  await navigate(page, 'scheduled'); await navigate(page, 'scrape'); await expect(page.locator('#sc-concurrency')).toHaveValue('2');
});

test('preview cancellation and late candidate reads preserve physical files and focus', async ({ page, live, p5 }, info) => {
  const movie = p5.movies[0]!, before = await disk(movie.directory); let confirms = 0;
  page.on('request', request => { if (new URL(request.url()).pathname === `/api/admin/items/${movie.id}/scrape`) confirms++; });
  await login(page, live, `/item/${movie.id}`); await page.locator('#detail-scrape').click();
  await expect(page.locator('#scrape-confirm')).toBeEnabled(); await expect(page.locator('.scrape-detail h3').first()).toHaveText('ABF-901 P5 Alpha');
  const footer = await page.locator('#scrape-confirm').boundingBox();
  p5.mode.delay = 700;
  await page.locator('.scrape-candidate').nth(0).click(); await page.locator('.scrape-candidate').nth(1).click();
  await expect(page.locator('.scrape-detail h3').first()).toHaveText('ABF-901 P5 Beta');
  expect(Math.abs((await page.locator('#scrape-confirm').boundingBox())!.y - footer!.y)).toBeLessThan(2);
  await page.locator('#scrape-confirm').focus(); await page.keyboard.press('Tab'); await expect(page.locator('#scrape-close')).toBeFocused();
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({ path: info.outputPath('vue-scrape-preview.png'), fullPage: true, animations: 'disabled', style: '.detail-path,.item-details dd {color:transparent!important}' });
  await page.keyboard.press('Escape'); await expect(page.locator('#scrape-overlay')).toHaveCount(0); await expect(page.locator('#detail-scrape')).toBeFocused();
  expect(await disk(movie.directory)).toEqual(before); expect(confirms).toBe(0);
  await page.locator('#detail-scrape').click(); await page.evaluate(() => { location.hash = '#/scrape'; }); await expect(page.locator('#scrape-overlay')).toHaveCount(0);
  expect(await disk(movie.directory)).toEqual(before); expect(confirms).toBe(0); expect(p5.mode.authorized).toBe(true);
});

test('CT03 real confirmation preserves explicit false and changes NFO/images with true or defaults', async ({ page, live, p5 }) => {
  const movie = p5.movies[0]!;
  await live.request(`/api/admin/items/${movie.id}/scrape`, { method: 'POST', body: { provider: 'fanza', id: 'ABF-901-a', overwrite: true } });
  await p5.reset(); const baseline = await disk(movie.directory);
  expect(baseline['ABF-901-poster.webp']).toBeDefined();
  await login(page, live, `/item/${movie.id}`);
  let writes = 0; page.on('request', request => { if (new URL(request.url()).pathname === `/api/admin/items/${movie.id}/scrape`) writes++; });
  await page.locator('#detail-scrape').click(); await expect(page.locator('#scrape-confirm')).toBeEnabled();
  await page.locator('.scrape-candidate').nth(1).click(); await expect(page.locator('.scrape-detail h3').first()).toHaveText('ABF-901 P5 Beta');
  await page.locator('#scrape-force').uncheck();
  p5.mode.delay = 400; await page.locator('#scrape-confirm').dblclick(); await expect(page.locator('#scrape-overlay')).toHaveCount(0);
  expect(writes).toBe(1); await expect(page.locator('.item-hero-title')).toHaveText('P5 Original ABF-901');
  let nfo = await readFile(movie.nfo, 'utf8'); expect(nfo).toContain('Original plot'); expect(nfo).toContain('P5 Director'); expect(nfo).toContain('P5 retained');
  let after = await disk(movie.directory); for (const name of ['ABF-901-poster.webp','ABF-901-fanart.webp','ABF-901-landscape.webp']) expect(after[name]).toEqual(baseline[name]);
  await page.locator('#detail-scrape').click(); await expect(page.locator('#scrape-confirm')).toBeEnabled(); await page.locator('.scrape-candidate').nth(1).click();
  await expect(page.locator('.scrape-detail h3').first()).toHaveText('ABF-901 P5 Beta'); await page.locator('#scrape-force').check();
  await page.locator('#scrape-confirm').click(); await expect(page.locator('#scrape-overlay')).toHaveCount(0); await expect(page.locator('.item-hero-title')).toHaveText('ABF-901 P5 Beta');
  after = await disk(movie.directory); expect(after['ABF-901-poster.webp']!.hash).not.toBe(baseline['ABF-901-poster.webp']!.hash);
  nfo = await readFile(movie.nfo, 'utf8'); expect(nfo).toContain('P5 new summary'); expect(nfo).toContain('P5 retained');
  for (const global of [false,true]) {
    await p5.reset(); await live.request('/api/admin/scrape/settings', { method: 'PUT', body: { overwrite: global } });
    await live.request(`/api/admin/items/${movie.id}/scrape`, { method: 'POST', body: { provider: 'fanza', id: 'ABF-901-b' } });
    // 上游标题以番号开头，joinNumberTitle 改写前缀后即为规范形态「ABF-901 P5 Beta」。
    expect(await readFile(movie.nfo, 'utf8')).toContain(`<title>${global ? 'ABF-901 P5 Beta' : 'P5 Original ABF-901'}</title>`);
  }
});

test('real batch survives navigation and reload; mutual exclusion and partial failure stay visible', async ({ page, live, p5 }) => {
  p5.mode.delay = 400; p5.mode.fail.add('ABF-902');
  await login(page, live); await page.locator('#sc-lib').selectOption(String(p5.libraryId)); await page.locator('#sc-only-missing').uncheck(); await page.locator('#sc-force').check();
  await page.locator('#scrape-start').click(); await expect(page.locator('#scrape-progress-title')).toContainText('正在刮削');
  const started = (await live.request('/api/admin/scrape/progress')).started_at;
  await navigate(page, 'overview'); await expect(page.locator('#reindex')).toBeDisabled(); await expect(page.locator('#scan')).toBeDisabled();
  await live.request('/api/admin/scan', { method: 'POST', body: { library_id: p5.libraryId }, expected: 409 });
  await navigate(page, 'scrape'); await page.reload(); await expect(page.locator('#scrape-progress')).toBeVisible();
  await expect.poll(async () => (await live.request('/api/admin/scrape/progress')).running).toBe(false);
  const result = await live.request('/api/admin/scrape/progress'); expect(result.started_at).toBe(started); expect(result.success).toBe(1); expect(result.failed).toBe(1);
  await expect(page.locator('#scrape-progress-detail')).toContainText('失败 1'); await expect(page.locator('.scrape-fail-samples')).toBeVisible(); await expect(page.locator('#scrape-start')).toBeEnabled();
  expect(await readFile(p5.movies[0]!.nfo, 'utf8')).toContain('P5 Alpha'); expect(await readFile(p5.movies[1]!.nfo, 'utf8')).toContain('P5 Original');
});

test('cancellation stops a real batch and avatars write only the owned media', async ({ page, live, p5 }) => {
  p5.mode.delay = 2000;
  await login(page, live); await page.locator('#sc-lib').selectOption(String(p5.libraryId)); await page.locator('#sc-only-missing').uncheck();
  const before = await disk(p5.movies[0]!.directory);
  await page.locator('#scrape-start').click(); await expect(page.locator('#scrape-cancel')).toBeEnabled(); await page.locator('#scrape-cancel').click();
  await expect.poll(async () => (await live.request('/api/admin/scrape/progress')).cancelled).toBe(true);
  await expect(page.locator('#scrape-progress-title')).toHaveText('刮削已中止'); expect(await disk(p5.movies[0]!.directory)).toEqual(before);
  p5.mode.delay = 0; await page.locator('#avatar-start').click();
  await expect.poll(async () => (await live.request('/api/admin/scrape/progress')).kind).toBe('scrape_avatars');
  await expect.poll(async () => (await live.request('/api/admin/scrape/progress')).running).toBe(false);
  const result = await live.request('/api/admin/scrape/progress'); expect(result.failed).toBe(0); expect(result.success).toBe(1);
  expect((await readdir(p5.settings.avatars_dir)).length).toBeGreaterThan(0); expect(await readFile(p5.movies[0]!.nfo, 'utf8')).toContain('<thumb>');
  await expect(page.locator('#scrape-progress-title')).toHaveText('头像任务结束');
});

test('batch probe restores after reload and only-missing repeat skips completed items', async ({ page, live, p5 }) => {
  // The global wall action has no library picker. Scope only the test wire to owned slow fixtures.
  await page.route('**/api/admin/probe/media', route => {
    if (route.request().method() !== 'POST') return route.continue();
    const payload = route.request().postDataJSON(); expect(payload).toEqual({ only_missing: true });
    return route.continue({ postData: JSON.stringify({ ...payload, library_id: p5.libraryId }) });
  });
  await login(page, live, `/items?library_id=${p5.libraryId}`); await page.locator('#probe-media').click();
  await expect(page.locator('#probe-progress-title')).toContainText('正在探测');
  const started = (await live.request('/api/admin/probe/media/progress')).started_at;
  await navigate(page, 'overview'); await expect(page.locator('#reindex')).toBeDisabled(); await page.goBack(); await page.reload();
  await expect.poll(async () => (await live.request('/api/admin/probe/media/progress')).running).toBe(false);
  const result = await live.request('/api/admin/probe/media/progress'); expect(result.started_at).toBe(started); expect(result.success).toBe(2); expect(result.failed).toBe(0);
  expect(await readFile(p5.movies[0]!.nfo, 'utf8')).toContain('<streamdetails>');
  await expect(page.locator('#probe-media')).toBeEnabled(); await page.locator('#probe-media').click(); await expect(page.locator('#toasts')).toContainText('成功 0 / 跳过 2 / 失败 0');
});

test('scheduled real CRUD validates cron, preserves edit selection, executes and leaves disk intact', async ({ page, live, p5 }, info) => {
  await login(page, live, '/scheduled'); const name = `P5 ${info.project.name} scheduled`; let id = 0;
  try {
    await page.locator('#s-name').fill(name); await page.locator('#s-cron').fill('invalid'); await expect(page.locator('#s-preview')).toContainText('表达式非法');
    await page.locator('#sched-submit').click(); await expect(page.locator('#s-name')).toHaveValue(name);
    await page.locator('[data-cron="0 3 * * *"]').click(); await expect(page.locator('#s-preview')).toContainText('接下来执行');
    await page.locator('#s-type').selectOption('scrape'); await page.locator('#s-overwrite').selectOption('false'); await page.locator('#s-only-missing').uncheck(); await page.locator('#s-limit').fill('1'); await page.locator('#s-lib').selectOption(String(p5.libraryId)); await page.locator('#s-enabled').uncheck();
    await page.locator('#sched-submit').click(); await expect(page.locator('#s-name')).toHaveValue('');
    const created = (await live.request('/api/admin/scheduled')).items.find((task: { name: string }) => task.name === name); id = created.id; expect(JSON.parse(created.params)).toEqual({ library_id: p5.libraryId, only_missing: false, overwrite: false, limit: 1 });
    const row = page.locator(`[data-scheduled="${id}"]`); await row.locator('[data-role=edit]').click(); await navigate(page, 'scrape'); await navigate(page, 'scheduled');
    await expect(page.locator('#s-name')).toHaveValue(name); await expect(page.locator('#s-overwrite')).toHaveValue('false');
    await page.screenshot({ path: info.outputPath('vue-scheduled.png'), fullPage: true, animations: 'disabled' });
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.locator('#s-type').selectOption('scan'); await page.locator('#sched-submit').click(); await expect(page.locator('#s-name')).toHaveValue('');
    await row.locator('label.switch').click(); await expect.poll(async () => (await live.request('/api/admin/scheduled')).items.find((t: { id: number }) => t.id === id).enabled).toBe(true);
    await row.locator('label.switch').click(); await expect.poll(async () => (await live.request('/api/admin/scheduled')).items.find((t: { id: number }) => t.id === id).enabled).toBe(false);
    await row.locator('[data-role=run]').click(); await expect(row).toContainText('成功');
    await navigate(page, 'tasks'); await expect(page.locator('#content')).toContainText('增量扫描'); await navigate(page, 'scheduled');
    const before = await disk(p5.movies[0]!.directory); page.once('dialog', dialog => dialog.dismiss()); await row.locator('[data-role=del]').click(); await expect(row).toBeVisible();
    page.once('dialog', dialog => dialog.accept()); await row.locator('[data-role=del]').click(); await expect(row).toHaveCount(0); expect(await disk(p5.movies[0]!.directory)).toEqual(before); id = 0;
  } finally { if (id) await live.request(`/api/admin/scheduled/${id}`, { method: 'DELETE', expected: 204 }); }
});

test('owned service restart keeps scheduled definitions but does not resume background work', async ({ page, live, p5 }) => {
  const task = await live.request('/api/admin/scheduled', { method: 'POST', body: { name: 'P5 restart retained', type: 'scan', cron: '0 3 * * *', enabled: false, params: { library_id: p5.libraryId } } });
  try {
    p5.mode.delay = 8000; await login(page, live); await page.locator('#sc-lib').selectOption(String(p5.libraryId)); await page.locator('#sc-only-missing').uncheck();
    const before = await disk(p5.movies[0]!.directory); await page.locator('#scrape-start').click(); await expect(page.locator('#scrape-progress-title')).toContainText('正在刮削');
    await live.restart();
    await expect(page.locator('#scrape-progress-detail')).toContainText('服务可能已重启'); await expect(page.locator('#scrape-start')).toBeEnabled();
    const result = await live.request('/api/admin/scrape/progress'); expect(result.running).toBe(false); expect(result.started_at).toBeUndefined();
    expect(await disk(p5.movies[0]!.directory)).toEqual(before); const saved = await live.request('/api/admin/scrape/settings'); expect(saved.metatube_url).toBe(p5.settings.metatube_url);
    expect((await live.request('/api/admin/scheduled')).items.some((item: { id: number }) => item.id === task.id)).toBe(true);
    await page.reload(); await expect(page.locator('#scrape-save')).toBeVisible(); await expect(page.locator('#scrape-start')).toBeEnabled();
  } finally { await live.request(`/api/admin/scheduled/${task.id}`, { method: 'DELETE', expected: 204 }); }
});
