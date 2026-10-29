import type { BrowserContext, Page } from '@playwright/test';
import { execFile } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';
import { test, expect } from './fixtures';
import type { R18LocalSource } from '../src/api/r18';

const libraryId = 9001;
const videoId = 'WyJmaXh0dXJlMDAxIiwiamF2ZGIiXQ';
const number = 'TEST-001';
const imageBase = `/api/r18/libraries/${libraryId}/videos/${videoId}/images/`;
const magnetHash = 'a'.repeat(40);
const magnet = `magnet:?xt=urn:btih:${magnetHash}`;
const apiAddress = 'https://avdb.fixture.invalid';
const poster = '<svg xmlns="http://www.w3.org/2000/svg" width="600" height="900" viewBox="0 0 600 900"><rect width="600" height="900" fill="#183e4a"/><rect x="60" y="120" width="480" height="660" rx="24" fill="#cfb87c"/></svg>';
const videosPath = `/api/r18/libraries/${libraryId}/videos`;
const detailPath = `${videosPath}/${videoId}`;
const previewPath = `${detailPath}/preview.m3u8`;
const previewSegmentPath = `${detailPath}/preview/e2e-session/segment-0`;
const connectionPath = `/api/admin/r18/libraries/${libraryId}`;
const samplePath = fileURLToPath(new URL('../../internal/server/testdata/probe-sample.mp4', import.meta.url));
const video = {
  id: videoId, content_id: 'fixture001', service_code: 'javdb', number,
  title: 'JavDB 在线测试影片', original_title: '在线资源隔离夹具', release_date: '2026-01-02',
  cover_url: imageBase + 'poster', images: [0, 1, 2].map(index => imageBase + index),
  duration: 120, score: 8.6, magnets_count: 13, has_local: false,
  tags: ['隔离剧情', '高清样本', '测试标签'], has_preview: true, preview_url: previewPath,
};
const localSources: R18LocalSource[] = [
  { id: 'strm-70001', item_id: '70001', name: '本地高清测试版本.strm', library_name: '隔离本地资料库', container: 'mp4' },
  { id: 'strm-70002', item_id: '70002', name: '本地第二测试版本.strm', library_name: '另一隔离资料库', container: 'mp4' },
];
type CatalogQuery = Record<string, string>;
type LoginEnvironment = { baseURL: string; credentials: { username: string; password: string } };
type JavDBMockOptions = { imageFailures?: number; singleItem?: boolean; noPreview?: boolean; noTags?: boolean; previewSegment?: string };

async function preparePreviewSample(output: string) {
  // 从仓库样本生成本次独立报告中的 TS 片段；不依赖外站，也不改写或清理已有样本。
  await promisify(execFile)(process.env.EMBY_E2E_FFMPEG || 'ffmpeg', [
    '-nostdin', '-v', 'error', '-stream_loop', '11', '-i', samplePath,
    '-t', '6', '-map', '0', '-c', 'copy', '-bsf:v', 'h264_mp4toannexb',
    '-f', 'mpegts', output,
  ], { windowsHide: true, timeout: 20_000 });
  return output;
}

async function assertNoHorizontalOverflow(page: Page) {
  await expect.poll(() => page.evaluate(() => ({
    document: document.documentElement.scrollWidth <= innerWidth,
    body: document.body.scrollWidth <= innerWidth,
  }))).toEqual({ document: true, body: true });
}

