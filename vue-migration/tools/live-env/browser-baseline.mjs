import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import { createRequire } from 'node:module';
import { pipeline } from 'node:stream/promises';
import { createWriteStream } from 'node:fs';
import { repoRoot, sleep } from './environment.mjs';
import { canonicalRequest, countRequests, isBusiness, isPagination, parseRequestLog } from './request-counter.mjs';

export function loadPlaywright(supplied = process.env.PW_CORE) {
  const require = createRequire(import.meta.url);
  const candidates = [supplied, path.join(repoRoot, 'frontend/node_modules/playwright-core'), path.join(process.env.TEMP || process.env.TMPDIR || '/tmp', 'emby-go-live-vue01/pw/node_modules/playwright-core'), 'playwright-core'].filter(Boolean);
  for (const candidate of candidates) {
    try {
      const entry = require.resolve(candidate);
      const directory = path.dirname(entry);
      const pw = require(entry);
      const bundle = require(path.join(directory, 'lib/utilsBundle.js'));
      assert(bundle.yauzl && bundle.yazl, 'Trace ZIP utilities unavailable');
      return { pw, bundle, version: require(path.join(directory, 'package.json')).version };
    } catch { /* Try the next already-installed package. Never install into the frontend here. */ }
  }
  throw new Error('Install Playwright separately or set PW_CORE to its playwright-core package directory');
}

export async function sanitizeTrace(input, output, bundle, redact) {
  const { yauzl, yazl } = bundle;
  const source = await new Promise((resolve, reject) => yauzl.open(input, { lazyEntries: true }, (error, zip) => error ? reject(error) : resolve(zip)));
  const target = new yazl.ZipFile();
  const written = pipeline(target.outputStream, createWriteStream(output, { flags: 'wx' }));
  try {
    await new Promise((resolve, reject) => {
      source.on('error', reject);
      source.on('end', resolve);
      source.on('entry', entry => {
        source.openReadStream(entry, (error, stream) => {
          if (error) return reject(error);
          const chunks = [];
          stream.on('error', reject);
          stream.on('data', chunk => chunks.push(chunk));
          stream.on('end', () => {
            try {
              let data = Buffer.concat(chunks);
              try {
                const text = new TextDecoder('utf-8', { fatal: true }).decode(data);
                data = Buffer.from(redact(text));
              } catch { /* Binary image/video resources contain no text credentials. */ }
              target.addBuffer(data, entry.fileName);
              source.readEntry();
            } catch (err) { reject(err); }
          });
        });
      });
      source.readEntry();
    });
    target.end();
    await written;
  } catch (error) {
    source.close();
    target.end();
    await written.catch(() => {});
    await fs.rm(output, { force: true });
    throw error;
  }
}

async function wallSnapshot(page) {
  return page.evaluate(() => {
    const cards = [...document.querySelectorAll('.wall-card')];
    const card = cards[249];
    return { count: cards.length, ids: cards.map(el => el.dataset.play), targetId: card?.dataset.play, scrollY: window.scrollY, targetTop: card?.getBoundingClientRect().top, focusId: document.activeElement?.dataset.play || null, documentWidth: document.documentElement.scrollWidth, viewportWidth: window.innerWidth };
  });
}

