import { test as base, expect } from './fixtures';
import { createServer } from 'node:http';
import { randomUUID, createHash } from 'node:crypto';
import { mkdir, readFile, writeFile, readdir, stat, rm } from 'node:fs/promises';
import path from 'node:path';
import type { Page } from '@playwright/test';

type Live = Awaited<ReturnType<typeof import('../../vue-migration/tools/live-env/environment.mjs').startLiveEnvironment>>;
export async function login(page: Page, live: Live, target = '/scrape') {
  await page.goto(`${live.baseURL}/admin#${target}`);
  await page.locator('#auth-user').fill(live.credentials.username);
  await page.locator('#auth-pw').fill(live.credentials.password);
  await page.locator('#auth-form button').click();
  await expect(page.locator('#nav')).toBeVisible();
}
export async function navigate(page: Page, name: string) {
  await page.locator(`[data-page=${name}]`).click();
  await expect(page).toHaveURL(new RegExp(`#/${name}$`));
}
export async function disk(directory: string): Promise<Record<string, { hash: string; mtime: number }>> {
  const result: Record<string, { hash: string; mtime: number }> = {};
  for (const name of await readdir(directory)) {
    const file = path.join(directory, name), s = await stat(file);
    if (s.isFile()) result[name] = { hash: createHash('sha256').update(await readFile(file)).digest('hex'), mtime: s.mtimeMs };
  }
  return result;
}
export const originalNFO = (code: string) => `<movie><title>P5 Original ${code}</title><num>${code}</num><plot>Original plot</plot><actor><name>P5 Fixture Actor</name></actor><custom>P5 retained</custom></movie>`;