async function mockJavDB(context: BrowserContext, page: Page, baseURL: string, sources: R18LocalSource[] = [], options: JavDBMockOptions = {}) {
  const errors: string[] = [];
  const queries: CatalogQuery[] = [];
  const saved: Record<string, unknown>[] = [];
  const unexpectedRequests: string[] = [];
  const externalRequests: string[] = [];
  const offlineRequests: string[] = [], localRequests: string[] = [], mediaRequests: string[] = [];
  const imageRequests: string[] = [];
  const previewRequests: string[] = [], playbackRequests: string[] = [];
  const currentVideo = {
    ...video, has_local: sources.length > 0,
    tags: options.noTags ? [] : video.tags,
    has_preview: !options.noPreview, preview_url: options.noPreview ? '' : previewPath,
  };
  page.on('pageerror', error => errors.push(error.message));
  page.on('request', request => {
    const url = new URL(request.url());
    if (url.origin !== baseURL) externalRequests.push('出现非隔离服务请求');
    // 请求监听独立于 mock 分支，错误的正片调用即使返回失败也不能漏检。
    if (/^\/Items\/[^/]+\/PlaybackInfo$/u.test(url.pathname) || url.pathname.startsWith('/Videos/') ||
      url.pathname.startsWith('/Sessions/Playing') || /^\/Users\/[^/]+\/PlayedItems(?:\/|$)/u.test(url.pathname)) {
      playbackRequests.push(`${request.method()} ${url.pathname}`);
    }
  });
  let library = { Id: libraryId, Name: 'JavDB 在线测试资料库', Path: '', SourceType: 'avdb_javdb', Revision: 7, Type: 'movies' };

  // 只模拟隔离服务器的浏览器请求，不访问 AVdb、JavDB 或真实 115 服务。
  await context.route(`${baseURL}/api/**`, async route => {
    const request = route.request();
    const url = new URL(request.url());
    if (url.pathname === previewPath || url.pathname === previewSegmentPath) {
      previewRequests.push(url.pathname + url.search);
      if (options.noPreview || !options.previewSegment || request.method() !== 'GET' || url.search) {
        unexpectedRequests.push('预览请求不符合隔离夹具契约');
        await route.fulfill({ status: 404 });
      } else if (url.pathname === previewPath) {
        const manifest = ['#EXTM3U', '#EXT-X-VERSION:3', '#EXT-X-TARGETDURATION:6', '#EXT-X-MEDIA-SEQUENCE:0',
          '#EXT-X-PLAYLIST-TYPE:VOD', '#EXTINF:6.000,', previewSegmentPath, '#EXT-X-ENDLIST', ''].join('\n');
        await route.fulfill({ contentType: 'application/vnd.apple.mpegurl', body: manifest, headers: { 'Cache-Control': 'no-store' } });
      } else await route.fulfill({ path: options.previewSegment, contentType: 'video/mp2t', headers: { 'Cache-Control': 'no-store' } });
    } else if (url.pathname.startsWith(imageBase)) {
      imageRequests.push(url.pathname + url.search);
      // 单卡片夹具让同 URL 的失败次数可精确验证，不依赖浏览器合并多个图片请求。
      if (url.pathname === imageBase + 'poster' && imageRequests.length <= (options.imageFailures || 0)) {
        await route.fulfill({ status: 503, contentType: 'text/plain', body: '隔离测试图片短暂不可用', headers: { 'Cache-Control': 'no-store' } });
      } else await route.fulfill({ contentType: 'image/svg+xml', body: poster, headers: { 'Cache-Control': 'no-store' } });
    } else if (url.pathname === '/api/admin/libraries' || url.pathname === '/api/r18/libraries') {
      await route.fulfill({ json: { items: [library], total: 1 } });
    } else if (url.pathname === videosPath) {
      const query = Object.fromEntries(url.searchParams);
      queries.push(query);
      // 路由保留 page_size，但 HTTP 必须使用 limit，禁止夹具掩盖真实契约错误。
      if (url.searchParams.has('page_size') || !['24', '40', '50'].includes(query.limit || '')) {
        unexpectedRequests.push('列表请求缺少有效 limit，或错误发送 page_size');
        await route.fulfill({ status: 400, json: { error: '分页参数不正确' } });
        return;
      }
      const currentPage = Number(query.page), pageSize = Number(query.limit);
      const items = Array.from({ length: options.singleItem ? 1 : pageSize }, (_, index) => index === 0 ? currentVideo : {
        ...currentVideo,
        id: Buffer.from(JSON.stringify([`fixture-${currentPage}-${index}`, 'javdb'])).toString('base64url'),
        content_id: `fixture-${currentPage}-${index}`,
        number: `TEST-${String(index + 1).padStart(3, '0')}`,
        title: `在线分页测试影片 ${currentPage}-${index + 1}`,
      });
      await route.fulfill({ json: { items, page: currentPage, page_size: pageSize, has_more: currentPage < 9999, has_previous: currentPage > 1 } });
    } else if (url.pathname === detailPath) {
      await route.fulfill({ json: { video: currentVideo, library_name: library.Name, playable: sources.length > 0, local_sources: sources } });
    } else if (url.pathname === `${detailPath}/local-playback`) {
      const sourceId = url.searchParams.get('source_id') || '';
      localRequests.push(sourceId);
      const source = sources.find(value => value.id === sourceId);
      await route.fulfill({ status: source ? 200 : 404, json: source ? { source } : { error: '本地版本不可用' } });
    } else if (url.pathname === `${detailPath}/resources`) {
      await route.fulfill({ json: { items: [{ id: `btih-${magnetHash}`, title: 'JavDB 在线磁力测试版本', number, date: '2026-01-03', magnet, website: 'JavDB 在线', detail_url: '', size_bytes: 1073741824, size_mb: 0, tags: ['HD', '中文字幕'], origins: ['online', 'library'] }], has_more: false, next_offset: 1 } });
    } else if (url.pathname === `${detailPath}/offline` && request.method() === 'GET') {
      offlineRequests.push(`${request.method()} ${url.pathname}`);
      await route.fulfill({ json: { configured: false, tasks: [], sources: [] } });
    } else if (url.pathname === connectionPath && request.method() === 'GET') {
      // 响应只有已配置状态，不提供任何可回显的 API Key。
      await route.fulfill({ json: { library, connection: { base_url: apiAddress, has_api_key: true } } });
    } else if (url.pathname === connectionPath && request.method() === 'PUT') {
      saved.push(request.postDataJSON() as Record<string, unknown>);
      library = { ...library, Revision: library.Revision + 1 };
      await route.fulfill({ json: library });
    } else if (url.pathname.startsWith('/api/r18/') || url.pathname.startsWith('/api/admin/r18/')) {
      if (url.pathname.includes('/offline')) offlineRequests.push(`${request.method()} ${url.pathname}`);
      unexpectedRequests.push(`${request.method()} ${url.pathname}`);
      await route.fulfill({ status: 404, json: { error: '隔离夹具不允许此请求' } });
    } else {
      await route.fallback();
    }
  });
  if (sources.length) {
    // 普通播放接口也限制在隔离服务内，视频使用仓库已有的小型样本，不能访问外站。
    await context.route(`${baseURL}/Items/**`, async route => {
      const pathname = new URL(route.request().url()).pathname;
      const source = sources.find(value => pathname === `/Items/${value.item_id}/PlaybackInfo`);
      if (!source) { unexpectedRequests.push(pathname); await route.fulfill({ status: 404 }); return; }
      await route.fulfill({ json: { MediaSources: [{ Id: `mediasource_${source.item_id}`, Container: source.container, MediaStreams: [{ Index: 0, Type: 'Video' }], DefaultAudioStreamIndex: -1, DefaultSubtitleStreamIndex: -1 }] } });
    });
    await context.route(`${baseURL}/Videos/**`, async route => {
      const pathname = new URL(route.request().url()).pathname;
      if (!sources.some(source => [`/Videos/${source.item_id}/stream`, `/Videos/${source.item_id}/proxy`].includes(pathname))) {
        unexpectedRequests.push(pathname); await route.fulfill({ status: 404 }); return;
      }
      mediaRequests.push(pathname);
      await route.fulfill({ path: samplePath, contentType: 'video/mp4' });
    });
    await context.route(`${baseURL}/Sessions/Playing/**`, route => route.fulfill({ status: 204 }));
    await context.route(`${baseURL}/Users/*/PlayedItems/**`, route => route.fulfill({ status: 204 }));
  }
  return { errors, queries, saved, unexpectedRequests, externalRequests, offlineRequests, localRequests, mediaRequests, imageRequests, previewRequests, playbackRequests };
}
type JavDBMock = Awaited<ReturnType<typeof mockJavDB>>;

