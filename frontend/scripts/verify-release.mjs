import { execFile } from 'node:child_process';
import { cp, mkdir, mkdtemp, readFile, rm, stat, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';
import { createHash } from 'node:crypto';
import { gzipSync } from 'node:zlib';

const run = promisify(execFile);
const root = fileURLToPath(new URL('../../', import.meta.url));
const temporary = await mkdtemp(path.join(tmpdir(), 'emby-release-verify-'));
const output = path.join(root, 'vue-migration/artifacts/P7/release');
const previousRef = process.env.EMBY_RELEASE_PREVIOUS_REF || '5c457893698e74928277d013931f55a2aafb1c84';
const report = { sourceKind: 'clean export of current tracked and non-ignored untracked source; not a committed checkout', targets: [], resources: [], runtime: process.version };
const options = { windowsHide: true, timeout: 300_000, maxBuffer: 8 * 1024 * 1024 };
async function execute(binary, args, cwd = temporary, env = process.env) {
  const result = await run(binary, args, { ...options, cwd, env });
  if (result.stdout.trim()) console.log(result.stdout.trim());
  return result.stdout;
}
try {
  await mkdir(output, { recursive: true });
  report.commit = (await execute('git', ['rev-parse', 'HEAD'], root)).trim();
  report.goVersion = (await execute('go', ['version'], root)).trim();
  const listing = await run('git', ['ls-files', '-co', '--exclude-standard', '-z'], { ...options, cwd: root });
  const paths = [...new Set(listing.stdout.split('\0').filter(Boolean))];
  for (const file of paths) {
    if (file.startsWith('vue-migration/artifacts/')) continue;
    const target = path.join(temporary, file);
    await mkdir(path.dirname(target), { recursive: true });
    await cp(path.join(root, file), target);
  }
  await execute('go', ['build', './...']);
  console.log('PASS clean source default Go build without frontend dependencies or generated UI');
  const npmCli = path.join(path.dirname(process.execPath), 'node_modules/npm/bin/npm-cli.js');
  await execute(process.execPath, [npmCli, '--prefix', 'frontend', 'ci']);
  await execute(process.execPath, [npmCli, '--prefix', 'frontend', 'run', 'typecheck']);
  await execute(process.execPath, [npmCli, '--prefix', 'frontend', 'run', 'test:unit', '--', '--run']);
  await execute(process.execPath, [npmCli, '--prefix', 'frontend', 'run', 'test:build']);
  await execute(process.execPath, [npmCli, '--prefix', 'frontend', 'run', 'build']);
  const manifest = JSON.parse(await readFile(path.join(temporary, 'internal/server/web_dist/build-info.json'), 'utf8'));
  for (const file of manifest.files) {
    const data = await readFile(path.join(temporary, 'internal/server/web_dist', file.path));
    report.resources.push({ path: file.path, raw: data.length, gzip: gzipSync(data).length, sha256: createHash('sha256').update(data).digest('hex') });
  }
  for (const [goos, goarch] of [['linux', 'amd64'], ['linux', 'arm64'], ['windows', 'amd64'], ['darwin', 'amd64'], ['darwin', 'arm64']]) {
    const name = `emby-go-${goos}-${goarch}${goos === 'windows' ? '.exe' : ''}`;
    const binary = path.join(output, name);
    const env = { ...process.env, GOOS: goos, GOARCH: goarch, CGO_ENABLED: '0', GOAMD64: 'v1' };
    await execute('go', ['build', '-tags', 'embedui', '-mod=readonly', '-trimpath', '-buildvcs=false', '-ldflags', '-s -w', '-o', binary, './cmd/metatube'], temporary, env);
    const { stdout: metadata } = await run('go', ['version', '-m', binary], { ...options, cwd: root });
    if (!metadata.includes('-tags=embedui')) throw new Error(`Missing embedui in ${name}`);
    report.targets.push({ target: `${goos}/${goarch}`, size: (await stat(binary)).size, sha256: createHash('sha256').update(await readFile(binary)).digest('hex'), metadata, executed: false });
    console.log(`PASS ${goos}/${goarch}`);
  }
  // Keep the pre-migration baseline stable after the Vue migration is committed.
  const previousCommit = (await execute('git', ['rev-parse', '--verify', `${previousRef}^{commit}`], root)).trim();
  const previous = path.join(temporary, 'previous-source'); await mkdir(previous);
  const archive = path.join(temporary, 'previous.tar');
  await execute('git', ['archive', '--format=tar', `--output=${archive}`, previousCommit], root);
  await execute('tar', ['-xf', archive, '-C', previous], root);
  report.previous = { commit: previousCommit, targets: [] };
  for (const goos of ['windows', 'linux']) {
    const name = goos === 'windows' ? 'emby-go-previous.exe' : 'emby-go-previous-linux-amd64';
    const binary = path.join(output, name);
    await execute('go', ['build', '-trimpath', '-buildvcs=false', '-o', binary, './cmd/metatube'], previous, { ...process.env, GOOS: goos, GOARCH: 'amd64', CGO_ENABLED: '0', GOAMD64: 'v1' });
    report.previous.targets.push({ target: `${goos}/amd64`, size: (await stat(binary)).size, sha256: createHash('sha256').update(await readFile(binary)).digest('hex') });
  }
  report.result = 'passed';
} catch (error) {
  report.result = 'failed'; report.error = String(error); process.exitCode = 1;
  console.error(error);
} finally {
  await writeFile(path.join(output, 'verification.json'), JSON.stringify(report, null, 2) + '\n');
  // This exact root is allocated above, not derived from arguments or repository data.
  if (!path.basename(temporary).startsWith('emby-release-verify-') || path.dirname(temporary) !== tmpdir()) throw new Error('Unsafe cleanup path');
  await rm(temporary, { recursive: true, force: true, maxRetries: 3 });
}
