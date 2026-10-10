import type { Page } from '@playwright/test';
import { writeFile } from 'node:fs/promises';
import { test, expect } from './fixtures';

async function login(page: Page, live: { baseURL: string; credentials: { username: string; password: string } }, target = '/items') {
  await page.goto(`${live.baseURL}/admin#${target}`);
  await page.locator('#auth-user').fill(live.credentials.username);
  await page.locator('#auth-pw').fill(live.credentials.password);
  await page.locator('#auth-form button[type=submit]').click();
  await expect(page).toHaveURL(`${live.baseURL}/admin#${target}`);
}

test('P7 large library paginates 1000 records with unique requests and reserved poster sizes', async ({ page, live }, info) => {
  test.skip(Number(process.env.EMBY_E2E_MEDIA_COUNT || 307) < 7000, 'Run with EMBY_E2E_MEDIA_COUNT=8000');
  test.setTimeout(180_000);
  const reads: string[] = [];
  page.on('request', r => { if (new URL(r.url()).pathname === '/api/admin/items') reads.push(new URL(r.url()).search); });
  await page.addInitScript(() => {
    const durations: number[] = [];
    Object.assign(window, { p7LongTasks: durations });
    new PerformanceObserver(list => { for (const entry of list.getEntries()) durations.push(entry.duration); }).observe({ type: 'longtask', buffered: true });
  });
  const cdp = await page.context().newCDPSession(page);
  await cdp.send('Performance.enable');
  const samples = [];
  await login(page, live);
  for (let count = 100; count <= 1000; count += 100) {
    await expect(page.locator('.wall-card')).toHaveCount(count);
    await cdp.send('HeapProfiler.collectGarbage');
    const { metrics } = await cdp.send('Performance.getMetrics');
    samples.push({ count, metrics: Object.fromEntries(metrics.filter(m => ['JSHeapUsedSize', 'Nodes', 'JSEventListeners', 'TaskDuration'].includes(m.name)).map(m => [m.name, m.value])) });
    if (count < 1000) await page.locator('.wall-card').nth(count - 1).scrollIntoViewIfNeeded();
  }
  expect(reads).toHaveLength(10);
  expect(new Set(reads).size).toBe(10);
  expect(reads.map(q => new URLSearchParams(q).get('offset'))).toEqual(Array.from({ length: 10 }, (_, i) => String(i * 100)));
  expect(await page.locator('.wall-card').evaluateAll(cards => new Set(cards.map(c => (c as HTMLElement).dataset.play)).size)).toBe(1000);
  const images = await page.locator('.wall-poster img').evaluateAll(imgs => imgs.map(img => {
    const container = img.parentElement!;
    const rect = container.getBoundingClientRect();
    return { loading: (img as HTMLImageElement).loading, width: rect.width, height: rect.height, ratio: getComputedStyle(container).aspectRatio };
  }));
  expect(images.length).toBeGreaterThan(0);
  for (const image of images) { expect(image.loading).toBe('lazy'); expect(image.width).toBeGreaterThan(0); expect(image.height).toBeGreaterThan(0); expect(image.ratio).not.toBe('auto'); }
  const longTasks = await page.evaluate(() => (window as unknown as { p7LongTasks: number[] }).p7LongTasks);
  const evidence = info.outputPath('large-library.json');
  await writeFile(evidence, JSON.stringify({ databaseCount: live.status!.success + live.status!.pending, reads, images, samples, longTasks }, null, 2));
  await info.attach('large-library-observations', { path: evidence, contentType: 'application/json' });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('P7 50 navigations and 20 real player sessions return global resources to baseline', async ({ page, live }, info) => {
  test.setTimeout(180_000);
  await page.addInitScript(() => {
    const timers = new Set<number>(), intervals = new Set<number>();
    const timeout = window.setTimeout.bind(window), clear = window.clearTimeout.bind(window);
    window.setTimeout = ((callback: TimerHandler, delay?: number, ...args: unknown[]) => {
      const id = timeout(() => { timers.delete(id); if (typeof callback === 'function') callback(...args); }, delay);
      timers.add(id); return id;
    }) as typeof window.setTimeout;
    window.clearTimeout = (id?: number) => { timers.delete(id!); clear(id); };
    const interval = window.setInterval.bind(window), clearInterval = window.clearInterval.bind(window);
    window.setInterval = ((callback: TimerHandler, delay?: number, ...args: unknown[]) => { const id = interval(callback, delay, ...args); intervals.add(id); return id; }) as typeof window.setInterval;
    window.clearInterval = (id?: number) => { intervals.delete(id!); clearInterval(id); };
    const listeners: { target: EventTarget; type: string; callback: EventListenerOrEventListenerObject; capture: boolean }[] = [];
    const add = EventTarget.prototype.addEventListener, remove = EventTarget.prototype.removeEventListener;
    EventTarget.prototype.addEventListener = function(type, callback, options) {
      const capture = typeof options === 'boolean' ? options : Boolean(options?.capture);
      if (callback && (this === window || this === document) && !listeners.some(l => l.target === this && l.type === type && l.callback === callback && l.capture === capture)) listeners.push({ target: this, type, callback, capture });
      return add.call(this, type, callback, options);
    };
    EventTarget.prototype.removeEventListener = function(type, callback, options) {
      const capture = typeof options === 'boolean' ? options : Boolean(options?.capture);
      const index = listeners.findIndex(l => l.target === this && l.type === type && l.callback === callback && l.capture === capture);
      if (index >= 0) listeners.splice(index, 1);
      return remove.call(this, type, callback, options);
    };
    Object.assign(window, { p7Resources: () => ({ timers: timers.size, intervals: intervals.size, listeners: listeners.map(l => `${l.target === window ? 'window' : 'document'}:${l.type}:${l.capture}`).sort() }) });
  });
  const resourceState = () => page.evaluate(() => (window as unknown as { p7Resources: () => object }).p7Resources());
  const cdp = await page.context().newCDPSession(page); await cdp.send('Performance.enable');
  const samples: object[] = [];
  async function sample(stage: string) {
    await cdp.send('HeapProfiler.collectGarbage');
    const { metrics } = await cdp.send('Performance.getMetrics');
    samples.push({ stage, resources: await resourceState(), metrics: Object.fromEntries(metrics.filter(m => ['JSHeapUsedSize', 'Nodes', 'JSEventListeners', 'Documents'].includes(m.name)).map(m => [m.name, m.value])) });
  }
  await login(page, live, '/items?search=ABF-005');
  await expect(page.locator('.wall-card')).toHaveCount(1);
  // Warm third-party code before taking a baseline; module caches are intentional.
  await page.locator('[data-quickplay]').click(); await expect(page.locator('#player-stage video')).toBeVisible();
  await expect.poll(() => page.locator('#player-stage video').evaluate((v: HTMLVideoElement) => v.readyState >= 2)).toBe(true);
  await page.locator('#player-close').click();
  await expect.poll(async () => (await resourceState() as { timers: number }).timers).toBe(0);
  const baseline = await resourceState(); await sample('warm');
  for (let cycle = 1; cycle <= 25; cycle++) {
    await page.locator('[data-page=overview]').click(); await expect(page.locator('#page-title')).toHaveText('总览');
    // Check GL-09 while the wall is absent, not just after returning to it.
    const offWall = await resourceState() as { listeners: string[] };
    expect(offWall.listeners.filter(l => l === 'window:scroll:false')).toHaveLength(0);
    await page.locator('[data-page=items]').click(); await expect(page.locator('.wall-card')).toHaveCount(100);
    if (cycle % 5 === 0) await sample(`navigation-${cycle * 2}`);
  }
  // Use a fixed small list so retained list size cannot mask a player leak.
  await page.locator('#item-search').fill('ABF-005'); await expect(page.locator('.wall-card')).toHaveCount(1);
  for (let cycle = 1; cycle <= 20; cycle++) {
    await page.locator('[data-quickplay]').click();
    await expect.poll(() => page.locator('#player-stage video').evaluate((v: HTMLVideoElement) => v.readyState >= 2)).toBe(true);
    await page.locator('#player-close').click(); await expect(page.locator('video, #player-overlay')).toHaveCount(0);
    await expect.poll(resourceState).toEqual(baseline);
    expect(await page.evaluate(() => document.body.classList.contains('player-open'))).toBe(false);
    if (cycle % 5 === 0) await sample(`player-${cycle}`);
  }
  const evidence = info.outputPath('resource-trend.json');
  await writeFile(evidence, JSON.stringify({ navigations: 50, playerSessions: 20, baseline, samples }, null, 2));
  await info.attach('resource-trend', { path: evidence, contentType: 'application/json' });
});

test('P7 missing player chunk prompts refresh and preserves current input', async ({ page, live }) => {
  await login(page, live, '/items?search=ABF-005'); await expect(page.locator('.wall-card')).toHaveCount(1);
  let release!: () => void;
  let started!: () => void;
  const pending = new Promise<void>(resolve => { started = resolve; });
  await page.route('**/web/ui/assets/artplayer*.js', async route => { started(); await new Promise<void>(resolve => { release = resolve; }); await route.fulfill({ status: 404, body: '' }); });
  await page.locator('[data-quickplay]').click(); await pending;
  await page.locator('#item-search').fill('unsaved input'); release();
  await expect(page.getByRole('alert')).toContainText('页面资源已更新');
  await expect(page.locator('#item-search')).toHaveValue('unsaved input');
  await expect(page.getByRole('button', { name: '刷新页面', exact: true })).toBeVisible();
  await expect(page.locator('#player-overlay')).toHaveCount(0);
});