async function login(page: Page, live: LoginEnvironment, query = `library_id=${libraryId}`) {
  await page.goto(`${live.baseURL}/admin#/r18?${query}`);
  await expect(page.locator('#auth-form')).toBeVisible();
  await page.locator('#auth-user').fill(live.credentials.username);
  await page.locator('#auth-pw').fill(live.credentials.password);
  await page.locator('#auth-form button[type=submit]').click();
  await expect(page.locator('.r18-catalog')).toBeVisible();
}

async function expectCatalog(page: Page, mock: JavDBMock, query: CatalogQuery) {
  await expect.poll(() => mock.queries.at(-1)).toEqual(query);
  await expect(page.locator('.r18-grid')).toHaveAttribute('aria-busy', 'false');
  await expect(page.locator('.r18-list-meta')).toContainText(`第 ${query.page} 页 · 本页 ${query.limit} 条`);
  await expect(page.locator('.r18-card')).toHaveCount(Number(query.limit));
  await expect(page.locator('#r18-page-size')).toHaveValue(query.limit!);
  await expect(page.locator('#r18-jump-page')).toHaveValue(query.page!);
  await expect(page.locator('.r18-list-meta')).not.toContainText(/上游总数|源表|约\s*\d|共\s*\d+\s*条|估算/u);
}

async function jump(page: Page, mock: JavDBMock, query: CatalogQuery, target = '7') {
  await page.locator('#r18-jump-page').fill(target);
  await page.locator('.r18-page-jump').getByRole('button', { name: '跳转', exact: true }).click();
  await expectCatalog(page, mock, { ...query, page: target });
}

