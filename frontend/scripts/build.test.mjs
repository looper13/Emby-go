import assert from 'node:assert/strict';
import { mkdtemp, mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { test } from 'node:test';
import { assertBuildPaths, buildPaths, inspectDistribution, publishDistribution, verifyPublished, withBuildLock } from './build-lib.mjs';

async function fixture(t) {
  const root = await mkdtemp(path.join(os.tmpdir(), 'emby-ui-build-test-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  const paths = buildPaths(root);
  for (const directory of [paths.dist, path.dirname(paths.target), path.join(paths.dist, 'assets'), path.join(paths.dist, '.vite')]) {
    await mkdir(directory, { recursive: true });
  }
  await writeFile(path.join(paths.dist, 'index.html'), '<link rel="stylesheet" href="/web/ui/assets/main-12345678.css"><script type="module" src="/web/ui/assets/main-12345678.js"></script>');
  await writeFile(path.join(paths.dist, 'assets/main-12345678.js'), 'console.log("fixture")');
  await writeFile(path.join(paths.dist, 'assets/main-12345678.css'), 'body{color:red}');
  await writeFile(path.join(paths.dist, 'favicon.ico'), 'icon');
  await writeFile(path.join(paths.dist, '.vite/manifest.json'), JSON.stringify({
    'index.html': { isEntry: true, file: 'assets/main-12345678.js', css: ['assets/main-12345678.css'] },
  }));
  return paths;
}

test('only manifest-backed hashed files are immutable and metadata is not published as a Vite asset', async t => {
  const paths = await fixture(t);
  const info = await inspectDistribution(paths.dist, { commit: 'test', dirty: false });
  assert.equal(info.files.find(file => file.path === 'favicon.ico').immutable, false);
  assert.equal(info.files.find(file => file.path.endsWith('.js')).immutable, true);
  assert.equal(info.files.some(file => file.path.startsWith('.vite')), false);
  await withBuildLock(paths, () => publishDistribution(paths, info));
  assert.equal((await verifyPublished(paths.target)).files.length, 4);
});

test('generated directory cannot be redirected to the frozen legacy web directory', async t => {
  const paths = await fixture(t);
  await assert.rejects(assertBuildPaths({ ...paths, dist: path.join(paths.root, 'internal/server/web') }), /unexpected dist/);
});

test('concurrent build is refused without removing the first lock', async t => {
  const paths = await fixture(t);
  await withBuildLock(paths, async () => {
    await assert.rejects(withBuildLock(paths, () => {}), /Another UI build/);
    assert.ok(await readFile(path.join(paths.lock, 'owner.json')));
  });
});

test('failure releases build lock', async t => {
  const paths = await fixture(t);
  await assert.rejects(withBuildLock(paths, () => { throw new Error('failed'); }), /failed/);
  await withBuildLock(paths, () => {});
});

for (const forbidden of ['.env', 'main.js.map', 'fixture.json', 'test.js.txt']) {
  test(`rejects unexpected ${forbidden}`, async t => {
    const paths = await fixture(t);
    await writeFile(path.join(paths.dist, forbidden), 'secret');
    await assert.rejects(inspectDistribution(paths.dist, {}), /Unexpected generated file/);
  });
}

test('missing resource refuses publication and leaves previous successful output', async t => {
  const paths = await fixture(t);
  const info = await inspectDistribution(paths.dist, {});
  await publishDistribution(paths, info);
  const old = await readFile(path.join(paths.target, 'build-info.json'), 'utf8');
  await rm(path.join(paths.dist, 'assets/main-12345678.js'));
  await assert.rejects(inspectDistribution(paths.dist, {}), /referenced asset/);
  assert.equal(await readFile(path.join(paths.target, 'build-info.json'), 'utf8'), old);
});

test('mutation after validation is detected before replacing old assets', async t => {
  const paths = await fixture(t);
  const info = await inspectDistribution(paths.dist, {});
  await publishDistribution(paths, info);
  await writeFile(path.join(paths.dist, 'assets/main-12345678.js'), 'corrupted');
  await assert.rejects(publishDistribution(paths, info), /Asset changed/);
  assert.equal((await verifyPublished(paths.target)).files.length, 4);
});

test('an interrupted swap is not silently overwritten', async t => {
  const paths = await fixture(t);
  await mkdir(paths.previous);
  await assert.rejects(publishDistribution(paths, await inspectDistribution(paths.dist, {})), /interrupted publish/);
});