export async function captureLegacyBaseline(env, { outputDir, screenshotsDir, injectFailure = false, playwrightPath } = {}) {
  await fs.mkdir(outputDir, { recursive: true });
  await fs.mkdir(screenshotsDir, { recursive: true });
  const { pw, bundle, version } = loadPlaywright(playwrightPath);
  const browser = await pw.chromium.launch({ channel: process.env.PW_CHANNEL || 'msedge', headless: true });
  const results = [];
  const allowed = new Set([env.baseURL, env.mediaURL]);
  const browserVersion = browser.version();
  try {
    for (const viewport of [{ name: 'desktop', width: 1280, height: 800 }, { name: 'mobile', width: 390, height: 844 }]) {
      const context = await browser.newContext({ viewport: { width: viewport.width, height: viewport.height }, deviceScaleFactor: 1, isMobile: viewport.name === 'mobile', hasTouch: viewport.name === 'mobile', serviceWorkers: 'block' });
      const page = await context.newPage();
      const requests = [];
      const pageErrors = [];
      const blocked = [];
      let phase = 'login';
      const log = () => env.diagnostics().find(child => child.name === 'go').log;
      const startLog = log().length;
      const rawTrace = path.join(env.root, `${viewport.name}-raw-trace.zip`);
      const trace = path.join(outputDir, `${viewport.name}-trace.zip`);
      let failure;
      let result;
      // No outbound browser traffic, even if future fixtures accidentally contain remote URLs.
      await context.route('**/*', route => {
        const url = new URL(route.request().url());
        if (allowed.has(url.origin)) return route.continue();
        blocked.push(`${route.request().method()} ${url.origin}${url.pathname}`);
        return route.abort('blockedbyclient');
      });
      page.on('pageerror', error => pageErrors.push(error.message));
      page.on('request', request => {
        const url = new URL(request.url());
        if (url.origin !== env.baseURL) return;
        requests.push({ request, phase, method: request.method(), target: url.pathname + url.search, status: null });
      });
      page.on('response', response => {
        const entry = requests.find(row => row.request === response.request());
        if (entry) entry.status = response.status();
      });
      // Network and DOM trace only: credential input must never be baked into screenshots.
      await context.tracing.start({ screenshots: false, snapshots: true, sources: false });
      const shoot = name => page.screenshot({ path: path.join(screenshotsDir, `${viewport.name}-${name}.png`), mask: [page.locator('.wall-path')], maskColor: '#262b31' });
      try {
        await page.goto(env.baseURL, { waitUntil: 'domcontentloaded' });
        await page.locator('#auth-form').waitFor();
        await shoot('login');
        await page.locator('#auth-user').fill(env.credentials.username);
        await page.locator('#auth-pw').fill(env.credentials.password);
        const loginResponse = page.waitForResponse(response => new URL(response.url()).pathname === '/Users/AuthenticateByName');
        await page.locator('#auth-form button[type=submit]').click();
        assert.equal((await loginResponse).status(), 200);
        await page.waitForURL(/\/admin(?:[?#]|$)/);
        env.addSecret(await page.evaluate(() => localStorage.getItem('emby_token')));
        await page.locator('.nav-item[data-page="items"]').waitFor();
        await page.waitForFunction(() => document.querySelector('#content .stats') || document.querySelector('#content .stat') || document.querySelector('#content .panel'));
        if (injectFailure) throw new Error('Injected browser failure after real UI login');
        phase = 'wall';
        await page.locator('.nav-item[data-page="items"]').click();
        await page.waitForFunction(() => document.querySelectorAll('.wall-card').length === 100);
        await shoot('wall-first');
        for (const count of [200, 300]) {
          await page.locator('#wall-more').scrollIntoViewIfNeeded();
          await page.waitForFunction(expected => document.querySelectorAll('.wall-card').length === expected, count);
        }
        const card = page.locator('.wall-card').nth(249);
        await card.evaluate(element => element.scrollIntoView({ block: 'center' }));
        await card.focus();
        await sleep(200);
        const before = await wallSnapshot(page);
        assert.equal(before.count, 300);
        assert.equal(new Set(before.ids).size, 300);
        await shoot('wall-250');
        phase = 'detail';
        await card.locator('.wall-meta').click();
        await page.waitForURL(url => url.hash === `#item/${before.targetId}`);
        await page.locator('.item-hero').waitFor();
        await page.waitForFunction(() => [...document.querySelectorAll('.item-hero img')].every(image => image.complete));
        const images = await page.locator('.item-hero img').evaluateAll(nodes => nodes.map(image => ({ loaded: image.naturalWidth > 0, naturalWidth: image.naturalWidth, width: image.getBoundingClientRect().width })));
        assert(images.length > 0 && images.every(image => image.loaded), 'Detail artwork should load');
        await shoot('detail-250');
        phase = 'return';
        await page.goBack();
        await page.waitForFunction(id => document.querySelectorAll('.wall-card').length === 300 && document.activeElement?.dataset.play === id, before.targetId);
        await sleep(350);
        const after = await wallSnapshot(page);
        assert.deepEqual(after.ids, before.ids, 'Return must preserve all 300 IDs and their order');
        assert.equal(after.focusId, before.targetId);
        assert(Math.abs(after.scrollY - before.scrollY) <= 2, `Scroll moved by ${after.scrollY - before.scrollY}px`);
        assert(Math.abs(after.targetTop - before.targetTop) <= 2, 'Target card reading position changed');
        await shoot('return-250');
        const sequence = requests.map(({ request, ...entry }) => entry);
        const pagination = sequence.filter(isPagination);
        assert.deepEqual(pagination.map(entry => new URL(entry.target, env.baseURL).searchParams.get('offset')), ['0', '100', '200']);
        const returnPages = pagination.filter(entry => entry.phase === 'return');
        assert.equal(returnPages.length, 0);
        const duplicatePages = Object.values(countRequests(pagination)).reduce((sum, count) => sum + Math.max(0, count - 1), 0);
        assert.equal(duplicatePages, 0);
        const serverLog = log().slice(startLog);
        const serverRequests = parseRequestLog(serverLog).filter(entry => isBusiness(entry.target));
        assert(serverRequests.length > 0, 'No parseable server requests: do not report a false zero');
        const browserBusiness = sequence.filter(entry => isBusiness(entry.target));
        assert.deepEqual(countRequests(serverRequests), countRequests(browserBusiness), 'Go log and browser request counters disagree');
        assert(browserBusiness.every(entry => entry.status === 200), 'Unexpected business response status');
        assert.equal(blocked.length, 0);
        assert.equal(pageErrors.length, 0);
        result = { viewport, before, after, images, duplicatePages, returnPages: returnPages.length, requestCounts: countRequests(browserBusiness), serverRequestCounts: countRequests(serverRequests), requests: sequence, pageErrors, blocked, horizontalOverflow: after.documentWidth - after.viewportWidth };
        await fs.writeFile(path.join(outputDir, `${viewport.name}-requests.log`), env.redact(serverLog));
        phase = 'pending';
        await page.locator('button[data-status="pending"]').click();
        await page.waitForFunction(() => document.querySelectorAll('.wall-card').length === 3);
        result.pendingCount = await page.locator('.wall-card').count();
        await shoot('pending');
      } catch (error) {
        failure = error;
        await page.screenshot({ path: path.join(screenshotsDir, `${viewport.name}-failure.png`), mask: [page.locator('input'), page.locator('.wall-path')], maskColor: '#262b31' }).catch(() => {});
      } finally {
        try {
          await context.tracing.stop({ path: rawTrace });
          await sanitizeTrace(rawTrace, trace, bundle, env.redact);
        } catch (traceError) { failure ||= traceError; }
        await context.close();
      }
      if (failure) {
        failure.evidence = { viewport, playwrightVersion: version, browserVersion, trace: path.basename(trace), error: env.redact(failure.message), requests: requests.map(({ request, ...entry }) => entry), pageErrors, blocked };
        throw failure;
      }
      results.push(result);
    }
    return { playwrightVersion: version, browserVersion, results };
  } finally {
    await browser.close();
  }
}