function expectSafeMock(mock: JavDBMock) {
  expect(mock.errors).toEqual([]);
  expect(mock.unexpectedRequests).toEqual([]);
  expect(mock.externalRequests).toEqual([]);
}

function expectPreviewIsolation(mock: JavDBMock) {
  expect(mock.offlineRequests).toEqual([]);
  expect(mock.localRequests).toEqual([]);
  expect(mock.mediaRequests).toEqual([]);
  expect(mock.playbackRequests).toEqual([]);
  expectSafeMock(mock);
}

test('JavDB 页码、每页数量与分类筛选排序变动保持一致', async ({ page, context, live }, info) => {
  test.setTimeout(60_000);
  const mock = await mockJavDB(context, page, live.baseURL);
  let query: CatalogQuery = { type: 'all', filter: 'magnets', sort: 'release', order: 'desc', page: '1', limit: '24' };
  await login(page, live);
  await expectCatalog(page, mock, query);
  expect(page.viewportSize()?.width).toBe(info.project.name === 'mobile' ? 390 : 1280);
  const firstCard = page.locator('.r18-card').first();
  await expect(firstCard).toContainText(number);
  await expect(firstCard).toContainText('120 分钟');
  await expect(firstCard).toContainText('评分 8.6');
  await expect(firstCard).toContainText('上游磁力 13');
  await expect(firstCard.locator('img')).toBeVisible();
  await expect(page.locator('.r18-query-hint')).toContainText('不导入整库');
  await assertNoHorizontalOverflow(page);

  const pager = page.getByRole('navigation', { name: 'JavDB 分页' });
  await expect(pager.getByRole('button', { name: '上一页', exact: true })).toBeDisabled();
  await pager.getByRole('button', { name: '下一页', exact: true }).click();
  await expectCatalog(page, mock, { ...query, page: '2' });
  await expect(pager.getByRole('button', { name: '上一页', exact: true })).toBeEnabled();
  await pager.getByRole('button', { name: '上一页', exact: true }).click();
  await expectCatalog(page, mock, query);
  await jump(page, mock, query);

  for (const limit of ['40', '50']) {
    await page.locator('#r18-page-size').selectOption(limit);
    query = { ...query, limit, page: '1' };
    await expectCatalog(page, mock, query);
    await assertNoHorizontalOverflow(page);
    await jump(page, mock, query);
  }

  for (const [id, key, selected] of [
    ['r18-type', 'type', '1'], ['r18-filter', 'filter', 'subtitle'],
    ['r18-sort', 'sort', 'score'], ['r18-order', 'order', 'asc'], ['r18-year', 'year', '2024'],
  ]) {
    if (id === 'r18-year') {
      await page.locator(`#${id}`).fill(selected!);
      await page.locator(`#${id}`).press('Tab');
    } else await page.locator(`#${id}`).selectOption(selected!);
    query = { ...query, [key!]: selected!, page: '1' };
    await expectCatalog(page, mock, query);
    await expect(page.locator(`#${id}`)).toHaveValue(selected!);
    await assertNoHorizontalOverflow(page);
    await jump(page, mock, query);
  }
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.screenshot({ path: info.outputPath('javdb-catalog-filters.png'), animations: 'disabled' });
  expectSafeMock(mock);
});