async function backend(live: Live) {
  const imageA = await readFile(path.join(live.root, 'media', 'L005', 'poster.png'));
  const imageB = await readFile(path.join(live.root, 'media', 'L005', 'fanart.png'));
  const mode = { delay: 0, fail: new Set<string>(), empty: false, searches: 0, authorized: true };
  const secret = `p5-${randomUUID()}`; live.addSecret(secret);
  let url = '';
  const server = createServer(async (req, res) => {
    try {
      const target = new URL(req.url!, url);
      if (target.pathname.startsWith('/v1/movies/') || target.pathname.startsWith('/v1/actors/')) {
        mode.authorized &&= req.headers.authorization === `Bearer ${secret}`;
        if (mode.delay) await new Promise(resolve => setTimeout(resolve, mode.delay));
      }
      if (res.destroyed) return;
      const json = (data: unknown, status = 200) => { res.writeHead(status, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(data)); };
      if (target.pathname === '/') return json({ ok: true });
      if (target.pathname === '/v2/translate') {
        let input = ''; for await (const chunk of req) input += chunk;
        return json({ translations: (JSON.parse(input).text ?? []).map(() => ({ text: 'P5 translated' })) });
      }
      if (target.pathname === '/v1/movies/search') {
        mode.searches++;
        const q = target.searchParams.get('q')!, code = /ABF-?90[12]/i.exec(q)?.[0].toUpperCase().replace(/^ABF(\d)/, 'ABF-$1') ?? 'ABF-901';
        if (mode.fail.has(code)) return json({ error: { code: 503, message: 'P5 fixture unavailable' } }, 503);
        return json({ data: mode.empty ? [] : ['a', 'b'].map(letter => ({ id: `${code}-${letter}`, number: code, provider: 'fanza', title: `${code} P5 ${letter === 'a' ? 'Alpha' : 'Beta'}`, score: 8, thumb_url: `${url}/actor.png` })) });
      }
      if (target.pathname.startsWith('/v1/movies/fanza/')) {
        const id = target.pathname.split('/').at(-1)!, code = id.slice(0, -2);
        return json({ data: { id, provider: 'fanza', number: code, title: `${code} P5 ${id.endsWith('-a') ? 'Alpha' : 'Beta'}`, summary: 'P5 new summary', director: 'P5 Director', actors: ['P5 Fixture Actor'], genres: ['P5 Genre'], maker: 'P5 Studio', runtime: 90, score: 8, release_date: '2024-01-02', cover_url: `${url}/actor.png` } });
      }
      if (target.pathname === '/v1/actors/search') return json({ data: [{ id: 'p5-actor', name: target.searchParams.get('q'), provider: 'fanza', images: [`${url}/actor.png`] }] });
      if (target.pathname.startsWith('/v1/images/') || target.pathname === '/actor.png') {
        res.writeHead(200, { 'Content-Type': 'image/png' }); return res.end(target.pathname.includes('-b') ? imageB : imageA);
      }
      return json({ error: { code: 404, message: 'Unknown fixture endpoint' } }, 404);
    } catch { if (!res.destroyed) { res.writeHead(500); res.end(); } }
  });
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  url = `http://127.0.0.1:${(server.address() as { port: number }).port}`;
  const directory = path.join(live.root, `p5-${randomUUID()}`); await mkdir(directory);
  const settings = { metatube_url: url, metatube_token: secret, timeout_seconds: 10, concurrency: 1, image_quality: 80, avatars_dir: path.join(directory, 'avatars'), download_images: true, overwrite: true,
    translate: { title: false, summary: false, target_lang: 'ZH', api_url: url, api_key: secret, timeout_seconds: 5 } };
  const previous = await live.request('/api/admin/scrape/settings');
  let libraryId = 0;
  const movies: { id: number; code: string; directory: string; nfo: string }[] = [];
  try {
    await live.request('/api/admin/scrape/settings', { method: 'PUT', body: settings });
    for (const code of ['ABF-901', 'ABF-902']) {
      const folder = path.join(directory, code); await mkdir(folder);
      const nfo = path.join(folder, `${code}.nfo`); await writeFile(nfo, originalNFO(code));
      await writeFile(path.join(folder, `${code}.strm`), `${live.mediaURL}/delay.mp4?ms=1800\n`);
      movies.push({ id: 0, code, directory: folder, nfo });
    }
    const library = await live.request('/api/admin/libraries', { method: 'POST', body: { Name: 'P5 owned fixtures', Path: directory } }); libraryId = library.Id;
    await live.request('/api/admin/scan', { method: 'POST', body: { library_id: libraryId } });
    const indexed = await live.request(`/api/admin/items?library_id=${libraryId}`);
    for (const movie of movies) movie.id = indexed.items.find((m: { Number: string }) => m.Number === movie.code).id;
  } catch (error) { server.closeAllConnections(); server.close(); throw error; }
  return { mode, settings, movies, libraryId,
    async reset(movie = movies[0]!) { await writeFile(movie.nfo, originalNFO(movie.code)); await live.request(`/api/admin/items/${movie.id}/reread`, { method: 'POST' }); },
    async close() {
      for (const kind of ['scrape', 'probe/media']) {
        if ((await live.request(`/api/admin/${kind}/progress`)).running) {
          await live.request(`/api/admin/${kind}/cancel`, { method: 'POST' });
          await expect.poll(async () => (await live.request(`/api/admin/${kind}/progress`)).running, { timeout: 15_000 }).toBe(false);
        }
      }
      await live.request('/api/admin/scrape/settings', { method: 'PUT', body: { ...previous, metatube_token: '', translate: { ...previous.translate, api_key: '' } } });
      const libraries = await live.request('/api/admin/libraries');
      if (libraries.items.some((l: { Id: number; Path: string }) => l.Id === libraryId && l.Path === directory)) await live.request(`/api/admin/libraries/${libraryId}`, { method: 'DELETE', expected: 204 });
      server.closeAllConnections(); await new Promise<void>(resolve => server.close(() => resolve()));
      const relative = path.relative(path.resolve(live.root), path.resolve(directory));
      if (!relative || relative.startsWith('..') || path.isAbsolute(relative)) throw new Error('P5 cleanup escaped owned environment');
      await rm(directory, { recursive: true, force: true });
    },
  };
}
export const test = base.extend<{ p5: Awaited<ReturnType<typeof backend>> }>({ p5: async ({ live }, use) => {
  const fixture = await backend(live); try { await use(fixture); } finally { await fixture.close(); }
} });
export { expect };
