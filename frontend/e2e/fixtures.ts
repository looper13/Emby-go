import { test as base, expect } from '@playwright/test';
import { execFile } from 'node:child_process';
import { copyFile, mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';
import { startLiveEnvironment } from '../../vue-migration/tools/live-env/environment.mjs';
import { loadPlaywright, sanitizeTrace } from '../../vue-migration/tools/live-env/browser-baseline.mjs';

type Live = Awaited<ReturnType<typeof startLiveEnvironment>>;
const repo = fileURLToPath(new URL('../../', import.meta.url));

export const test = base.extend<{ evidence: void }, { live: Live }>({
  evidence: [async ({ page, live }, use, info) => {
    const context = page.context();
    await context.route('**/*', route => {
      const origin = new URL(route.request().url()).origin;
      return origin === live.baseURL || origin === live.mediaURL ? route.continue() : route.abort();
    });
    await context.tracing.start({ snapshots: true, screenshots: false, sources: true });
    try { await use(); }
    finally {
      if (info.status !== info.expectedStatus) {
        const raw = path.join(live.root, `trace-${info.testId.replace(/[^a-z0-9]/gi, '_')}.zip`);
        const trace = info.outputPath('trace.zip');
        await context.tracing.stop({ path: raw });
        await sanitizeTrace(raw, trace, loadPlaywright().bundle, live.redact);
        await info.attach('trace', { path: trace, contentType: 'application/zip' });
        await page.screenshot({ path: info.outputPath('failure.png'), mask: [page.locator('input, textarea, [data-secret], [data-private-path], .wall-path, .lib-path, .detail-path, .item-details dd')], animations: 'disabled' });
      } else await context.tracing.stop();
    }
  }, { auto: true }],
  live: [async ({}, use) => {
    const directory = await mkdtemp(path.join(tmpdir(), 'emby-ui-e2e-binary-'));
    let live: Live | undefined;
    try {
      const binary = path.join(directory, process.platform === 'win32' ? 'emby-ui.exe' : 'emby-ui');
      if (process.env.EMBY_E2E_BINARY) await copyFile(process.env.EMBY_E2E_BINARY, binary);
      else await promisify(execFile)('go', ['build', '-tags', 'embedui', '-o', binary, './cmd/metatube'], {
        cwd: repo, windowsHide: true, timeout: 120_000,
      });
      live = await startLiveEnvironment({ binaryPath: binary, mediaCount: Number(process.env.EMBY_E2E_MEDIA_COUNT || 307) });
      await use(live);
    } finally {
      try { if (live) await live.close(); }
      finally { await rm(directory, { recursive: true, force: true }); }
    }
  }, { scope: 'worker', timeout: 120_000 }],
});

export { expect };