test('JavDB 详情返回恢复页码筛选，关键词搜索仅按相关性查询', async ({ page, context, live }) => {
  test.setTimeout(60_000);
  const mock = await mockJavDB(context, page, live.baseURL);
  const query: CatalogQuery = { type: '2', filter: 'can_play', sort: 'watched_count', order: 'asc', year: '2023', page: '7', limit: '40' };
  const routeQuery = new URLSearchParams({ library_id: String(libraryId), ...query, page_size: query.limit! });
  routeQuery.delete('limit');
  await login(page, live, routeQuery.toString());
  await expectCatalog(page, mock, query);
  const catalogURL = page.url();
  await page.locator('.r18-card').first().click();
  await expect(page.locator('.r18-detail-info')).toContainText(video.title);
  await expect(page.locator('.r18-facts')).toContainText('120 分钟');
  await expect(page.locator('.r18-facts')).toContainText('8.6');
  await expect(page.locator('.r18-facts')).toContainText('13 个 · 实际版本见下方');
  await expect(page.locator('#detail-play')).toBeDisabled();
  await assertNoHorizontalOverflow(page);
  await page.getByRole('button', { name: '返回资料库', exact: true }).click();
  await expect(page).toHaveURL(catalogURL);
  await expectCatalog(page, mock, query);
  for (const [id, key] of [['r18-type', 'type'], ['r18-filter', 'filter'], ['r18-sort', 'sort'], ['r18-order', 'order'], ['r18-year', 'year']]) {
    await expect(page.locator(`#${id}`)).toHaveValue(query[key!]!);
  }

  await page.locator('#r18-search').fill('  TEST online  ');
  await page.locator('.r18-filters').getByRole('button', { name: '查询', exact: true }).click();
  const searchQuery: CatalogQuery = { type: '2', filter: 'can_play', search: 'TEST online', page: '1', limit: '40' };
  await expectCatalog(page, mock, searchQuery);
  for (const id of ['r18-sort', 'r18-order', 'r18-year']) await expect(page.locator(`#${id}`)).toBeDisabled();
  await expect(page.locator('.r18-query-hint')).toContainText('相关性');
  const searchParams = new URLSearchParams(new URL(page.url()).hash.split('?')[1]);
  for (const key of ['sort', 'order', 'year']) expect(searchParams.has(key)).toBe(false);
  await jump(page, mock, searchQuery);
  const searchURL = page.url();
  await page.locator('.r18-card').first().click();
  await expect(page.locator('.r18-detail-info')).toContainText(video.title);
  await page.getByRole('button', { name: '返回资料库', exact: true }).click();
  await expect(page).toHaveURL(searchURL);
  await expectCatalog(page, mock, { ...searchQuery, page: '7' });
  await expect(page.locator('#r18-search')).toHaveValue(searchQuery.search!);
  await assertNoHorizontalOverflow(page);
  expectSafeMock(mock);
});

