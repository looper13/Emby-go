import type { BrowserContext, Page } from '@playwright/test';
import { writeFile } from 'node:fs/promises';
import { test, expect } from './fixtures';
import type { OfflineState, OfflineTask } from '../src/api/cloud115';

const libraryId = 9002, videoId = 'guangya-fixture';
const detailPath = `/api/r18/libraries/${libraryId}/videos/${videoId}`;
const hash = 'a'.repeat(40), magnet = `magnet:?xt=urn:btih:${hash}`;
const resourceId = `btih-${hash}`;
const cloudStreamPath = `/Videos/r18-${libraryId}-${videoId}/stream`;
const source = { Id: 'guangya-task', InfoHash: hash, ResourceId: resourceId, Name: '光鸭隔离正片.mp4', Container: 'mp4', Default: false,
  DirectStreamUrl: `${cloudStreamPath}?MediaSourceId=guangya-task&playback_token=guangya-fixture-ticket` };
const localSource = { id: 'strm-70003', item_id: '70003', name: '本地隔离正片.strm', library_name: '本地隔离资料库', container: 'mp4' };
const task: OfflineTask = { id: 'task', resource_id: resourceId, magnet, info_hash: hash, status: 'downloading', progress: 40, stage: '', error: '', retryable: false, version: 1, files: [], file_id: '', expires_at: Math.floor(Date.now() / 1000) + 86400 };
const files = [{ id: 'attachment', name: '附件.mp4', path: '附件.mp4', size: 100 }, { id: 'main', name: source.Name, path: source.Name, size: 1024 ** 3 }];
const video = { id: videoId, content_id: 'guangya-fixture', service_code: 'javdb', number: 'TEST-001', title: '光鸭网盘隔离播放测试', original_title: '', release_date: '2026-01-02', cover_url: '', images: [], duration: 120, score: 8, magnets_count: 1, has_local: false, tags: ['隔离测试'], has_preview: false, preview_url: '' };
type Live = { baseURL: string; mediaURL: string; credentials: { username: string; password: string }; addSecret: (value: string) => void };

async function noHorizontalOverflow(page: Page) {
  await expect.poll(() => page.evaluate(() => ({ document: document.documentElement.scrollWidth <= innerWidth, body: document.body.scrollWidth <= innerWidth }))).toEqual({ document: true, body: true });
}

