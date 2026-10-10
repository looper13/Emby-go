import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { parseArgs } from 'node:util';
import { startLiveEnvironment, repoRoot, snapshotTree } from './environment.mjs';
import { captureLegacyBaseline } from './browser-baseline.mjs';

export { startLiveEnvironment } from './environment.mjs';

async function checkMedia(env) {
  const request = (pathname, options = {}) => fetch(`${env.mediaURL}${pathname}`, { ...options, signal: AbortSignal.timeout(10000) });
  const head = await request('/sample.mp4', { method: 'HEAD' });
  const size = Number(head.headers.get('content-length'));
  assert.equal(head.status, 200);
  assert(size > 32);
  const checks = [];
  for (const [range, expected, length, contentRange] of [
    ['bytes=0-31', 206, 32, `bytes 0-31/${size}`],
    ['bytes=-16', 206, 16, `bytes ${size - 16}-${size - 1}/${size}`],
    [`bytes=${size - 10}-`, 206, 10, `bytes ${size - 10}-${size - 1}/${size}`],
    [`bytes=${size}-`, 416, 0, `bytes */${size}`],
    ['bytes=12-4', 416, 0, `bytes */${size}`],
  ]) {
    const response = await request('/sample.mp4', { headers: { Range: range } });
    assert.equal(response.status, expected);
    assert.equal((await response.arrayBuffer()).byteLength, length);
    assert.equal(response.headers.get('content-range'), contentRange);
    checks.push({ range, status: response.status, length, contentRange });
  }
  const redirect = await request('/redirect.mp4', { redirect: 'manual' });
  assert.equal(redirect.status, 302);
  assert.equal(redirect.headers.get('location'), '/sample.mp4');
  const followed = await request('/redirect.mp4', { headers: { Range: 'bytes=0-31' } });
  assert.equal(followed.status, 206);
  await followed.arrayBuffer();
  const start = Date.now();
  const delayed = await request('/delay.mp4?ms=350');
  await delayed.arrayBuffer();
  const delayMs = Date.now() - start;
  assert(delayMs >= 300, `Delay returned too early: ${delayMs}ms`);
  const failure = await request('/failure.mp4');
  assert.equal(failure.status, 503);
  await failure.text();
  await assert.rejects(request('/disconnect.mp4'));
  return { head: head.status, size, checks, redirect: redirect.status, followedRange: followed.status, delayMs, failure: failure.status, disconnect: 'rejected' };
}

async function collectSamples(env, directory) {
  await fs.mkdir(directory, { recursive: true });
  const save = async (name, data) => fs.writeFile(path.join(directory, `${name}.json`), env.redact(JSON.stringify(data, null, 2)) + '\n', { flag: 'wx' });
  await save('status', env.status);
  await save('scan-result', env.scan);
  const page = await env.request('/api/admin/items?limit=100&offset=200&sort=datecreated&order=desc');
  assert.equal(page.items.length, 100);
  await save('items-page-200', page);
  const pending = await env.request('/api/admin/items?status=pending&limit=100&offset=0');
  assert.equal(pending.total, 3);
  await save('pending', pending);
  const cases = {};
  for (const number of ['042', '304', '306', '307']) {
    const data = await env.request(`/api/admin/items?search=ABF-${number}`);
    assert.equal(data.items.length, 1);
    cases[number] = data.items[0].id;
    if (number === '042' || number === '304') await save(`detail-${number}`, await env.request(`/api/admin/items/${cases[number]}/detail`));
  }
  await save('probe-failure', await env.request(`/api/admin/items/${cases['306']}/probe`, { method: 'POST', expected: 502 }));
  await save('probe-success', await env.request(`/api/admin/items/${cases['307']}/probe`, { method: 'POST' }));
  return { pending: pending.total, cases, fixtures: 8 };
}