test('JavDB 共用剧照灯箱、影片标签与留空 API Key 保存保持安全', async ({ page, context, live }, info) => {
  test.setTimeout(60_000);
  const mock = await mockJavDB(context, page, live.baseURL);
  await login(page, live);
  await expectCatalog(page, mock, { type: 'all', filter: 'magnets', sort: 'release', order: 'desc', page: '1', limit: '24' });
  await page.locator('.r18-card').first().click();
  await expect(page.locator('.r18-detail-info')).toContainText(video.title);
  await expect(page.locator('.r18-resources')).toContainText('JavDB 磁力列表');
  await expect(page.locator('.r18-magnet-text')).toHaveText(magnet);
  const thumbs = page.locator('.detail-artwork-grid button');
  await expect(thumbs).toHaveCount(3);
  await expect(page.locator('.detail-artwork-grid img')).toHaveCount(3);
  for (let index = 0; index < 3; index++) {
    await expect(thumbs.nth(index)).toHaveAttribute('aria-label', `查看剧照 ${index + 1}`);
    const image = thumbs.nth(index).locator('img');
    await image.scrollIntoViewIfNeeded();
    await expect(image).toHaveAttribute('src', imageBase + index);
    await expect.poll(() => image.evaluate(element => (element as HTMLImageElement).complete && (element as HTMLImageElement).naturalWidth > 0)).toBe(true);
  }
  const tags = page.locator('.r18-film-metadata .detail-meta-row dd');
  await expect(tags).toHaveText(video.tags.join(' '));
  await expect(page.locator('.r18-film-metadata button, .r18-film-metadata a')).toHaveCount(0);
  await expect(page.locator('#item-trailer')).toBeVisible();
  expect(mock.previewRequests).toEqual([]);

  // 同一套灯箱在桌面与手机都覆盖视口，支持键盘、点击翻页与原触发按钮焦点恢复。
  const opener = thumbs.nth(1);
  await opener.click();
  const overlay = page.locator('#artwork-overlay');
  await expect(overlay.getByRole('dialog')).toHaveAttribute('aria-modal', 'true');
  await expect(page.locator('#artwork-title')).toHaveText('剧照 2 / 3');
  await expect(page.locator('.artwork-image')).toHaveAttribute('src', imageBase + '1');
  await expect.poll(() => overlay.evaluate(element => {
    const box = element.getBoundingClientRect();
    return { left: box.left, top: box.top, width: box.width === innerWidth, height: box.height === innerHeight };
  })).toEqual({ left: 0, top: 0, width: true, height: true });
  await page.keyboard.press('ArrowRight');
  await expect(page.locator('#artwork-title')).toHaveText('剧照 3 / 3');
  await page.keyboard.press('ArrowRight');
  await expect(page.locator('#artwork-title')).toHaveText('剧照 1 / 3');
  await page.keyboard.press('ArrowLeft');
  await expect(page.locator('#artwork-title')).toHaveText('剧照 3 / 3');
  await page.locator('#artwork-prev').click();
  await expect(page.locator('#artwork-title')).toHaveText('剧照 2 / 3');
  await page.locator('#artwork-next').click();
  await expect(page.locator('#artwork-title')).toHaveText('剧照 3 / 3');
  await expect(page.locator('.artwork-image')).toHaveAttribute('src', imageBase + '2');
  await expect.poll(() => page.locator('.artwork-image').evaluate(element => (element as HTMLImageElement).naturalWidth > 0)).toBe(true);
  await page.locator('#artwork-close').focus();
  await page.keyboard.press('Shift+Tab');
  await expect(page.locator('#artwork-next')).toBeFocused();
  await page.keyboard.press('Tab');
  await expect(page.locator('#artwork-close')).toBeFocused();
  await assertNoHorizontalOverflow(page);
  await page.screenshot({ path: info.outputPath('javdb-gallery-lightbox.png'), animations: 'disabled' });
  await page.locator('#artwork-close').click();
  await expect(overlay).toHaveCount(0);
  await expect(opener).toBeFocused();
  await thumbs.first().click();
  await page.locator('#artwork-next').click();
  await page.keyboard.press('Escape');
  await expect(overlay).toHaveCount(0);
  await expect(thumbs.first()).toBeFocused();
  await assertNoHorizontalOverflow(page);
  await page.screenshot({ path: info.outputPath('javdb-detail.png'), fullPage: true, animations: 'disabled' });

  await page.goto(`${live.baseURL}/admin#/libraries`);
  const libraryHeading = page.locator('.library-manage-panel > .panel-head .panel-head-title h2');
  await expect(libraryHeading).toHaveText('媒体库');
  // 窄屏标题不能被右侧操作挤成竖排；实际高度应保持在单行范围内。
  await expect.poll(() => libraryHeading.evaluate(element => {
    const fontSize = Number.parseFloat(getComputedStyle(element).fontSize);
    return element.getBoundingClientRect().height / fontSize;
  })).toBeLessThan(2);
  await page.getByRole('button', { name: '连接配置', exact: true }).click();
  const form = page.locator('.r18-connection-form');
  await expect(form.locator('#r18-base-url')).toHaveValue(apiAddress);
  await expect(form.locator('#r18-api-key')).toHaveValue('');
  await expect(form.locator('#r18-api-key')).toHaveAttribute('type', 'password');
  await expect(form.locator('#r18-api-key')).toHaveAttribute('placeholder', '留空保留已保存 API Key');
  await expect(form).toContainText('API Key 仅保存在服务端');
  await assertNoHorizontalOverflow(page);

  // 留空保存必须省略敏感字段，不能把空字符串当成覆盖已有 Key。
  await form.getByRole('button', { name: '保存连接配置', exact: true }).click();
  await expect.poll(() => mock.saved.length).toBe(1);
  expect(mock.saved[0]).toEqual({ name: 'JavDB 在线测试资料库', base_url: apiAddress, revision: 7 });
  expect(mock.saved[0]).not.toHaveProperty('api_key');
  await expect(page.locator('#toasts')).toContainText('已保存媒体库');
  await page.getByRole('button', { name: '连接配置', exact: true }).click();
  await expect(page.locator('#r18-api-key')).toHaveValue('');
  await expect(page.locator('#r18-api-key')).toHaveAttribute('placeholder', '留空保留已保存 API Key');
  await assertNoHorizontalOverflow(page);
  await page.screenshot({ path: info.outputPath('javdb-api-config.png'), fullPage: true, animations: 'disabled', mask: [page.locator('input[type=password]')] });
  expectSafeMock(mock);
});

test('JavDB 预览真实播放仅读取第一方HLS，不进入115或正片状态链', async ({ page, context, live }, info) => {
  test.setTimeout(60_000);
  const previewSegment = await preparePreviewSample(info.outputPath('preview-sample.ts'));
  const mock = await mockJavDB(context, page, live.baseURL, localSources, { previewSegment, singleItem: true });
  await login(page, live);
  await page.locator('.r18-card').first().click();
  const preview = page.locator('#item-trailer');
  await expect(preview).toBeVisible();
  await expect(page.locator('#detail-play')).toHaveText('播放本地 STRM');
  expect(mock.previewRequests).toEqual([]);
  expectPreviewIsolation(mock);

  await preview.click();
  await expect(page.locator('#player-overlay')).toBeVisible();
  await expect(page.locator('#player-title')).toHaveText('预览视频 · ' + video.title);
  await expect.poll(() => mock.previewRequests).toEqual([previewPath, previewSegmentPath]);
  const player = page.locator('#player-stage video');
  await expect(player).toBeVisible();
  await expect.poll(() => player.evaluate(element => (element as HTMLVideoElement).currentTime)).toBeGreaterThan(0);
  await expect(page.locator('.player-track-toolbar')).toHaveCount(0);
  expectPreviewIsolation(mock);
  await assertNoHorizontalOverflow(page);
  await page.screenshot({ path: info.outputPath('javdb-preview-playing.png'), animations: 'disabled' });

  // 结束与关闭都会触发普通影片状态回写；预览没有影片标识，必须在这两条路径上保持零写入。
  await player.evaluate(element => element.dispatchEvent(new Event('ended')));
  await page.keyboard.press('Escape');
  await expect(page.locator('#player-overlay')).toHaveCount(0);
  await expect(preview).toBeFocused();
  await page.waitForTimeout(200);
  expectPreviewIsolation(mock);
  expect(mock.previewRequests).toEqual([previewPath, previewSegmentPath]);
});