async function mockCloud(context: BrowserContext, page: Page, live: Live) {
  const errors: string[] = [], externalRequests: string[] = [], unexpected: string[] = [];
  const offlineRequests: { provider: string; method: string; action: string }[] = [];
  const saved: Record<string, unknown>[] = [], selections: Record<string, unknown>[] = [], prepares: string[] = [], redirects: string[] = [];
  let configured = false, revision = 0, submitted = false, bound = false, localAvailable = false;
  page.on('pageerror', error => errors.push(error.message));
  page.on('request', request => { const origin = new URL(request.url()).origin; if (![live.baseURL, live.mediaURL].includes(origin)) externalRequests.push('请求离开隔离实例'); });
  function state(): OfflineState {
    return { configured, tasks: submitted ? [{ ...task, ...(bound ? { status: 'ready', progress: 100, files, file_id: 'main' } : {}) }] : [], sources: bound ? [source] : [] };
  }
  function counts() { return { '115': offlineRequests.filter(r => r.provider === '115').length, guangya: offlineRequests.filter(r => r.provider === 'guangya').length }; }

  // 所有网盘接口仅在浏览器内模拟，真实媒体请求只访问本次临时服务。
  await context.route(`${live.baseURL}/api/**`, async route => {
    const request = route.request(), url = new URL(request.url()), method = request.method();
    const handled = url.pathname === '/api/admin/guangya/connection' || url.pathname.startsWith('/api/r18/');
    if (handled && !request.headers()['x-emby-token']) unexpected.push('鉴权接口缺少登录凭证');
    if (url.pathname === '/api/admin/guangya/connection') {
      if (method === 'GET') return route.fulfill({ json: { configured, account_id: configured ? 'fixture-account' : '', folder_id: configured ? 'fixture-folder' : '', folder_name: '缓存-24h自动删除', revision } });
      if (method === 'PUT') {
        saved.push(request.postDataJSON() as Record<string, unknown>); configured = true; revision++;
        return route.fulfill({ status: 204 });
      }
    } else if (url.pathname === '/api/r18/libraries') {
      return route.fulfill({ json: { items: [{ Id: libraryId, Name: '光鸭隔离 JavDB 资料库', Path: '', SourceType: 'avdb_javdb', Revision: 1 }], total: 1 } });
    } else if (url.pathname === detailPath) {
      return route.fulfill({ json: { video: { ...video, has_local: localAvailable }, library_name: '光鸭隔离 JavDB 资料库', playable: localAvailable, local_sources: localAvailable ? [localSource] : [] } });
    } else if (url.pathname === `${detailPath}/resources`) {
      return route.fulfill({ json: { items: [{ id: resourceId, title: '光鸭磁力测试版本', number: video.number, date: '', magnet, website: 'JavDB', detail_url: '', size_bytes: 1024 ** 3, size_mb: 0, tags: ['HD'], origins: ['online'] }], has_more: false, next_offset: 1 } });
    } else if (url.pathname === `${detailPath}/local-playback`) {
      if (!localAvailable || url.searchParams.get('source_id') !== localSource.id) unexpected.push('本地片源复查不符合契约');
      return route.fulfill({ json: { source: localSource } });
    } else if (url.pathname.startsWith(`${detailPath}/offline`)) {
      const provider = url.pathname.startsWith(`${detailPath}/offline/guangya`) ? 'guangya' : '115';
      const base = `${detailPath}/offline${provider === 'guangya' ? '/guangya' : ''}`;
      const action = url.pathname.slice(base.length);
      offlineRequests.push({ provider, method, action });
      if (provider === '115' && method === 'GET' && action === '') return route.fulfill({ json: { configured: true, tasks: [], sources: [] } });
      if (provider === 'guangya' && action === '' && method === 'GET') {
        // 本次首次下载已完成，下一次本地状态读取发现两份视频，验证自动选择最大正片。
        const next = state(); if (submitted && !bound) next.tasks = [{ ...task, status: 'waiting_selection', progress: 100, files }];
        return route.fulfill({ json: next });
      }
      if (provider === 'guangya' && action === '' && method === 'POST') {
        if (request.postDataJSON().resource_id !== resourceId) unexpected.push('提交的磁力版本不正确');
        submitted = true; return route.fulfill({ json: state() });
      }
      if (provider === 'guangya' && action === '/selection' && method === 'POST') {
        selections.push(request.postDataJSON() as Record<string, unknown>); bound = true;
        return route.fulfill({ json: state() });
      }
      if (provider === 'guangya' && action === '/prepare' && method === 'GET') {
        prepares.push(url.searchParams.get('source_id') || ''); return route.fulfill({ json: state() });
      }
      unexpected.push(`${provider} 出现非预期离线操作 ${method} ${action}`); return route.fulfill({ status: 404, json: { error: '隔离夹具拒绝此操作' } });
    }
    if (handled) { unexpected.push(`${method} ${url.pathname}`); return route.fulfill({ status: 404 }); }
    return route.fallback();
  });
  await context.route(`${live.baseURL}/Videos/**`, async route => {
    const url = new URL(route.request().url());
    if (url.pathname === cloudStreamPath && url.searchParams.get('MediaSourceId') === source.Id) redirects.push('guangya');
    else if ([`/Videos/${localSource.item_id}/stream`, `/Videos/${localSource.item_id}/proxy`].includes(url.pathname)) redirects.push('local');
    else { unexpected.push('出现未知的视频播放入口'); return route.fulfill({ status: 404 }); }
    return route.fulfill({ status: 302, headers: { Location: `${live.mediaURL}/sample.mp4`, 'Cache-Control': 'no-store' } });
  });
  await context.route(`${live.baseURL}/Items/**`, async route => {
    if (new URL(route.request().url()).pathname !== `/Items/${localSource.item_id}/PlaybackInfo`) { unexpected.push('出现未知本地播放信息请求'); return route.fulfill({ status: 404 }); }
    return route.fulfill({ json: { MediaSources: [{ Id: `mediasource_${localSource.item_id}`, Container: 'mp4', MediaStreams: [{ Index: 0, Type: 'Video' }], DefaultAudioStreamIndex: -1, DefaultSubtitleStreamIndex: -1 }] } });
  });
  await context.route(`${live.baseURL}/Sessions/Playing/**`, route => route.fulfill({ status: 204 }));
  await context.route(`${live.baseURL}/Users/*/PlayedItems/**`, route => route.fulfill({ status: 204 }));
  return { errors, externalRequests, unexpected, offlineRequests, saved, selections, prepares, redirects, counts, enableLocal: () => { localAvailable = true; } };
}

