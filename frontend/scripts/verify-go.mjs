import { execFile } from 'node:child_process';
import { cp, mkdir, mkdtemp, rm, stat } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';

const root = fileURLToPath(new URL('../../', import.meta.url));
const temporary = await mkdtemp(path.join(tmpdir(), 'emby-ui-clean-build-'));
const run = promisify(execFile);
const options = { cwd: temporary, windowsHide: true, timeout: 180_000, maxBuffer: 8 * 1024 * 1024 };

try {
  for (const name of ['go.mod', 'go.sum', 'cmd', 'internal']) {
    await cp(path.join(root, name), path.join(temporary, name), {
      recursive: true,
      filter: source => !['web_dist', 'web_dist.next', 'web_dist.previous'].includes(path.basename(source)),
    });
  }
  await run('go', ['build', './...'], options);
  console.log('PASS: clean source copy without frontend/ or web_dist builds with Go only');
  let rejected = false;
  try { await run('go', ['build', '-tags', 'embedui', './...'], options); }
  catch (error) {
    if (!String(error.stderr).includes('no matching files found')) throw error;
    rejected = true;
  }
  if (!rejected) throw new Error('embedui unexpectedly succeeded without UI resources');
  console.log('PASS: embedui rejects missing UI rather than embedding a placeholder');
  await cp(path.join(root, 'internal/server/web_dist'), path.join(temporary, 'internal/server/web_dist'), { recursive: true });
  const sizes = {};
  await mkdir(path.join(temporary, 'dist'));
  for (const [goos, goarch] of [['windows', 'amd64'], ['linux', 'amd64']]) {
    const output = path.join(temporary, 'dist', `emby-go-${goos}-${goarch}${goos === 'windows' ? '.exe' : ''}`);
    await run('go', ['build', '-tags', 'embedui', '-trimpath', '-o', output, './cmd/metatube'], {
      ...options, env: { ...process.env, GOOS: goos, GOARCH: goarch, CGO_ENABLED: '0', GOAMD64: 'v1' },
    });
    sizes[`${goos}/${goarch}`] = (await stat(output)).size;
  }
  console.log(`PASS: real embedded UI cross-builds ${JSON.stringify(sizes)}`);
} finally {
  // The unique directory is allocated above and never derived from user input.
  await rm(temporary, { recursive: true, force: true, maxRetries: 3 });
}