test('JavDB 缺少预览与影片标签时不渲染虚假入口', async ({ page, context, live }, info) => {
  test.setTimeout(60_000);
  const mock = await mockJavDB(context, page, live.baseURL, localSources, { noPreview: true, noTags: true, singleItem: true });
  await login(page, live);
  await page.locator('.r18-card').first().click();
  await expect(page.locator('.r18-detail-info')).toContainText(video.title);
  await expect(page.locator('.detail-artwork-grid button')).toHaveCount(3);
  await expect(page.locator('.r18-film-metadata')).toHaveCount(0);
  await expect(page.locator('#item-trailer')).toHaveCount(0);
  await expect(page.locator('#player-overlay')).toHaveCount(0);
  await expect(page.locator('#detail-play')).toHaveText('播放本地 STRM');
  await assertNoHorizontalOverflow(page);
  await page.screenshot({ path: info.outputPath('javdb-detail-no-preview.png'), fullPage: true, animations: 'disabled' });
  expect(mock.previewRequests).toEqual([]);
  expectPreviewIsolation(mock);
});

test('JavDB 隐藏媒体图片后封面、剧照和预览缩略图零请求，预览仍可播放', async ({ page, context, live }, info) => {
  test.setTimeout(60_000);
  await context.addInitScript(() => localStorage.setItem('emby_hide_media_images', 'true'));
  const previewSegment = await preparePreviewSample(info.outputPath('preview-sample.ts'));
  const mock = await mockJavDB(context, page, live.baseURL, localSources, { previewSegment, singleItem: true });
  await login(page, live);
  await expect(page.locator('html')).toHaveAttribute('data-hide-media', 'true');
  await expect(page.locator('.r18-card img')).toHaveCount(0);
  await page.locator('.r18-card').first().click();
  await expect(page.locator('.r18-detail-info')).toContainText(video.title);
  await expect(page.locator('.r18-detail-cover img')).toHaveCount(0);
  await expect(page.locator('.detail-artwork-grid')).toHaveCount(0);
  const preview = page.locator('#item-trailer');
  await expect(preview).toBeVisible();
  await expect(preview.locator('img')).toHaveCount(0);
  await expect(page.locator('.r18-film-metadata .detail-meta-row dd')).toHaveText(video.tags.join(' '));
  await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
  await page.waitForTimeout(750);
  expect(mock.imageRequests).toEqual([]);
  await assertNoHorizontalOverflow(page);
  await page.screenshot({ path: info.outputPath('javdb-images-hidden.png'), fullPage: true, animations: 'disabled' });

  await preview.click();
  await expect.poll(() => mock.previewRequests).toEqual([previewPath, previewSegmentPath]);
  await expect.poll(() => page.locator('#player-stage video').evaluate(element => (element as HTMLVideoElement).currentTime)).toBeGreaterThan(0);
  await page.locator('#player-close').click();
  await expect(page.locator('#player-overlay')).toHaveCount(0);
  await expect(preview).toBeFocused();
  expect(mock.imageRequests).toEqual([]);
  expectPreviewIsolation(mock);
});