async function expectActualVideo(page: Page) {
  await expect(page.locator('#player-overlay')).toBeVisible();
  const element = page.locator('#player-stage video');
  await expect(element).toBeVisible();
  await expect.poll(() => element.evaluate(video => (video as HTMLVideoElement).currentTime)).toBeGreaterThan(0);
  const frame = await element.evaluate(video => ({ width: (video as HTMLVideoElement).videoWidth, height: (video as HTMLVideoElement).videoHeight, time: (video as HTMLVideoElement).currentTime }));
  expect(frame.width).toBeGreaterThan(0); expect(frame.height).toBeGreaterThan(0); await noHorizontalOverflow(page);
  return frame;
}

test('光鸭设置、真实样本播放与网盘切换在桌面和手机保持隔离', async ({ page, context, live }, info) => {
  test.setTimeout(60_000);
  const mock = await mockCloud(context, page, live);
  const credentials = { access_token: 'guangya-isolated-access', refresh_token: 'guangya-isolated-refresh', device_id: '0123456789abcdef0123456789abcdef' };
  for (const value of Object.values(credentials)) live.addSecret(value);
  await page.goto(`${live.baseURL}/admin#/settings`);
  await page.locator('#auth-user').fill(live.credentials.username); await page.locator('#auth-pw').fill(live.credentials.password);
  await page.locator('#auth-form button[type=submit]').click();
  await expect(page.locator('.cloud-guangya-settings')).toBeVisible();
  await page.locator('#guangya-access-token').fill(credentials.access_token);
  await page.locator('#guangya-refresh-token').fill(credentials.refresh_token);
  await page.locator('#guangya-device-id').fill(credentials.device_id);
  await page.locator('.cloud-guangya-settings button[type=submit], .cloud-guangya-settings form button').click();
  await expect(page.locator('.cloud-guangya-settings [role=status]')).toContainText('连接已验证并保存');
  expect(mock.saved).toEqual([{ ...credentials, revision: 0 }]);
  for (const id of ['guangya-access-token', 'guangya-refresh-token', 'guangya-device-id']) { await expect(page.locator(`#${id}`)).toHaveValue(''); await expect(page.locator(`#${id}`)).toHaveAttribute('type', 'password'); }
  await noHorizontalOverflow(page);
  await page.locator('.cloud-guangya-settings').screenshot({ path: info.outputPath('guangya-settings.png'), animations: 'disabled', mask: [page.locator('.cloud-guangya-settings input')] });

  await page.goto(`${live.baseURL}/admin#/r18/${libraryId}/item/${videoId}`);
  await expect(page.locator('#r18-cloud-provider')).toHaveValue('115');
  await expect.poll(() => mock.counts()).toEqual({ '115': 1, guangya: 0 });
  await page.locator('#r18-cloud-provider').selectOption('guangya');
  await expect(page.locator('.offline-panel h2')).toHaveText('光鸭 播放源');
  await expect(page.locator('#detail-play')).toBeEnabled();
  await page.locator('#detail-play').click();
  const initialFrame = await expectActualVideo(page);
  expect(mock.selections).toEqual([{ task_id: 'task', file_id: 'main', default: false, prepare: true }]);
  expect(mock.offlineRequests.filter(request => request.method === 'POST')).toEqual([{ provider: 'guangya', method: 'POST', action: '' }, { provider: 'guangya', method: 'POST', action: '/selection' }]);
  await page.screenshot({ path: info.outputPath('guangya-playing.png'), animations: 'disabled' });
  await page.locator('#player-close').click(); await expect(page.locator('#player-overlay')).toHaveCount(0);
  await page.locator('#detail-play').click();
  await expect.poll(() => mock.prepares).toEqual([source.Id]);
  const preparedFrame = await expectActualVideo(page);
  await page.locator('#player-close').click(); await expect(page.locator('#player-overlay')).toHaveCount(0);

  const beforeSwitch = mock.counts();
  await page.locator('#r18-cloud-provider').selectOption('115');
  await expect(page.locator('.offline-panel h2')).toHaveText('115 播放源');
  await expect.poll(() => mock.counts()).toEqual({ '115': beforeSwitch['115'] + 1, guangya: beforeSwitch.guangya });
  // 就绪缓存有五秒轮询，切换后必须停止上一网盘的计时器。
  await page.waitForTimeout(5200); expect(mock.counts()).toEqual({ '115': beforeSwitch['115'] + 1, guangya: beforeSwitch.guangya });
  await noHorizontalOverflow(page);

  mock.enableLocal(); const beforeLocal = mock.counts(); await page.reload();
  await expect(page.locator('#detail-play')).toHaveText('播放本地 STRM');
  await expect(page.locator('.offline-panel')).toHaveCount(0); expect(mock.counts()).toEqual(beforeLocal);
  await page.locator('#r18-use-magnets').click(); await expect(page.locator('#r18-cloud-provider')).toBeVisible();
  await page.locator('#r18-cloud-provider').selectOption('guangya'); await expect(page.locator('.offline-panel h2')).toHaveText('光鸭 播放源');
  await expect(page.locator('#r18-cloud-source')).toBeVisible(); await expect(page.locator('.offline-panel')).toHaveAttribute('aria-busy', 'false');
  const beforeReturn = mock.counts(); await page.locator('#r18-use-local').click();
  await expect(page.locator('.offline-panel')).toHaveCount(0); await expect(page.locator('#detail-play')).toHaveText('播放本地 STRM');
  await page.waitForTimeout(5200); expect(mock.counts()).toEqual(beforeReturn);
  await page.locator('#detail-play').click(); const localFrame = await expectActualVideo(page);
  expect(mock.counts()).toEqual(beforeReturn); await page.locator('#player-close').click();
  await noHorizontalOverflow(page);
  await page.screenshot({ path: info.outputPath('guangya-local-return.png'), fullPage: true, animations: 'disabled' });
  expect(mock.redirects.filter(provider => provider === 'guangya').length).toBeGreaterThanOrEqual(2); expect(mock.redirects).toContain('local');
  expect(mock.errors).toEqual([]); expect(mock.unexpected).toEqual([]); expect(mock.externalRequests).toEqual([]);
  await writeFile(info.outputPath('guangya-verification.json'), JSON.stringify({ project: info.project.name, viewport: page.viewportSize(), initialFrame, preparedFrame, localFrame, offlineRequests: mock.offlineRequests, redirectProviders: mock.redirects, savedFields: Object.keys(mock.saved[0] || {}), wrongProviderRequests: mock.unexpected, externalRequests: mock.externalRequests, errors: mock.errors, horizontalOverflow: false }, null, 2));
});