function cleanObject(value, redact = text => text) {
  let text = JSON.stringify(value, null, 2);
  text = redact(text);
  text = text.replace(/[A-Z]:[\\/](?:[^"\r\n]*[\\/])?emby-go-vue-live-[A-Za-z0-9]+/g, 'TEMP_LIVE');
  return text + '\n';
}

async function main() {
  const { values } = parseArgs({ options: { runs: { type: 'string', default: '2' }, 'fault-check': { type: 'boolean', default: false }, binary: { type: 'string' }, redis: { type: 'string' }, playwright: { type: 'string' } }, strict: true });
  const runs = Number(values.runs);
  assert(Number.isInteger(runs) && runs >= 1 && runs <= 10);
  const stamp = new Date().toISOString().replace(/[:.]/g, '-');
  const evidence = path.join(repoRoot, 'vue-migration/artifacts/live-env', stamp);
  const screenshots = path.join(repoRoot, 'vue-migration/screenshots/live-runs', stamp);
  const fixtures = path.join(evidence, 'fixtures');
  await fs.mkdir(evidence, { recursive: true });
  const frozen = {};
  for (const folder of ['internal/server/web', 'internal/server/web_test']) frozen[folder] = await snapshotTree(path.join(repoRoot, folder));
  const report = { startedAt: new Date().toISOString(), evidence: path.relative(repoRoot, evidence).replaceAll('\\', '/'), runs: [], faults: [], frozenBefore: frozen };
  const options = { binaryPath: values.binary, redisPath: values.redis };
  const writeReport = () => fs.writeFile(path.join(evidence, 'report.json'), cleanObject(report));
  let fatal;
  try {
    for (let iteration = 1; iteration <= runs; iteration++) {
      console.log(`Starting isolated run ${iteration}/${runs}`);
      let env;
      const result = { iteration };
      try {
        env = await startLiveEnvironment(options);
        result.ports = env.ports;
        result.binarySHA256 = env.binarySHA256;
        result.scan = env.scan;
        result.media = await checkMedia(env);
        if (iteration === 1) result.samples = await collectSamples(env, fixtures);
        result.browser = await captureLegacyBaseline(env, { outputDir: path.join(evidence, `run-${iteration}`), screenshotsDir: path.join(screenshots, `run-${iteration}`), playwrightPath: values.playwright });
        result.passed = true;
      } catch (error) {
        result.passed = false;
        result.error = env ? env.redact(error.message) : error.message;
        result.failure = error.evidence;
        result.cleanup = error.cleanup;
        throw error;
      } finally {
        let cleanupFailure;
        if (env) {
          try { result.cleanup = await env.close(); }
          catch (error) { result.cleanup = error.cleanup; result.passed = false; cleanupFailure = error; }
          finally { result.diagnostics = env.diagnostics(); }
          if (result.cleanup) result.cleanup.root = 'TEMP_LIVE';
        }
        report.runs.push(result);
        await writeReport();
        if (cleanupFailure) throw cleanupFailure;
      }
      console.log(`Run ${iteration}: 1280 + 390 passed; 300 restored, duplicate pagination=0, cleanup=${result.cleanup.rootRemoved}`);
    }
    if (values['fault-check']) {
      for (const faultAfter of ['redis', 'go']) {
        let unexpected;
        try {
          unexpected = await startLiveEnvironment({ ...options, faultAfter });
          throw new Error('Expected startup fault did not occur');
        } catch (error) {
          assert.equal(error.message, `Injected startup failure after ${faultAfter}`);
          assert(error.cleanup?.rootRemoved);
          assert(Object.values(error.cleanup.ports).every(port => port.released));
          error.cleanup.root = 'TEMP_LIVE';
          report.faults.push({ stage: faultAfter, passed: true, cleanup: error.cleanup });
        } finally { await unexpected?.close(); }
        console.log(`Injected startup failure after ${faultAfter}: cleaned`);
      }
      const env = await startLiveEnvironment(options);
      const fault = { stage: 'browser-after-login' };
      try {
        await captureLegacyBaseline(env, { outputDir: path.join(evidence, 'fault-browser'), screenshotsDir: path.join(screenshots, 'fault-browser'), injectFailure: true, playwrightPath: values.playwright });
        throw new Error('Expected browser fault did not occur');
      } catch (error) {
        assert.equal(error.message, 'Injected browser failure after real UI login');
        fault.evidence = error.evidence;
        assert((await fs.stat(path.join(evidence, 'fault-browser', 'desktop-trace.zip'))).size > 0);
        fault.passed = true;
      } finally {
        fault.cleanup = await env.close();
        fault.cleanup.root = 'TEMP_LIVE';
        report.faults.push(fault);
      }
      console.log('Injected browser failure: sanitized trace retained, environment cleaned');
    }
  } catch (error) {
    fatal = error;
    report.error = error.message;
  } finally {
    report.frozenUnchanged = true;
    for (const [folder, before] of Object.entries(frozen)) {
      const after = await snapshotTree(path.join(repoRoot, folder));
      try { assert.deepEqual(after, before); } catch (error) { report.frozenUnchanged = false; fatal ||= error; }
    }
    report.finishedAt = new Date().toISOString();
    report.passed = !fatal;
    await writeReport();
    console.log(`Evidence: ${evidence}`);
  }
  if (fatal) throw fatal;
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch(error => { console.error(error.message); process.exitCode = 1; });
}