test('JavDB 优先播放已存在的本地STRM，切换版本全过程零115请求', async ({ page, context, live }, info) => {
  test.setTimeout(60_000);
  const mock = await mockJavDB(context, page, live.baseURL, localSources);
  await login(page, live);
  await expectCatalog(page, mock, { type: 'all', filter: 'magnets', sort: 'release', order: 'desc', page: '1', limit: '24' });
  await expect(page.locator('.r18-card').first()).toContainText('本地 STRM 可播');
  await page.locator('.r18-card').first().click();
  await expect(page.locator('#r18-local-version option')).toHaveCount(2);
  await expect(page.locator('#detail-play')).toHaveText('播放本地 STRM');
  await expect(page.locator('#detail-play')).toBeEnabled();
  await expect(page.locator('.offline-panel')).toHaveCount(0);
  await expect(page.locator('.r18-resource-origin')).toHaveText(['JavDB 在线磁力', 'AVdb 本地资源库磁力']);
  await expect(page.locator('.r18-resources')).toContainText('AVdb 本地资源库磁力不代表本服务已有 STRM');
  await assertNoHorizontalOverflow(page);
  expect(mock.offlineRequests).toEqual([]);

  for (const source of localSources) {
    await page.locator('#r18-local-version').selectOption(source.id);
    await page.locator('#detail-play').click();
    await expect(page.locator('#player-overlay')).toBeVisible();
    await expect(page.locator('#player-title')).toHaveText(source.name);
    await expect.poll(() => mock.localRequests.at(-1)).toBe(source.id);
    await expect.poll(() => mock.mediaRequests.includes(`/Videos/${source.item_id}/stream`)).toBe(true);
    await expect.poll(() => page.locator('#player-stage video').evaluate(element => (element as HTMLVideoElement).currentTime)).toBeGreaterThan(0);
    expect(mock.offlineRequests).toEqual([]);
    await page.locator('#player-close').click();
    await expect(page.locator('#player-overlay')).toHaveCount(0);
  }
  expect(mock.localRequests).toEqual(localSources.map(source => source.id));
  expect(mock.offlineRequests).toEqual([]);
  await assertNoHorizontalOverflow(page);
  await page.screenshot({ path: info.outputPath('javdb-local-strm.png'), fullPage: true, animations: 'disabled' });
  expectSafeMock(mock);
});

test('JavDB 封面一次503后有界重试恢复原URL图片', async ({ page, context, live }, info) => {
  test.setTimeout(60_000);
  const mock = await mockJavDB(context, page, live.baseURL, [], { imageFailures: 1, singleItem: true });
  await login(page, live);
  await expect(page.locator('.r18-card')).toHaveCount(1);
  const image = page.locator('.r18-cover img');
  await expect(image).toBeVisible();
  await expect.poll(() => image.evaluate(element => (element as HTMLImageElement).complete && (element as HTMLImageElement).naturalWidth > 0)).toBe(true);
  await expect(image).toHaveAttribute('src', imageBase + 'poster');
  expect(mock.imageRequests).toEqual([imageBase + 'poster', imageBase + 'poster']);
  await page.waitForTimeout(750);
  expect(mock.imageRequests).toHaveLength(2);
  await expect(page.locator('.r18-cover')).not.toContainText('图片加载失败');
  await assertNoHorizontalOverflow(page);
  await page.screenshot({ path: info.outputPath('javdb-image-auto-recovered.png'), fullPage: true, animations: 'disabled' });
  expectSafeMock(mock);
});

test('JavDB 封面两次503停止重试，手动列表刷新同URL后恢复', async ({ page, context, live }, info) => {
  test.setTimeout(60_000);
  const mock = await mockJavDB(context, page, live.baseURL, [], { imageFailures: 2, singleItem: true });
  await login(page, live);
  await expect(page.locator('.r18-card')).toHaveCount(1);
  const cover = page.locator('.r18-cover');
  await expect(cover).toContainText('图片加载失败');
  await expect(cover.locator('img')).toHaveCount(0);
  expect(mock.imageRequests).toEqual([imageBase + 'poster', imageBase + 'poster']);
  // 超过两轮自动重试间隔后仍无新请求，失败不会造成无限请求风暴。
  await page.waitForTimeout(1250);
  expect(mock.imageRequests).toHaveLength(2);
  await page.screenshot({ path: info.outputPath('javdb-image-bounded-failure.png'), fullPage: true, animations: 'disabled' });

  await page.locator('.r18-header').getByRole('button', { name: '刷新', exact: true }).click();
  const image = cover.locator('img');
  await expect(image).toBeVisible();
  await expect.poll(() => image.evaluate(element => (element as HTMLImageElement).complete && (element as HTMLImageElement).naturalWidth > 0)).toBe(true);
  await expect(image).toHaveAttribute('src', imageBase + 'poster');
  await expect(cover).not.toContainText('图片加载失败');
  expect(mock.imageRequests).toEqual(Array(3).fill(imageBase + 'poster'));
  expect(mock.queries).toHaveLength(2);
  await assertNoHorizontalOverflow(page);
  await page.screenshot({ path: info.outputPath('javdb-image-manual-recovered.png'), fullPage: true, animations: 'disabled' });
  expectSafeMock(mock);
});
